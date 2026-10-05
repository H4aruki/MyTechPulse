// Package article は、記事の提供元に依存しない共通の記事表現を定義する。
package article

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

// 提供元名。画面表示と推薦計算の両方でこの値を使う。
const (
	SourceQiita = "Qiita"
	SourceZenn  = "Zenn"
)

// Article は提供元ごとの応答型を隠した、推薦計算と画面表示用の記事。
type Article struct {
	Source      string
	Title       string
	URL         string
	Tags        []string
	Likes       int
	PublishedAt time.Time
}

// Validate は不変条件を確認し、PublishedAt をUTCへ揃えた複製を返す。
// 返すエラーには記事の内容を含めない。
func Validate(a Article) (Article, error) {
	if a.Source != SourceQiita && a.Source != SourceZenn {
		return Article{}, errors.New("article: 提供元が不正です")
	}
	if strings.TrimSpace(a.Title) == "" {
		return Article{}, errors.New("article: タイトルが空です")
	}
	if strings.TrimSpace(a.URL) == "" {
		return Article{}, errors.New("article: URLが空です")
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return Article{}, errors.New("article: URLはhttpsの絶対URLにしてください")
	}
	if a.Likes < 0 {
		return Article{}, errors.New("article: likesが負です")
	}
	if a.PublishedAt.IsZero() {
		return Article{}, errors.New("article: 公開日時が未設定です")
	}
	for _, tag := range a.Tags {
		if strings.TrimSpace(tag) == "" {
			return Article{}, errors.New("article: 空のタグがあります")
		}
	}
	out := a
	out.Tags = append([]string(nil), a.Tags...)
	out.PublishedAt = a.PublishedAt.UTC()
	return out, nil
}
