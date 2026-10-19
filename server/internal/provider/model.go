// Package provider は、外部の記事提供元へ安全に問い合わせるための共通部品を提供する。
package provider

import (
	"context"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
)

// Client は1つの提供元から記事を取得する。
// 外部応答の型は実装の外へ出さず、検証済みの共通記事だけを返す。
type Client interface {
	// Search は1つのタグに合う記事を返す。
	Search(ctx context.Context, tag string) ([]article.Article, error)
	// Popular はタグを問わず、全体で人気の記事を1回の問い合わせで返す。
	// 興味のあるタグが5つに満たない利用者のフィードの空き枠を埋めるために使う(#240)。
	// since は取得したい期間の始まりの目安。提供元が期間で絞れない場合は使わなくてよい。
	// 期間の最終的な絞り込みは呼び出し側が行う。
	Popular(ctx context.Context, since time.Time) ([]article.Article, error)
}
