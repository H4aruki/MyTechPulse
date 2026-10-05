package provider

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
)

// 観測で使う結果コード。値は固定の語彙で、外部応答の内容は入れない。
const (
	OutcomeSuccess         = "success"
	OutcomeTimeout         = "timeout"
	OutcomeRateLimited     = "rate_limited"
	OutcomeInvalidResponse = "invalid_response"
	OutcomeUnavailable     = "unavailable"
	OutcomeInvalidRequest  = "invalid_request"
)

// Outcome は err を結果コードへ変換する。nilは success。
// 分類できないエラーは unavailable として扱う。
func Outcome(err error) string {
	switch {
	case err == nil:
		return OutcomeSuccess
	case errors.Is(err, ErrTimeout):
		return OutcomeTimeout
	case errors.Is(err, ErrRateLimited):
		return OutcomeRateLimited
	case errors.Is(err, ErrInvalidResponse):
		return OutcomeInvalidResponse
	case errors.Is(err, ErrInvalidRequest):
		return OutcomeInvalidRequest
	default:
		return OutcomeUnavailable
	}
}

// StatusClass は外部のHTTP状態を "2xx" のような区分へ丸める。
// 成功は "2xx"、通信できなかった失敗は "none"。
func StatusClass(err error) string {
	if err == nil {
		return "2xx"
	}
	var pe *Error
	if !errors.As(err, &pe) || pe.StatusCode < 100 || pe.StatusCode > 599 {
		return "none"
	}
	return string(rune('0'+pe.StatusCode/100)) + "xx"
}

// LogAttrs は提供元1回分の取得結果を表すログ属性を返す。
// 属性は provider、outcome、status_class、duration_ms だけで、
// トークン・検索語・外部応答の本文・エラー文言は含めない。
func LogAttrs(providerName string, err error, elapsed time.Duration) []slog.Attr {
	return []slog.Attr{
		slog.String("provider", providerName),
		slog.String("outcome", Outcome(err)),
		slog.String("status_class", StatusClass(err)),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
	}
}

// Observed は Client を包み、1回の取得ごとに安全な属性だけのログを1行出す。
type Observed struct {
	Name   string
	Client Client
	Logger *slog.Logger
	// Now は所要時間の計測に使う。nilなら time.Now。
	Now func() time.Time
}

var _ Client = Observed{}

func (o Observed) Search(ctx context.Context, tag string) ([]article.Article, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	articles, err := o.Client.Search(ctx, tag)
	if o.Logger != nil {
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		o.Logger.LogAttrs(ctx, level, "provider_request", LogAttrs(o.Name, err, now().Sub(start))...)
	}
	return articles, err
}
