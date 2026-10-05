// Package provider は、外部の記事提供元へ安全に問い合わせるための共通部品を提供する。
package provider

import (
	"context"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
)

// Client は1つの提供元から、1つのタグに合う記事を取得する。
// 外部応答の型は実装の外へ出さず、検証済みの共通記事だけを返す。
type Client interface {
	Search(ctx context.Context, tag string) ([]article.Article, error)
}
