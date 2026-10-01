package qiita

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const testToken = "synthetic-token"

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "compatibility", "qiita_articles.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newClient は固定サーバーだけを許可し、リダイレクトを追従しないクライアントを返す。
func newClient(srv *httptest.Server) Client {
	u, _ := url.Parse(srv.URL)
	hc := provider.NewHTTPClient(2 * time.Second)
	hc.Transport = srv.Client().Transport
	f := provider.JSONFetcher{
		Client:       hc,
		MaxBytes:     1 << 20,
		AllowedHosts: map[string]struct{}{u.Hostname(): {}},
		RetryDelay:   time.Millisecond,
	}
	c := New(f, testToken)
	c.BaseURL = srv.URL
	return c
}

func serve(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSearchRequestContractAndConversion(t *testing.T) {
	body := fixture(t)
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/api/v2/tags/Go/items" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		q := r.URL.Query()
		if q.Get("per_page") != "20" || q.Get("page") != "1" || len(q) != 2 {
			t.Errorf("query = %v", q)
		}
		_, _ = io.WriteString(w, body)
	})

	got, err := newClient(srv).Search(context.Background(), "Go")
	if err != nil {
		t.Fatal(err)
	}
	want := []article.Article{{
		Source:      article.SourceQiita,
		Title:       "Go and PostgreSQL",
		URL:         "https://qiita.com/example/items/go-postgres",
		Tags:        []string{"Go", "PostgreSQL"},
		Likes:       4,
		PublishedAt: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
	}}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	g, w := got[0], want[0]
	if g.Source != w.Source || g.Title != w.Title || g.URL != w.URL || g.Likes != w.Likes ||
		!g.PublishedAt.Equal(w.PublishedAt) || g.PublishedAt.Location() != time.UTC ||
		strings.Join(g.Tags, ",") != strings.Join(w.Tags, ",") {
		t.Fatalf("got %+v, want %+v", g, w)
	}
}

func TestSearchEscapesTagAsSinglePathSegment(t *testing.T) {
	tags := map[string]string{
		"Sass/SCSS": "Sass%2FSCSS",
		"../":       "..%2F",
		"C#":        "C%23",
		"C++":       "C++",
		"a b":       "a%20b",
		"日本語":       "%E6%97%A5%E6%9C%AC%E8%AA%9E",
		"a?b=c":     "a%3Fb=c",
		"%2e%2e":    "%252e%252e",
	}
	for tag, wantSegment := range tags {
		t.Run(tag, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotQuery = r.URL.EscapedPath(), r.URL.Query()
				_, _ = io.WriteString(w, "[]")
			})
			if _, err := newClient(srv).Search(context.Background(), tag); err != nil {
				t.Fatal(err)
			}
			if want := "/api/v2/tags/" + wantSegment + "/items"; gotPath != want {
				t.Fatalf("path = %q, want %q", gotPath, want)
			}
			if len(gotQuery) != 2 {
				t.Fatalf("タグがqueryへ漏れています: %v", gotQuery)
			}
		})
	}
}

func TestSearchRejectsBadInputBeforeSending(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		perPage int
	}{
		{"空のタグ", "", 0},
		{"空白だけのタグ", "   ", 0},
		{"ドット1つ", ".", 0},
		{"ドット2つ", "..", 0},
		{"PerPageが負", "Go", -1},
		{"PerPageが101", "Go", 101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "[]")
			})
			c := newClient(srv)
			c.PerPage = tt.perPage
			_, err := c.Search(context.Background(), tt.tag)
			if !errors.Is(err, provider.ErrInvalidRequest) {
				t.Fatalf("err = %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("通信前に失敗する必要があります")
			}
		})
	}
}

func TestSearchPerPageBounds(t *testing.T) {
	for _, tc := range []struct {
		perPage int
		want    string
	}{{0, "20"}, {1, "1"}, {100, "100"}} {
		var got string
		srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Query().Get("per_page")
			_, _ = io.WriteString(w, "[]")
		})
		c := newClient(srv)
		c.PerPage = tc.perPage
		if _, err := c.Search(context.Background(), "Go"); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("PerPage %d: per_page = %q, want %q", tc.perPage, got, tc.want)
		}
	}
}

func TestSearchOmitsAuthorizationWithoutToken(t *testing.T) {
	var has bool
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, has = r.Header["Authorization"]
		_, _ = io.WriteString(w, "[]")
	})
	c := newClient(srv)
	c.Token = ""
	if _, err := c.Search(context.Background(), "Go"); err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("tokenが空ならAuthorizationを送らない")
	}
}

func TestSearchEmptyResultIsSuccess(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "[]") })
	got, err := newClient(srv).Search(context.Background(), "Go")
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestSearchRejectsInvalidResponses(t *testing.T) {
	const ok = `"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]`
	tests := map[string]string{
		"titleが欠落":        `[{"url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"titleが空":         `[{"title":"","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"urlが欠落":          `[{"title":"T","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"tagsが欠落":         `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00"}]`,
		"tagsが空配列":        `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[]}]`,
		"タグ名が空":           `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":""}]}]`,
		"likesが欠落":        `[{"title":"T","url":"https://qiita.com/u/items/1","created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"likesが負":         `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":-1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"likesが文字列":       `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":"1","created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"created_atが不正":   `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"created_at":"yesterday","tags":[{"name":"Go"}]}]`,
		"created_atが欠落":   `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":1,"tags":[{"name":"Go"}]}]`,
		"urlがhttp":        `[{"title":"T","url":"http://qiita.com/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"urlが別host":       `[{"title":"T","url":"https://evil.example/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"urlが前方一致だけのhost": `[{"title":"T","url":"https://qiita.com.evil.example/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"urlがuserinfo偽装":  `[{"title":"T","url":"https://qiita.com@evil.example/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"urlが相対":          `[{"title":"T","url":"/u/items/1","likes_count":1,"created_at":"2026-09-13T00:00:00+09:00","tags":[{"name":"Go"}]}]`,
		"配列でなくオブジェクト":     `{"message":"Not found"}`,
		"要素がnull":         `[null]`,
		"1件でも不正なら全体が不正":   `[{` + ok + `},{"title":"T"}]`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
			got, err := newClient(srv).Search(context.Background(), "Go")
			if !errors.Is(err, provider.ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
			if got != nil {
				t.Fatalf("不正な応答の一部を返してはいけません: %v", got)
			}
		})
	}
}

func TestSearchValidItemWithOffsetIsNormalizedToUTC(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"title":"T","url":"https://qiita.com/u/items/1","likes_count":0,"created_at":"2026-09-13T09:00:00+09:00","tags":[{"name":"Go"}]}]`)
	})
	got, err := newClient(srv).Search(context.Background(), "Go")
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if want := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC); !got[0].PublishedAt.Equal(want) || got[0].PublishedAt.Location() != time.UTC {
		t.Fatalf("PublishedAt = %v", got[0].PublishedAt)
	}
}

func TestSearchKeepsFailureClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"429", http.StatusTooManyRequests, provider.ErrRateLimited},
		{"401", http.StatusUnauthorized, provider.ErrUnavailable},
		{"500", http.StatusInternalServerError, provider.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"message":"leaky-response-body"}`)
			})
			_, err := newClient(srv).Search(context.Background(), "secret-tag")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Provider != "qiita" || pe.StatusCode != tt.status {
				t.Fatalf("提供元名/statusが保持されていません: %+v", pe)
			}
			if tt.status == http.StatusTooManyRequests && pe.RetryAt.IsZero() {
				t.Fatal("Retry-Afterが保持されていません")
			}
			if calls.Load() != 1 {
				t.Fatalf("calls = %d, 429と4xx/500は再試行しません", calls.Load())
			}
			for _, secret := range []string{testToken, "secret-tag", "leaky-response-body"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("エラーに秘密値 %q が含まれています: %v", secret, err)
				}
			}
		})
	}
}

func TestSearchUsesOnlyAllowedHost(t *testing.T) {
	srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "[]") })
	c := newClient(srv)
	c.Fetcher.AllowedHosts = map[string]struct{}{"qiita.com": {}}
	_, err := c.Search(context.Background(), "Go")
	if !errors.Is(err, provider.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("許可外ホストへ通信してはいけません")
	}
}

func TestSearchRedirectIsRejectedWithoutForwardingToken(t *testing.T) {
	var targetAuth atomic.Value
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		targetAuth.Store(r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, "[]")
	}))
	defer target.Close()
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	})
	_, err := newClient(srv).Search(context.Background(), "Go")
	if !errors.Is(err, provider.ErrInvalidResponse) {
		t.Fatalf("err = %v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("リダイレクト先へ接続しました（Authorization=%v）", targetAuth.Load())
	}
}

func TestNewDefaults(t *testing.T) {
	c := New(provider.JSONFetcher{}, "t")
	if c.BaseURL != "https://qiita.com" || c.PerPage != 20 || c.Token != "t" {
		t.Fatalf("%+v", c)
	}
}
