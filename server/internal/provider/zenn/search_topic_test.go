package zenn

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const batchArticle = `{"slug":"s1","title":"t","path":"/alice/articles/s1","liked_count":4,"published_at":"2026-10-21T01:02:03.000+09:00","user":{"username":"alice"}}`

func TestTopicForMatchesSearch(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"Go", "go"},
		{"Spring Boot", "springboot"},
		{"  React  ", "react"},
		{"C++", "c++"},
		{"TDD（テスト駆動開発）", "tdd（テスト駆動開発）"},
		{"Clean Architecture", "cleanarchitecture"},
	} {
		if got := TopicFor(tc.tag); got != tc.want {
			t.Errorf("TopicFor(%q) = %q, want %q", tc.tag, got, tc.want)
		}
		// Search が送る topicname と同じ値
		var sent string
		srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
			sent = r.URL.Query().Get("topicname")
			_, _ = io.WriteString(w, `{"articles":[]}`)
		})
		if _, err := newClient(srv).Search(context.Background(), tc.tag); err != nil {
			t.Fatal(err)
		}
		if sent != TopicFor(tc.tag) {
			t.Errorf("Search(%q) topicname = %q, TopicFor = %q", tc.tag, sent, TopicFor(tc.tag))
		}
	}
}

func TestSearchTopicRequestAndConversion(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("topicname") != "springboot" || q.Get("count") != "5" || len(q) != 2 {
			t.Errorf("query = %v", q)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"articles":[`+batchArticle+`]}`)
	})
	got, err := newClient(srv).SearchTopic(context.Background(), "springboot")
	if err != nil || len(got) != 1 {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
	a := got[0]
	if a.ExternalID != "s1" || a.Author != "alice" || a.Likes != 4 || a.Source != "Zenn" || len(a.Tags) != 0 || a.URL != "https://zenn.dev/alice/articles/s1" {
		t.Fatalf("article = %+v", a)
	}
}

func TestSearchTopicRejectsMissingSlugOrAuthor(t *testing.T) {
	for name, art := range map[string]string{
		"slugが無い":    strings.Replace(batchArticle, `"slug":"s1",`, "", 1),
		"userが無い":    strings.Replace(batchArticle, `,"user":{"username":"alice"}`, "", 1),
		"usernameが空": strings.Replace(batchArticle, `"username":"alice"`, `"username":""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"articles":[`+art+`]}`)
			})
			_, err := newClient(srv).SearchTopic(context.Background(), "go")
			if !errors.Is(err, provider.ErrInvalidResponse) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSearchTopicRejectsUnderivedTopic(t *testing.T) {
	c := New(provider.JSONFetcher{})
	for _, topic := range []string{"", "Go", "spring boot"} {
		if _, err := c.SearchTopic(context.Background(), topic); !errors.Is(err, provider.ErrInvalidRequest) {
			t.Errorf("SearchTopic(%q) err = %v", topic, err)
		}
	}
}
