package zenn

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

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "compatibility", "zenn_articles.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

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
	c := New(f)
	c.Endpoint = srv.URL + "/api/articles"
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

// TestCurrentAPIContract は、現行の一覧APIへ送る問い合わせの形を固定する。
func TestCurrentAPIContract(t *testing.T) {
	var gotMethod, gotPath, gotRawQuery string
	var gotQuery url.Values
	var gotAuth []string
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotRawQuery, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.URL.Query()
		gotAuth = r.Header.Values("Authorization")
		_, _ = io.WriteString(w, fixture(t))
	})

	if _, err := newClient(srv).Search(context.Background(), "Go"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s", gotMethod)
	}
	if gotPath != "/api/articles" {
		t.Errorf("path = %q, want /api/articles", gotPath)
	}
	if gotQuery.Get("topicname") != "go" || gotQuery.Get("count") != "5" || len(gotQuery) != 2 {
		t.Errorf("query = %v", gotQuery)
	}
	if len(gotAuth) != 0 {
		t.Errorf("Zennへ認証情報を送ってはいけません: %v", gotAuth)
	}
	if strings.Contains(strings.ToLower(gotPath+gotRawQuery), "rss") || strings.Contains(gotPath, "feed") {
		t.Errorf("RSS系の取得先を使ってはいけません: %s?%s", gotPath, gotRawQuery)
	}
}

func TestCurrentAPIContractDefaultEndpoint(t *testing.T) {
	c := New(provider.JSONFetcher{})
	if c.Endpoint != "https://zenn.dev/api/articles" || c.Count != 5 {
		t.Fatalf("%+v", c)
	}
}

func TestSearchConvertsFixture(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, fixture(t)) })

	got, err := newClient(srv).Search(context.Background(), "  Go  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	a := got[0]
	if a.Source != article.SourceZenn || a.Title != "Go API Design" ||
		a.URL != "https://zenn.dev/example/articles/go-api" || a.Likes != 7 {
		t.Fatalf("%+v", a)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "go" {
		t.Fatalf("タグには検索に使った小文字のトピックを入れる: %v", a.Tags)
	}
	if want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC); !a.PublishedAt.Equal(want) || a.PublishedAt.Location() != time.UTC {
		t.Fatalf("PublishedAt = %v", a.PublishedAt)
	}
}

func TestSearchLowercasesTopic(t *testing.T) {
	var topic string
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		topic = r.URL.Query().Get("topicname")
		_, _ = io.WriteString(w, `{"articles":[]}`)
	})
	if _, err := newClient(srv).Search(context.Background(), "PostgreSQL"); err != nil {
		t.Fatal(err)
	}
	if topic != "postgresql" {
		t.Fatalf("topicname = %q", topic)
	}
}

func TestSearchEscapesTopicInQuery(t *testing.T) {
	var gotQuery url.Values
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = io.WriteString(w, `{"articles":[]}`)
	})
	if _, err := newClient(srv).Search(context.Background(), "C#&count=100"); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("topicname") != "c#&count=100" || gotQuery.Get("count") != "5" || len(gotQuery) != 2 {
		t.Fatalf("topicがqueryへ漏れています: %v", gotQuery)
	}
}

func TestSearchEmptyResultIsSuccess(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"articles":[]}`) })
	got, err := newClient(srv).Search(context.Background(), "go")
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestSearchRejectsBadInputBeforeSending(t *testing.T) {
	tests := []struct {
		name  string
		tag   string
		count int
	}{
		{"空のタグ", "", 0},
		{"空白だけのタグ", "  ", 0},
		{"Countが負", "go", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"articles":[]}`) })
			c := newClient(srv)
			c.Count = tt.count
			if _, err := c.Search(context.Background(), tt.tag); !errors.Is(err, provider.ErrInvalidRequest) {
				t.Fatalf("err = %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("通信前に失敗する必要があります")
			}
		})
	}
}

func TestSearchRejectsInvalidResponses(t *testing.T) {
	wrap := func(fields string) string { return `{"articles":[{` + fields + `}]}` }
	const (
		title = `"title":"T"`
		path  = `"path":"/u/articles/a"`
		liked = `"liked_count":1`
		pub   = `"published_at":"2026-09-01T00:00:00.000Z"`
	)
	tests := map[string]string{
		"titleが欠落":         wrap(path + "," + liked + "," + pub),
		"titleが空":          wrap(`"title":"",` + path + "," + liked + "," + pub),
		"pathが欠落":          wrap(title + "," + liked + "," + pub),
		"liked_countが欠落":   wrap(title + "," + path + "," + pub),
		"liked_countが負":    wrap(title + "," + path + `,"liked_count":-1,` + pub),
		"liked_countが文字列":  wrap(title + "," + path + `,"liked_count":"1",` + pub),
		"published_atが欠落":  wrap(title + "," + path + "," + liked),
		"published_atが不正":  wrap(title + "," + path + "," + liked + `,"published_at":"yesterday"`),
		"pathが相対でない(絶対)":   wrap(title + `,"path":"https://zenn.dev/u/articles/a",` + liked + "," + pub),
		"pathが別hostの絶対URL": wrap(title + `,"path":"https://evil.example/a",` + liked + "," + pub),
		"pathがスキーム相対":      wrap(title + `,"path":"//evil.example/a",` + liked + "," + pub),
		"pathが/で始まらない":     wrap(title + `,"path":"u/articles/a",` + liked + "," + pub),
		"pathにバックスラッシュ":    wrap(title + `,"path":"/\\evil.example/a",` + liked + "," + pub),
		"pathがuserinfo偽装":  wrap(title + `,"path":"@evil.example/a",` + liked + "," + pub),
		"articlesが欠落":      `{}`,
		"articlesがnull":    `{"articles":null}`,
		"articlesが配列でない":   `{"articles":{}}`,
		"トップが配列":           `[]`,
		"要素がnull":          `{"articles":[null]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
			got, err := newClient(srv).Search(context.Background(), "go")
			if !errors.Is(err, provider.ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
			if got != nil {
				t.Fatalf("不正な応答の一部を返してはいけません: %v", got)
			}
		})
	}
}

func TestSearchDeduplicatesSameURLWithinResponse(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"articles":[
			{"title":"A","path":"/u/articles/a","liked_count":3,"published_at":"2026-09-01T00:00:00.000Z"},
			{"title":"B","path":"/u/articles/b","liked_count":2,"published_at":"2026-09-02T00:00:00.000Z"},
			{"title":"A again","path":"/u/articles/a","liked_count":9,"published_at":"2026-09-03T00:00:00.000Z"}
		]}`)
	})
	got, err := newClient(srv).Search(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "A" || got[1].Title != "B" {
		t.Fatalf("先に出た記事を残し、順序を保つ: %+v", got)
	}
}

func TestSearchKeepsOldArticlesForFallback(t *testing.T) {
	// 期間の絞り込みは呼び出し側で行う。古い記事もここでは捨てない。
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"articles":[{"title":"Old","path":"/u/articles/old","liked_count":0,"published_at":"2020-01-01T00:00:00.000Z"}]}`)
	})
	got, err := newClient(srv).Search(context.Background(), "go")
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestSearchKeepsFailureClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
		calls  int32
	}{
		{"429", http.StatusTooManyRequests, provider.ErrRateLimited, 1},
		{"404", http.StatusNotFound, provider.ErrUnavailable, 1},
		{"503は1回だけ再試行", http.StatusServiceUnavailable, provider.ErrUnavailable, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"message":"leaky-response-body"}`)
			})
			_, err := newClient(srv).Search(context.Background(), "secret-topic")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Provider != "zenn" || pe.StatusCode != tt.status {
				t.Fatalf("提供元名/statusが保持されていません: %+v", pe)
			}
			if calls.Load() != tt.calls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tt.calls)
			}
			for _, secret := range []string{"secret-topic", "leaky-response-body"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("エラーに %q が含まれています: %v", secret, err)
				}
			}
		})
	}
}

func TestSearchRedirectIsRejected(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = io.WriteString(w, `{"articles":[]}`)
	}))
	defer target.Close()
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	})
	_, err := newClient(srv).Search(context.Background(), "go")
	if !errors.Is(err, provider.ErrInvalidResponse) || targetHits.Load() != 0 {
		t.Fatalf("err=%v targetHits=%d", err, targetHits.Load())
	}
}

func TestSearchUsesOnlyAllowedHost(t *testing.T) {
	srv, calls := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"articles":[]}`) })
	c := newClient(srv)
	c.Fetcher.AllowedHosts = map[string]struct{}{"zenn.dev": {}}
	if _, err := c.Search(context.Background(), "go"); !errors.Is(err, provider.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("許可外ホストへ通信してはいけません")
	}
}
