package qiita

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const batchItem = `{"id":"abc123","title":"t","url":"https://qiita.com/u/items/abc123","likes_count":3,"created_at":"2026-10-21T01:02:03+09:00","tags":[{"name":"Go"},{"name":"SpringBoot"}],"user":{"id":"alice"}}`

func TestListCreatedRequestAndConversion(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/api/v2/items" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		q := r.URL.Query()
		if q.Get("page") != "3" || q.Get("per_page") != "20" || q.Get("query") != "created:>=2026-10-21 created:<2026-10-22" || len(q) != 3 {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Rate-Remaining", "777")
		_, _ = io.WriteString(w, "["+batchItem+"]")
	})
	c := newClient(srv)
	// 時刻が付いていても、UTCの日付に丸める
	page, err := c.ListCreated(context.Background(), time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), 3)
	if err != nil {
		t.Fatal(err)
	}
	if page.RateRemaining != 777 || len(page.Articles) != 1 {
		t.Fatalf("page = %+v", page)
	}
	a := page.Articles[0]
	if a.ExternalID != "abc123" || a.Author != "alice" || a.Likes != 3 || len(a.Tags) != 2 || a.Source != "Qiita" {
		t.Fatalf("article = %+v", a)
	}
	if c.FullPage(page) {
		t.Fatal("1件のページは満杯ではない")
	}
}

func TestListCreatedAllowsArticlesWithoutTags(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[`+strings.Replace(batchItem, `"tags":[{"name":"Go"},{"name":"SpringBoot"}]`, `"tags":[]`, 1)+`]`)
	})
	page, err := newClient(srv).ListCreated(context.Background(), time.Now(), 1)
	if err != nil || len(page.Articles) != 1 || len(page.Articles[0].Tags) != 0 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestListCreatedRejectsMissingRequiredFields(t *testing.T) {
	for name, item := range map[string]string{
		"idが無い":     strings.Replace(batchItem, `"id":"abc123",`, "", 1),
		"idが空":      strings.Replace(batchItem, `"id":"abc123"`, `"id":""`, 1),
		"userが無い":   strings.Replace(batchItem, `,"user":{"id":"alice"}`, "", 1),
		"user.idが空": strings.Replace(batchItem, `"user":{"id":"alice"}`, `"user":{"id":""}`, 1),
		"likesが無い":  strings.Replace(batchItem, `"likes_count":3,`, "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "["+item+"]") })
			_, err := newClient(srv).ListCreated(context.Background(), time.Now(), 1)
			if !errors.Is(err, provider.ErrInvalidResponse) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestListCreatedSkipsOnlyArticlesMissingRequiredFields(t *testing.T) {
	broken := strings.Replace(batchItem, `,"user":{"id":"alice"}`, "", 1)
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "["+batchItem+","+broken+"]")
	})
	c := newClient(srv)
	c.PerPage = 2
	page, err := c.ListCreated(context.Background(), time.Now(), 1)
	if err != nil || len(page.Articles) != 1 || page.Skipped != 1 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if !c.FullPage(page) {
		t.Fatal("飛ばした記事も含めて満杯なら、次のページがありうる")
	}
}

func TestListCreatedRateRemainingMissingIsMinusOne(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "[]") })
	page, err := newClient(srv).ListCreated(context.Background(), time.Now(), 1)
	if err != nil || page.RateRemaining != -1 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestListCreatedReturnsRateRemainingOnRateLimit(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Rate-Remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	page, err := newClient(srv).ListCreated(context.Background(), time.Now(), 1)
	if !errors.Is(err, provider.ErrRateLimited) || page.RateRemaining != 0 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestListCreatedRejectsBadArguments(t *testing.T) {
	c := New(provider.JSONFetcher{}, testToken)
	for name, call := range map[string]func() error{
		"日付なし":   func() error { _, err := c.ListCreated(context.Background(), time.Time{}, 1); return err },
		"ページ0":   func() error { _, err := c.ListCreated(context.Background(), time.Now(), 0); return err },
		"ページ101": func() error { _, err := c.ListCreated(context.Background(), time.Now(), 101); return err },
	} {
		if err := call(); !errors.Is(err, provider.ErrInvalidRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestListCreatedErrorDoesNotLeakToken(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom "+r.Header.Get("Authorization")+" "+r.URL.String())
	})
	_, err := newClient(srv).ListCreated(context.Background(), time.Now(), 1)
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
}
