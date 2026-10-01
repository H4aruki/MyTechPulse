package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// trackedBody はcloseされたかを記録する。
type trackedBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b trackedBody) Close() error { b.closed.Add(1); return nil }

func fakeResponse(req *http.Request, status int, body string, closed *atomic.Int32) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{},
		Body:          trackedBody{strings.NewReader(body), closed},
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

// fakeFetcher は qiita.com だけを許可し、transport で応答を差し替えた取得部品を返す。
func fakeFetcher(rt roundTripFunc) JSONFetcher {
	c := NewHTTPClient(time.Second)
	c.Transport = rt
	return JSONFetcher{
		Client:       c,
		MaxBytes:     64,
		AllowedHosts: map[string]struct{}{"qiita.com": {}},
		RetryDelay:   time.Millisecond,
	}
}

const fakeURL = "https://qiita.com/api/v2/tags/Go/items"

type okBody struct {
	OK bool `json:"ok"`
}

func TestJSONFetcherSuccessSendsHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	f := tlsFetcher(srv, 64)
	h := http.Header{"Authorization": {"Bearer synthetic-token"}}
	var out okBody
	if err := f.GetJSON(context.Background(), srv.URL+"/x", h, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("復号されていません")
	}
	if got.Get("Authorization") != "Bearer synthetic-token" || got.Get("Accept") != "application/json" {
		t.Fatalf("ヘッダーが送られていません: %v", got)
	}
}

// tlsFetcher はテスト用TLSサーバーのhostだけを許可し、リダイレクトを追従しない実クライアントを使う。
func tlsFetcher(srv *httptest.Server, maxBytes int64) JSONFetcher {
	u, _ := url.Parse(srv.URL)
	c := NewHTTPClient(2 * time.Second)
	c.Transport = srv.Client().Transport
	return JSONFetcher{
		Client:       c,
		MaxBytes:     maxBytes,
		AllowedHosts: map[string]struct{}{u.Hostname(): {}},
		RetryDelay:   time.Millisecond,
	}
}

func TestJSONFetcherRejectsUnsafeURLsBeforeSending(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})
	tests := map[string]string{
		"http":        "http://qiita.com/api/v2/tags/Go/items",
		"許可外host":     "https://example.com/api",
		"前方一致だけのhost": "https://qiita.com.evil.example/api",
		"後方一致だけのhost": "https://evilqiita.com/api",
		"userinfoで偽装": "https://qiita.com@evil.example/api",
		"scheme無し":    "qiita.com/api",
		"空":           "",
		"ftp":         "ftp://qiita.com/api",
		"host無し":      "https:///api",
		"大文字小文字違いは別host扱い": "https://QIITA.COM.evil/api",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			err := f.GetJSON(context.Background(), raw, nil, &okBody{})
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("通信してはいけません: %d回", calls.Load())
	}
}

func TestJSONFetcherRejectsMisconfiguration(t *testing.T) {
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) { return nil, errors.New("no") })
	f.MaxBytes = 0
	if err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("MaxBytes 0: err = %v", err)
	}
	f = fakeFetcher(nil)
	f.Client = nil
	if err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Client nil: err = %v", err)
	}
}

func TestJSONFetcherDoesNotFollowRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer target.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	var out okBody
	err := tlsFetcher(srv, 64).GetJSON(context.Background(), srv.URL+"/x", nil, &out)
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v, want ErrInvalidResponse", err)
	}
	if targetHits.Load() != 0 {
		t.Fatal("リダイレクト先へ接続してはいけません")
	}
}

func TestJSONFetcherRejectsRedirectFollowedByInjectedClient(t *testing.T) {
	// 追従してしまうクライアントを注入されても、別URLの応答は受け取らない。
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer srv.Close()
	f := tlsFetcher(srv, 64)
	f.Client = srv.Client() // 既定どおり追従する

	var out okBody
	err := f.GetJSON(context.Background(), srv.URL+"/x", nil, &out)
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v, want ErrInvalidResponse", err)
	}
	if out.OK {
		t.Fatal("別URLの応答を復号してはいけません")
	}
}

func TestJSONFetcherStatusHandling(t *testing.T) {
	tests := []struct {
		status    int
		want      error
		wantCalls int32
	}{
		{http.StatusTooManyRequests, ErrRateLimited, 1},
		{http.StatusBadRequest, ErrUnavailable, 1},
		{http.StatusUnauthorized, ErrUnavailable, 1},
		{http.StatusForbidden, ErrUnavailable, 1},
		{http.StatusNotFound, ErrUnavailable, 1},
		{http.StatusInternalServerError, ErrUnavailable, 1},
		{http.StatusFound, ErrInvalidResponse, 1},
		{http.StatusNoContent, ErrInvalidResponse, 1},
		{http.StatusBadGateway, ErrUnavailable, 2},
		{http.StatusServiceUnavailable, ErrUnavailable, 2},
		{http.StatusGatewayTimeout, ErrUnavailable, 2},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			var calls, closed atomic.Int32
			f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return fakeResponse(r, tt.status, `{"secret":"response-body"}`, &closed), nil
			})
			err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var pe *Error
			if !errors.As(err, &pe) || pe.StatusCode != tt.status {
				t.Fatalf("StatusCodeが保持されていません: %+v", pe)
			}
			if calls.Load() != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
			if closed.Load() != calls.Load() {
				t.Fatalf("bodyのclose回数 %d が通信回数 %d と違います", closed.Load(), calls.Load())
			}
			if strings.Contains(err.Error(), "response-body") || strings.Contains(err.Error(), "qiita.com") {
				t.Fatalf("エラーに本文またはURLが含まれています: %v", err)
			}
		})
	}
}

func TestJSONFetcherRetriesOnceThenSucceeds(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls, closed atomic.Int32
			f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return fakeResponse(r, status, "", &closed), nil
				}
				return fakeResponse(r, 200, `{"ok":true}`, &closed), nil
			})
			var out okBody
			if err := f.GetJSON(context.Background(), fakeURL, nil, &out); err != nil {
				t.Fatal(err)
			}
			if !out.OK || calls.Load() != 2 || closed.Load() != 2 {
				t.Fatalf("out=%v calls=%d closed=%d", out, calls.Load(), closed.Load())
			}
		})
	}
}

func TestJSONFetcherRetriesConnectionFailureOnce(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("connection refused to https://qiita.com/api/v2/tags/Go/items")
		}
		return fakeResponse(r, 200, `{"ok":true}`, new(atomic.Int32)), nil
	})
	var out okBody
	if err := f.GetJSON(context.Background(), fakeURL, nil, &out); err != nil || !out.OK {
		t.Fatalf("err=%v out=%v", err, out)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestJSONFetcherConnectionFailureTwiceIsUnavailable(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection refused to " + r.URL.String())
	})
	err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2（最大1回の再試行）", calls.Load())
	}
	if strings.Contains(err.Error(), "qiita.com") || strings.Contains(err.Error(), "Go") {
		t.Fatalf("エラーにURLが含まれています: %v", err)
	}
}

func TestJSONFetcherTimeoutIsRetriedOnceAndClassified(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, &url.Error{Op: "Get", URL: r.URL.String(), Err: timeoutErr{}}
	})
	err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestJSONFetcherRealClientTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	f := tlsFetcher(srv, 64)
	c := NewHTTPClient(30 * time.Millisecond)
	c.Transport = srv.Client().Transport
	f.Client = c

	err := f.GetJSON(context.Background(), srv.URL+"/slow", nil, &okBody{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestJSONFetcherContextDeadlineIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tlsFetcher(srv, 64).GetJSON(ctx, srv.URL+"/slow", nil, &okBody{})
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want ErrTimeout wrapping DeadlineExceeded", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestJSONFetcherCanceledContext(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.GetJSON(ctx, fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() > 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestJSONFetcherSkipsRetryWhenRemainingTimeIsShort(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return fakeResponse(r, 503, "", new(atomic.Int32)), nil
	})
	f.RetryDelay = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := f.GetJSON(ctx, fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1（残り時間が待ち時間より短い）", calls.Load())
	}
	if time.Since(start) >= 100*time.Millisecond {
		t.Fatal("待たずに終了するはずです")
	}
}

func TestJSONFetcherStopsWaitingWhenContextCanceled(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return fakeResponse(r, 503, "", new(atomic.Int32)), nil
	})
	f.RetryDelay = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	start := time.Now()
	err := f.GetJSON(ctx, fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
	if time.Since(start) > time.Second {
		t.Fatal("キャンセルで待機を打ち切るはずです")
	}
}

func TestJSONFetcherRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		want   time.Time
	}{
		{"秒", "7", now.Add(7 * time.Second)},
		{"HTTP日付", "Mon, 14 Sep 2026 00:01:00 GMT", time.Date(2026, 9, 14, 0, 1, 0, 0, time.UTC)},
		{"負の秒は無視", "-5", time.Time{}},
		{"解釈不能は無視", "soon", time.Time{}},
		{"無し", "", time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				resp := fakeResponse(r, 429, "", new(atomic.Int32))
				if tt.header != "" {
					resp.Header.Set("Retry-After", tt.header)
				}
				return resp, nil
			})
			f.Now = func() time.Time { return now }
			err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
			var pe *Error
			if !errors.Is(err, ErrRateLimited) || !errors.As(err, &pe) {
				t.Fatalf("err = %v", err)
			}
			if !pe.RetryAt.Equal(tt.want) {
				t.Fatalf("RetryAt = %v, want %v", pe.RetryAt, tt.want)
			}
			if calls.Load() != 1 {
				t.Fatalf("429は再試行しません: calls=%d", calls.Load())
			}
		})
	}
}

func TestJSONFetcherBodyLimit(t *testing.T) {
	// 8バイトの枠 + 詰め物で、ちょうど n バイトの有効なJSONを作る。
	jsonOf := func(n int) string { return `{"a":"` + strings.Repeat("x", n-8) + `"}` }
	type anyBody struct {
		A string `json:"a"`
	}

	t.Run("上限ちょうど(64)は成功 Content-Lengthあり", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, jsonOf(64))
		}))
		defer srv.Close()
		var out anyBody
		if err := tlsFetcher(srv, 64).GetJSON(context.Background(), srv.URL, nil, &out); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("65バイトはContent-Lengthで拒否", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, jsonOf(65))
		}))
		defer srv.Close()
		err := tlsFetcher(srv, 64).GetJSON(context.Background(), srv.URL, nil, &anyBody{})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("chunkedで上限ちょうど(64)は成功", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := jsonOf(64)
			_, _ = io.WriteString(w, body[:10])
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, body[10:])
		}))
		defer srv.Close()
		if err := tlsFetcher(srv, 64).GetJSON(context.Background(), srv.URL, nil, &anyBody{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("chunkedで65バイトは拒否", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := jsonOf(65)
			_, _ = io.WriteString(w, body[:10])
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, body[10:])
		}))
		defer srv.Close()
		err := tlsFetcher(srv, 64).GetJSON(context.Background(), srv.URL, nil, &anyBody{})
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestJSONFetcherDoesNotReadBeyondLimit(t *testing.T) {
	var read int64
	f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
		body := io.NopCloser(readCounter{n: &read})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body, ContentLength: -1, Request: r}, nil
	})
	err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v", err)
	}
	if read > f.MaxBytes+1 {
		t.Fatalf("上限+1バイトを超えて読みました: %d", read)
	}
}

// readCounter は無限に 'x' を返し、読んだ量を数える。
type readCounter struct{ n *int64 }

func (r readCounter) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	*r.n += int64(len(p))
	return len(p), nil
}

func TestJSONFetcherInvalidJSON(t *testing.T) {
	tests := map[string]string{
		"壊れたJSON": `{"ok":`,
		"型違い":     `{"ok":"yes"}`,
		"末尾の余分":   `{"ok":true} {}`,
		"空本文":     ``,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			f := fakeFetcher(func(r *http.Request) (*http.Response, error) {
				return fakeResponse(r, 200, body, new(atomic.Int32)), nil
			})
			err := f.GetJSON(context.Background(), fakeURL, nil, &okBody{})
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v", err)
			}
			if body != "" && strings.Contains(err.Error(), body) {
				t.Fatal("エラーに本文が含まれています")
			}
		})
	}
}

func TestNewHTTPClientDoesNotFollowRedirects(t *testing.T) {
	c := NewHTTPClient(3 * time.Second)
	if c.Timeout != 3*time.Second {
		t.Fatalf("Timeout = %v", c.Timeout)
	}
	if err := c.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
}
