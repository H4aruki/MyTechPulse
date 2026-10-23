package savedarticle_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/savedarticle"
)

const origin = "http://localhost:5173"

// 利用者ごとのセッショントークン。
var tokens = map[string]int64{"token-a": 9, "token-b": 10}

type sessionRepo struct{}

func (sessionRepo) FindUser(_ context.Context, hash [32]byte, _ time.Time) (auth.Session, error) {
	for tok, id := range tokens {
		if hash == auth.HashToken(tok) {
			return auth.Session{User: auth.User{ID: id, Username: "u", Role: auth.RoleMember}}, nil
		}
	}
	return auth.Session{}, auth.ErrNotFound
}
func (sessionRepo) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (sessionRepo) Delete(context.Context, [32]byte) error                   { return nil }
func (sessionRepo) DeleteExpired(context.Context, time.Time) error           { return nil }

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

// fakeRepo は DB を使わない偽の保管先。
type fakeRepo struct {
	saveUsers, unsaveUsers, listUsers []int64
	saveResult                        bool
	saveErr                           error
	listItems                         []savedarticle.Article
	listTotal                         int
	limit, offset                     int
}

func (r *fakeRepo) Save(_ context.Context, userID, _ int64) (bool, error) {
	r.saveUsers = append(r.saveUsers, userID)
	return r.saveResult, r.saveErr
}
func (r *fakeRepo) Unsave(_ context.Context, userID, _ int64) error {
	r.unsaveUsers = append(r.unsaveUsers, userID)
	return nil
}
func (r *fakeRepo) List(_ context.Context, userID int64, limit, offset int) ([]savedarticle.Article, int, error) {
	r.listUsers = append(r.listUsers, userID)
	r.limit, r.offset = limit, offset
	return r.listItems, r.listTotal, nil
}

func newHandler() (http.Handler, *fakeRepo) {
	repo := &fakeRepo{}
	authSvc := auth.Service{Sessions: sessionRepo{}, Clock: testClock{}}
	cfg := config.Config{Environment: "test", SwaggerEnabled: true, CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Auth:         &authSvc,
		SavedArticle: &savedarticle.Service{Repo: repo},
	})
	return h, repo
}

func do(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if method != http.MethodGet {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: token})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequireAuthentication(t *testing.T) {
	h, repo := newHandler()
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodGet} {
		path := "/api/v1/saved-articles/1"
		if m == http.MethodGet {
			path = "/api/v1/saved-articles"
		}
		if got := do(h, m, path, ""); got.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session = %d %s", m, got.Code, got.Body.String())
		}
		if got := do(h, m, path, "bad-token"); got.Code != http.StatusUnauthorized {
			t.Fatalf("%s with bad session = %d", m, got.Code)
		}
	}
	if len(repo.saveUsers)+len(repo.unsaveUsers)+len(repo.listUsers) != 0 {
		t.Fatal("repo was called without authentication")
	}
}

func TestSaveIsIdempotentAndUnsaveReturns204(t *testing.T) {
	h, repo := newHandler()
	repo.saveResult = true
	if got := do(h, http.MethodPut, "/api/v1/saved-articles/5", "token-a"); got.Code != http.StatusCreated {
		t.Fatalf("first save = %d %s", got.Code, got.Body.String())
	}
	repo.saveResult = false
	if got := do(h, http.MethodPut, "/api/v1/saved-articles/5", "token-a"); got.Code != http.StatusOK {
		t.Fatalf("second save = %d %s", got.Code, got.Body.String())
	}
	if got := do(h, http.MethodDelete, "/api/v1/saved-articles/5", "token-a"); got.Code != http.StatusNoContent {
		t.Fatalf("unsave = %d %s", got.Code, got.Body.String())
	}
	if len(repo.saveUsers) != 2 || repo.saveUsers[0] != 9 || len(repo.unsaveUsers) != 1 || repo.unsaveUsers[0] != 9 {
		t.Fatalf("users save=%v unsave=%v", repo.saveUsers, repo.unsaveUsers)
	}
}

func TestInvalidArticleID(t *testing.T) {
	h, _ := newHandler()
	for _, id := range []string{"0", "abc"} {
		if got := do(h, http.MethodPut, "/api/v1/saved-articles/"+id, "token-a"); got.Code != http.StatusUnprocessableEntity {
			t.Fatalf("PUT %s = %d", id, got.Code)
		}
	}
}

func TestSaveErrors(t *testing.T) {
	h, repo := newHandler()
	repo.saveErr = savedarticle.ErrArticleNotFound
	if got := do(h, http.MethodPut, "/api/v1/saved-articles/5", "token-a"); got.Code != http.StatusNotFound {
		t.Fatalf("not found = %d %s", got.Code, got.Body.String())
	}
	repo.saveErr = savedarticle.ErrLimitReached
	got := do(h, http.MethodPut, "/api/v1/saved-articles/5", "token-a")
	if got.Code != http.StatusConflict {
		t.Fatalf("limit = %d %s", got.Code, got.Body.String())
	}
	var p struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &p); err != nil || p.Detail != "保存は500件まで。いらない保存を外してください" {
		t.Fatalf("detail = %q err=%v body=%s", p.Detail, err, got.Body.String())
	}
}

func TestListUsesSessionUserAndPaging(t *testing.T) {
	h, repo := newHandler()
	repo.listTotal = 1
	repo.listItems = []savedarticle.Article{{ID: 3, Source: "qiita", URL: "https://example.com/3", Title: "t"}}

	got := do(h, http.MethodGet, "/api/v1/saved-articles", "token-b")
	if got.Code != http.StatusOK {
		t.Fatalf("list = %d %s", got.Code, got.Body.String())
	}
	if len(repo.listUsers) != 1 || repo.listUsers[0] != 10 || repo.limit != 20 || repo.offset != 0 {
		t.Fatalf("user=%v limit=%d offset=%d", repo.listUsers, repo.limit, repo.offset)
	}
	var body savedarticle.ListResponse
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil || body.Total != 1 || len(body.Items) != 1 || body.Items[0].ID != 3 || body.Limit != 20 {
		t.Fatalf("body = %s err=%v", got.Body.String(), err)
	}

	if got := do(h, http.MethodGet, "/api/v1/saved-articles?limit=100&offset=40", "token-a"); got.Code != http.StatusOK || repo.limit != 100 || repo.offset != 40 || repo.listUsers[1] != 9 {
		t.Fatalf("custom paging = %d limit=%d offset=%d users=%v", got.Code, repo.limit, repo.offset, repo.listUsers)
	}

	// 空の一覧は null ではなく []
	repo.listItems = nil
	repo.listTotal = 0
	if got := do(h, http.MethodGet, "/api/v1/saved-articles", "token-a"); !strings.Contains(got.Body.String(), `"items":[]`) {
		t.Fatalf("empty list body = %s", got.Body.String())
	}

	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "offset=-1", "limit=abc"} {
		if got := do(h, http.MethodGet, "/api/v1/saved-articles?"+q, "token-a"); got.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d", q, got.Code)
		}
	}
}

func TestWriteRateLimitIsPerUserAndShared(t *testing.T) {
	h, _ := newHandler()
	// PUT と DELETE を合わせて30回まで通る
	for i := 0; i < 30; i++ {
		method := http.MethodPut
		if i%2 == 1 {
			method = http.MethodDelete
		}
		if got := do(h, method, "/api/v1/saved-articles/1", "token-a"); got.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d limited too early", i+1)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		got := do(h, m, "/api/v1/saved-articles/1", "token-a")
		if got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
			t.Fatalf("31st %s = %d retry-after=%q", m, got.Code, got.Header().Get("Retry-After"))
		}
	}
	// 別の利用者は影響を受けない
	if got := do(h, http.MethodPut, "/api/v1/saved-articles/1", "token-b"); got.Code == http.StatusTooManyRequests {
		t.Fatalf("other user limited: %d", got.Code)
	}
	// 一覧は書き込みの枠に数えない
	if got := do(h, http.MethodGet, "/api/v1/saved-articles", "token-a"); got.Code != http.StatusOK {
		t.Fatalf("list after limit = %d", got.Code)
	}
}
