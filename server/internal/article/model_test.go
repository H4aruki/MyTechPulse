package article

import (
	"strings"
	"testing"
	"time"
)

func validArticle() Article {
	return Article{
		Source:      SourceQiita,
		Title:       "Go and PostgreSQL",
		URL:         "https://qiita.com/example/items/go-postgres",
		Tags:        []string{"Go"},
		Likes:       0,
		PublishedAt: time.Date(2026, 9, 13, 9, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Article)
		wantErr bool
	}{
		{"Qiitaは有効", func(a *Article) {}, false},
		{"Zennは有効", func(a *Article) { a.Source = SourceZenn }, false},
		{"likes 0は有効", func(a *Article) { a.Likes = 0 }, false},
		{"提供元が不明", func(a *Article) { a.Source = "Other" }, true},
		{"提供元が空", func(a *Article) { a.Source = "" }, true},
		{"小文字の提供元", func(a *Article) { a.Source = "qiita" }, true},
		{"タイトルが空", func(a *Article) { a.Title = "  " }, true},
		{"URLが空", func(a *Article) { a.URL = "" }, true},
		{"URLがhttp", func(a *Article) { a.URL = "http://qiita.com/x" }, true},
		{"URLが相対", func(a *Article) { a.URL = "/x" }, true},
		{"likesが負", func(a *Article) { a.Likes = -1 }, true},
		{"公開日時が未設定", func(a *Article) { a.PublishedAt = time.Time{} }, true},
		{"空のタグ", func(a *Article) { a.Tags = []string{"Go", " "} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := validArticle()
			tt.mutate(&a)
			got, err := Validate(a)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && got.PublishedAt.Location() != time.UTC {
				t.Fatalf("PublishedAtがUTCではありません: %v", got.PublishedAt.Location())
			}
		})
	}
}

func TestValidateNormalizesToUTCWithoutMutatingInput(t *testing.T) {
	a := validArticle()
	got, err := Validate(a)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(want) || got.PublishedAt.Location() != time.UTC {
		t.Fatalf("PublishedAt = %v, want %v", got.PublishedAt, want)
	}
	if a.PublishedAt.Location() == time.UTC {
		t.Fatal("入力が書き換えられています")
	}
	got.Tags[0] = "changed"
	if a.Tags[0] != "Go" {
		t.Fatal("Tagsが入力と共有されています")
	}
}

func TestValidateErrorDoesNotLeakContent(t *testing.T) {
	a := validArticle()
	a.Title = "secret-title"
	a.URL = "http://secret.example/path"
	_, err := Validate(a)
	if err == nil {
		t.Fatal("エラーになるはず")
	}
	for _, s := range []string{"secret-title", "secret.example"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("エラーに内容が含まれています: %q", err.Error())
		}
	}
}
