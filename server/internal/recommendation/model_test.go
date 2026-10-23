package recommendation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
)

func TestFeedArticlesIncludesIDOnlyForDBArticles(t *testing.T) {
	fromDB := testArticle(article.SourceQiita, "https://qiita.com/a", []string{"go"}, 1, time.Time{})
	fromDB.ID = 42
	direct := testArticle(article.SourceZenn, "https://zenn.dev/a", []string{"go"}, 1, time.Time{})

	got := feedArticles([]article.Article{fromDB, direct})
	if got[0].ID == nil || *got[0].ID != 42 {
		t.Fatalf("DBの記事の id = %v", got[0].ID)
	}
	if got[1].ID != nil {
		t.Fatalf("即時検索の記事に id が入っている: %v", *got[1].ID)
	}
	withID, err := json.Marshal(got[0])
	if err != nil || !strings.Contains(string(withID), `"id":42`) {
		t.Fatalf("json = %s, err = %v", withID, err)
	}
	withoutID, err := json.Marshal(got[1])
	if err != nil || strings.Contains(string(withoutID), `"id"`) {
		t.Fatalf("json = %s, err = %v", withoutID, err)
	}
}
