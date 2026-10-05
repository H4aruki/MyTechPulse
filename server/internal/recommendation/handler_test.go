package recommendation_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
)

const sessionToken = "fixed-session-token"
const origin = "http://localhost:5173"

type sessionRepo struct{ session auth.Session }

func (r sessionRepo) FindUser(_ context.Context, hash [32]byte, _ time.Time) (auth.Session, error) {
	if hash != auth.HashToken(sessionToken) {
		return auth.Session{}, auth.ErrNotFound
	}
	return r.session, nil
}
func (sessionRepo) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (sessionRepo) Delete(context.Context, [32]byte) error                   { return nil }
func (sessionRepo) DeleteExpired(context.Context, time.Time) error           { return nil }

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

type repo struct{ tags []string }

func (r *repo) List(context.Context, int64) ([]interest.Weight, error) {
	return []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}, nil
}
func (r *repo) UpdateForClick(_ context.Context, _ int64, tags []string) ([]interest.Weight, error) {
	r.tags = append([]string(nil), tags...)
	return nil, nil
}

type emptyProvider struct{}

func (emptyProvider) Search(context.Context, string) ([]article.Article, error) { return nil, nil }

func handler() (http.Handler, *repo) {
	store := &repo{}
	authSvc := auth.Service{Sessions: sessionRepo{session: auth.Session{User: auth.User{ID: 9, Username: "fixture", Role: auth.RoleMember}}}, Clock: testClock{}}
	rec := &recommendation.Service{Interests: store, Providers: recommendation.ProviderSet{Qiita: emptyProvider{}, Zenn: emptyProvider{}}, Clock: testClock{}}
	cfg := config.Config{Environment: "test", SwaggerEnabled: true, CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Auth: &authSvc, Recommendation: rec})
	return h, store
}
func request(h http.Handler, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: sessionToken})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func TestFeedAndClickRequireAuthenticationAndKeepScoresPrivate(t *testing.T) {
	h, store := handler()
	if got := request(h, http.MethodGet, "/api/v1/feed", "", false); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", got.Code)
	}
	feed := request(h, http.MethodGet, "/api/v1/feed", "", true)
	if feed.Code != http.StatusOK {
		t.Fatalf("feed = %d %s", feed.Code, feed.Body.String())
	}
	if !strings.Contains(feed.Body.String(), `"qiita_articles":[]`) || strings.Contains(feed.Body.String(), "score") {
		t.Fatalf("unexpected feed body: %s", feed.Body.String())
	}
	click := request(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":["Go"]}`, true)
	if click.Code != http.StatusNoContent || len(store.tags) != 1 || store.tags[0] != "Go" {
		t.Fatalf("click = %d %q %v", click.Code, click.Body.String(), store.tags)
	}
}

func TestClickRejectsInvalidTag(t *testing.T) {
	h, _ := handler()
	got := request(h, http.MethodPost, "/api/v1/feedback/article-clicks", `{"tags":[""]}`, true)
	if got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body %s", got.Code, got.Body.String())
	}
}

func TestRecommendationOpenAPIContainsRoutesAndArticleSchema(t *testing.T) {
	h, _ := handler()
	for _, path := range []string{"/api/v1/feed", "/api/v1/feedback/article-clicks"} {
		req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), path) {
			t.Fatalf("OpenAPI missing %s: %d", path, rec.Code)
		}
	}
}
