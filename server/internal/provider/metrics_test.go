package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
)

type fakeClient struct {
	articles []article.Article
	err      error
}

func (f fakeClient) Search(context.Context, string) ([]article.Article, error) {
	return f.articles, f.err
}

func TestOutcome(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "success"},
		{newError(ErrTimeout, 0), "timeout"},
		{newError(ErrRateLimited, 429), "rate_limited"},
		{newError(ErrInvalidResponse, 200), "invalid_response"},
		{newError(ErrUnavailable, 503), "unavailable"},
		{newError(ErrInvalidRequest, 0), "invalid_request"},
		{fmt.Errorf("wrap: %w", newError(ErrTimeout, 0)), "timeout"},
		{errors.New("something else"), "unavailable"},
	}
	for _, tt := range tests {
		if got := Outcome(tt.err); got != tt.want {
			t.Errorf("Outcome(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "2xx"},
		{newError(ErrRateLimited, 429), "4xx"},
		{newError(ErrUnavailable, 503), "5xx"},
		{newError(ErrInvalidResponse, 302), "3xx"},
		{newError(ErrTimeout, 0), "none"},
		{newError(ErrUnavailable, 999), "none"},
		{errors.New("plain"), "none"},
	}
	for _, tt := range tests {
		if got := StatusClass(tt.err); got != tt.want {
			t.Errorf("StatusClass(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestObservedLogsOnlySafeAttributes(t *testing.T) {
	const (
		token = "synthetic-token-abc123"
		tag   = "secret-search-term"
		body  = `{"message":"leaky-external-response-body"}`
	)
	// 秘密値を文言に含む失敗。どの形で渡されても、ログへは出ない。
	leaky := fmt.Errorf("Authorization: Bearer %s tag=%s body=%s", token, tag, body)
	cases := map[string]error{
		"分類済みエラー(429)": &Error{Provider: "qiita", Kind: ErrRateLimited, StatusCode: 429},
		"分類済みエラー(期限)":  &Error{Provider: "qiita", Kind: ErrTimeout, cause: context.DeadlineExceeded},
		"秘密を含む未分類エラー":  leaky,
		"秘密を含む包み込み":    fmt.Errorf("fetch: %w", errors.Join(ErrUnavailable, leaky)),
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			c := Observed{Name: "qiita", Client: fakeClient{err: failure}, Logger: logger}

			if _, err := c.Search(context.Background(), tag); err == nil {
				t.Fatal("エラーがそのまま返る必要があります")
			}
			line := buf.String()
			for _, secret := range []string{token, tag, "leaky-external-response-body", "Authorization", "Bearer"} {
				if strings.Contains(line, secret) {
					t.Fatalf("ログに %q が含まれています: %s", secret, line)
				}
			}
			var rec map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
				t.Fatalf("JSON1行ではありません: %v: %s", err, line)
			}
			allowed := map[string]bool{"time": true, "level": true, "msg": true,
				"provider": true, "outcome": true, "status_class": true, "duration_ms": true}
			for k := range rec {
				if !allowed[k] {
					t.Fatalf("想定外の属性 %q: %s", k, line)
				}
			}
			if rec["provider"] != "qiita" || rec["level"] != "WARN" {
				t.Fatalf("record = %v", rec)
			}
		})
	}
}

func TestObservedRecordsOutcomeVocabulary(t *testing.T) {
	tests := []struct {
		err         error
		wantOutcome string
		wantClass   string
		wantLevel   string
	}{
		{nil, "success", "2xx", "INFO"},
		{&Error{Kind: ErrTimeout}, "timeout", "none", "WARN"},
		{&Error{Kind: ErrRateLimited, StatusCode: 429}, "rate_limited", "4xx", "WARN"},
		{&Error{Kind: ErrInvalidResponse, StatusCode: 200}, "invalid_response", "2xx", "WARN"},
		{&Error{Kind: ErrUnavailable, StatusCode: 502}, "unavailable", "5xx", "WARN"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		c := Observed{Name: "zenn", Client: fakeClient{err: tt.err}, Logger: logger}
		_, _ = c.Search(context.Background(), "go")

		var rec map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
			t.Fatal(err)
		}
		if rec["outcome"] != tt.wantOutcome || rec["status_class"] != tt.wantClass ||
			rec["level"] != tt.wantLevel || rec["provider"] != "zenn" {
			t.Errorf("err=%v record=%v", tt.err, rec)
		}
	}
}

func TestObservedRecordsDurationAndPassesResultThrough(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	base := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	ticks := []time.Time{base, base.Add(250 * time.Millisecond)}
	now := func() time.Time { t := ticks[0]; ticks = ticks[1:]; return t }
	want := []article.Article{{Title: "t"}}

	c := Observed{Name: "qiita", Client: fakeClient{articles: want}, Logger: logger, Now: now}
	got, err := c.Search(context.Background(), "go")
	if err != nil || len(got) != 1 || got[0].Title != "t" {
		t.Fatalf("結果をそのまま返す: got=%v err=%v", got, err)
	}
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["duration_ms"] != float64(250) {
		t.Fatalf("duration_ms = %v", rec["duration_ms"])
	}
}

func TestObservedWithoutLoggerStillWorks(t *testing.T) {
	c := Observed{Name: "qiita", Client: fakeClient{err: newError(ErrTimeout, 0)}}
	if _, err := c.Search(context.Background(), "go"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewJSONFetcherFixesAllowedHosts(t *testing.T) {
	f := NewJSONFetcher(3*time.Second, 1234)
	if f.MaxBytes != 1234 {
		t.Fatalf("MaxBytes = %d", f.MaxBytes)
	}
	if len(f.AllowedHosts) != 2 {
		t.Fatalf("AllowedHosts = %v", f.AllowedHosts)
	}
	for _, h := range []string{"qiita.com", "zenn.dev"} {
		if _, ok := f.AllowedHosts[h]; !ok {
			t.Errorf("%s が許可されていません", h)
		}
	}
	c, ok := f.Client.(*http.Client)
	if !ok || c.Timeout != 3*time.Second || c.CheckRedirect == nil {
		t.Fatalf("実クライアントの設定が不正です: %+v", f.Client)
	}
	// 許可外ホストは通信前に拒否される。
	f.Client = nil
	if err := f.GetJSON(context.Background(), "https://example.com/x", nil, &struct{}{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
}
