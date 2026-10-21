package recommendation_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
)

// twoUsers は2人分のセッションを持つ。トークンは "token-<ID>"。
type twoUsers struct{}

func (twoUsers) FindUser(_ context.Context, hash [32]byte, _ time.Time) (auth.Session, error) {
	for _, id := range []int64{1, 2} {
		if hash == auth.HashToken("token-"+strconv.FormatInt(id, 10)) {
			return auth.Session{User: auth.User{ID: id, Username: "u" + strconv.FormatInt(id, 10), Role: auth.RoleMember}}, nil
		}
	}
	return auth.Session{}, auth.ErrNotFound
}
func (twoUsers) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (twoUsers) Delete(context.Context, [32]byte) error                   { return nil }
func (twoUsers) DeleteExpired(context.Context, time.Time) error           { return nil }

type movingClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *movingClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func limitedHandler(clock *movingClock) http.Handler {
	authSvc := auth.Service{Sessions: twoUsers{}, Clock: clock}
	rec := &recommendation.Service{Interests: &repo{}, Providers: recommendation.ProviderSet{Qiita: emptyProvider{}, Zenn: emptyProvider{}}, Clock: clock}
	cfg := config.Config{Environment: "test", CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Auth: &authSvc, Recommendation: rec, Clock: clock})
	return h
}

func call(h http.Handler, method, path, body, token, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: token})
	}
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFeedIsLimitedPerUser(t *testing.T) {
	clock := &movingClock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	h := limitedHandler(clock)
	for i := 0; i < 30; i++ {
		if r := call(h, http.MethodGet, "/api/v1/feed", "", "token-1", "192.0.2.1"); r.Code != http.StatusOK {
			t.Fatalf("feed %d = %d", i, r.Code)
		}
		// IPの保険(1分300回)に当たらないよう、別のIPから送る
		if i%2 == 0 {
			clock.Advance(time.Millisecond)
		}
	}
	r := call(h, http.MethodGet, "/api/v1/feed", "", "token-1", "192.0.2.2")
	if r.Code != http.StatusTooManyRequests || r.Header().Get("Retry-After") != "2" || !strings.Contains(r.Body.String(), `"code":"too_many_requests"`) {
		t.Fatalf("31st feed = %d %v %s", r.Code, r.Header(), r.Body.String())
	}
	if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	// 別の利用者には影響しない。同じIPからでも利用者ごとに数える
	if r := call(h, http.MethodGet, "/api/v1/feed", "", "token-2", "192.0.2.2"); r.Code != http.StatusOK {
		t.Fatalf("other user feed = %d", r.Code)
	}
	// 認証できない要求は、今までどおり401(利用者の枠は使わない)
	if r := call(h, http.MethodGet, "/api/v1/feed", "", "", "192.0.2.2"); r.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", r.Code)
	}
	clock.Advance(2 * time.Second)
	if r := call(h, http.MethodGet, "/api/v1/feed", "", "token-1", "192.0.2.2"); r.Code != http.StatusOK {
		t.Fatalf("feed after waiting = %d", r.Code)
	}
}

func TestClicksAllowBurstOf20ThenOnePerSecond(t *testing.T) {
	clock := &movingClock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	h := limitedHandler(clock)
	for i := 0; i < 20; i++ {
		if r := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["go"]}`, "token-1", "192.0.2.1"); r.Code != http.StatusNoContent {
			t.Fatalf("click %d = %d", i, r.Code)
		}
	}
	r := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["go"]}`, "token-1", "192.0.2.1")
	if r.Code != http.StatusTooManyRequests || r.Header().Get("Retry-After") != "1" {
		t.Fatalf("21st click = %d %v", r.Code, r.Header())
	}
	if r := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["go"]}`, "token-2", "192.0.2.1"); r.Code != http.StatusNoContent {
		t.Fatalf("other user click = %d", r.Code)
	}
	clock.Advance(time.Second)
	if r := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["go"]}`, "token-1", "192.0.2.1"); r.Code != http.StatusNoContent {
		t.Fatalf("click after 1s = %d", r.Code)
	}
}

func TestClickIgnoresTagsWithInvisibleOrControlCharacters(t *testing.T) {
	clock := &movingClock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	r := &repo{}
	authSvc := auth.Service{Sessions: twoUsers{}, Clock: clock}
	rec := &recommendation.Service{Interests: r, Providers: recommendation.ProviderSet{Qiita: emptyProvider{}, Zenn: emptyProvider{}}, Clock: clock}
	cfg := config.Config{Environment: "test", CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Auth: &authSvc, Recommendation: rec, Clock: clock})
	// 使えない文字を含むタグは、要求ごと断らず、そのタグだけ学習に使わない(知らないタグを無視するのと同じ扱い)
	for name, tag := range map[string]string{
		"ゼロ幅スペース": `go\u200blang`,
		"改行":      `go\nlang`,
		"タブ":      `go\tlang`,
		"BOM":     `\ufeffgo`,
		"向きの制御":   `go\u202e`,
		"連続した空白":  `Spring  Boot`,
		"使えない記号":  `<b>`,
	} {
		resp := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["Go","`+tag+`"]}`, "token-1", "192.0.2.1")
		if resp.Code != http.StatusNoContent {
			t.Errorf("%s: status = %d %s", name, resp.Code, resp.Body.String())
		}
		if len(r.tags) != 1 || r.tags[0] != "Go" {
			t.Errorf("%s: learned tags = %q, want only Go", name, r.tags)
		}
		clock.Advance(time.Second)
	}
	// 全部が使えないタグでも、要求は成功する(0.8倍の減衰はこれまでどおり行う)
	resp := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["<b>"]}`, "token-1", "192.0.2.1")
	if resp.Code != http.StatusNoContent || len(r.tags) != 0 {
		t.Errorf("all invalid: status = %d, tags = %q", resp.Code, r.tags)
	}
	clock.Advance(time.Second)
	// 長さの誤りは、これまでどおり要求ごと422
	resp = call(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["`+strings.Repeat("a", 51)+`"]}`, "token-1", "192.0.2.1")
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(resp.Body.String(), "validation_failed") {
		t.Errorf("too long: status = %d %s", resp.Code, resp.Body.String())
	}
	clock.Advance(time.Second)
	// 実在するタグの形は通り、前後の空白は取って渡される
	for _, tag := range []string{"C++", "C#", "CI/CD", "自然言語処理（NLP）", "#Next.js", "Spring Boot", " go "} {
		body := `{"tags":[` + strconv.Quote(tag) + `]}`
		if resp := call(h, http.MethodPost, "/api/v1/feedback/article-clicks", body, "token-1", "192.0.2.1"); resp.Code != http.StatusNoContent {
			t.Errorf("%q: status = %d %s", tag, resp.Code, resp.Body.String())
		}
		if want := strings.TrimSpace(tag); len(r.tags) != 1 || r.tags[0] != want {
			t.Errorf("%q: learned tags = %q", tag, r.tags)
		}
		clock.Advance(time.Second)
	}
}
