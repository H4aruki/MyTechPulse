package articlefetch

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/provider"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/zenn"
)

const secretToken = "tok-SECRET-0123456789"

// httpFixture は、偽のQiita・Zennサーバー(httptest)に実際のクライアントをつないだ取得。
type httpFixture struct {
	store *fakeJobStore
	logs  *bytes.Buffer
	job   *Job
}

func newHTTPFixture(t *testing.T, qiitaHandler, zennHandler http.HandlerFunc) *httpFixture {
	t.Helper()
	qs := httptest.NewTLSServer(qiitaHandler)
	zs := httptest.NewTLSServer(zennHandler)
	t.Cleanup(qs.Close)
	t.Cleanup(zs.Close)
	hosts := map[string]struct{}{}
	for _, s := range []*httptest.Server{qs, zs} {
		u, _ := url.Parse(s.URL)
		hosts[u.Hostname()] = struct{}{}
	}
	hc := provider.NewHTTPClient(2 * time.Second)
	hc.Transport = qs.Client().Transport // 2つのサーバーは同じ証明書の発行元(httptest)を使う
	fetcher := provider.JSONFetcher{Client: hc, MaxBytes: 1 << 20, AllowedHosts: hosts, MaxAttempts: 1}
	qc := qiita.New(fetcher, secretToken)
	qc.BaseURL = qs.URL
	qc.PerPage = 2
	zc := zenn.New(fetcher)
	zc.Endpoint = zs.URL + "/api/articles"

	logs := &bytes.Buffer{}
	store := &fakeJobStore{
		tags:      []TagRef{{ID: 1, Name: "Go"}, {ID: 2, Name: "Spring Boot"}},
		fetchTags: []FetchTag{{ID: 1, Name: "Go", ZennTopic: "go"}, {ID: 2, Name: "Spring Boot", ZennTopic: "springboot"}},
	}
	return &httpFixture{store: store, logs: logs, job: &Job{
		Store: store, Qiita: qc, Zenn: zc, Clock: newAutoClock(jobNow),
		Logger:           slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, nil)),
		QiitaMaxRequests: 300,
	}}
}

func qiitaItemJSON(id string) string {
	return `{"id":"` + id + `","title":"t","url":"https://qiita.com/u/items/` + id + `","likes_count":2,"created_at":"2026-10-21T01:02:03+09:00","tags":[{"name":"Go"},{"name":"SpringBoot"},{"name":"unknown-tag"}],"user":{"id":"alice"}}`
}

func TestJobOverHTTPSavesArticlesWithMatchedTags(t *testing.T) {
	var qiitaCalls atomic.Int32
	f := newHTTPFixture(t,
		func(w http.ResponseWriter, r *http.Request) {
			qiitaCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+secretToken {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			q := r.URL.Query()
			w.Header().Set("Rate-Remaining", "900")
			// 10-22 の1ページ目だけ満杯、ほかは1件
			if strings.HasPrefix(q.Get("query"), "created:>=2026-10-22") && q.Get("page") == "1" {
				_, _ = io.WriteString(w, "["+qiitaItemJSON("a1")+","+qiitaItemJSON("a2")+"]")
				return
			}
			if strings.HasPrefix(q.Get("query"), "created:>=2026-10-22") {
				_, _ = io.WriteString(w, "[]")
				return
			}
			_, _ = io.WriteString(w, "[]")
		},
		func(w http.ResponseWriter, r *http.Request) {
			topic := r.URL.Query().Get("topicname")
			_, _ = io.WriteString(w, `{"articles":[{"slug":"`+topic+`-s","title":"t","path":"/bob/articles/`+topic+`-s","liked_count":1,"published_at":"2026-10-21T01:02:03.000+09:00","user":{"username":"bob"}}]}`)
		})
	res := f.job.Execute(context.Background())
	if res.Status != StatusSucceeded {
		t.Fatalf("res = %+v", res)
	}
	if qiitaCalls.Load() != 7 || res.QiitaRequests != 7 || res.ZennRequests != 2 {
		t.Fatalf("qiita = %d, res = %+v", qiitaCalls.Load(), res)
	}
	got := map[string][]int32{}
	for _, r := range f.store.saved {
		got[r.Article.URL] = r.TagIDs
	}
	if len(got) != 4 {
		t.Fatalf("saved = %v", got)
	}
	if tags := got["https://qiita.com/u/items/a1"]; len(tags) != 2 || tags[0] != 1 || tags[1] != 2 {
		t.Fatalf("qiita tags = %v", tags)
	}
	if tags := got["https://zenn.dev/bob/articles/springboot-s"]; len(tags) != 1 || tags[0] != 2 {
		t.Fatalf("zenn tags = %v", tags)
	}
}

func TestJobOverHTTPNeverRecordsSecrets(t *testing.T) {
	leaky := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// 失敗の応答の本文に、受け取ったヘッダーとURLをそのまま入れて返すサーバー
		_, _ = io.WriteString(w, "boom "+r.Header.Get("Authorization")+" "+r.URL.String()+" "+secretToken)
	}
	f := newHTTPFixture(t, leaky, leaky)
	res := f.job.Execute(context.Background())
	if res.Status != StatusFailed {
		t.Fatalf("res = %+v", res)
	}
	if res.Message == "" {
		t.Fatal("失敗の内容が記録されない")
	}
	for _, text := range []string{res.Message, f.logs.String()} {
		for _, bad := range []string{secretToken, "Bearer", "Authorization", "https://", "127.0.0.1", "topicname", "created:"} {
			if strings.Contains(text, bad) {
				t.Fatalf("%q が含まれている:\n%s", bad, text)
			}
		}
	}
	// 分類済みの文だけ
	for _, want := range []string{"qiita: provider unavailable (status 500)", "zenn: provider unavailable (status 500)"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("message = %q, want to contain %q", res.Message, want)
		}
	}
	// 再試行つきでも、Qiitaの呼び出しは上限(300)に収まる: 6日 × 4回
	if res.QiitaRequests != 24 {
		t.Fatalf("qiita requests = %d", res.QiitaRequests)
	}
}

func TestJobOverHTTPStopsOnRateLimit(t *testing.T) {
	var calls atomic.Int32
	f := newHTTPFixture(t,
		func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Rate-Remaining", "0")
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"articles":[]}`) })
	res := f.job.Execute(context.Background())
	if n := calls.Load(); n < 1 || n > qiitaWorkers {
		t.Fatalf("calls = %d", n)
	}
	if res.Status != StatusPartial || !strings.Contains(res.Message, "qiita: provider rate limited (status 429)") {
		t.Fatalf("res = %+v", res)
	}
}
