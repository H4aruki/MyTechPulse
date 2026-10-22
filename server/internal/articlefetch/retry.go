package articlefetch

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const (
	// maxRetries は1回の呼び出しの再試行の最大回数（最初の1回を除く。合計では最大4回通信する）。
	maxRetries = 3
	// backoffBase は最初の再試行までの待ち。以後 2倍ずつ（1s, 2s, 4s）。
	backoffBase = time.Second
	// backoffJitter は待ち時間のゆらぎ（±20%）。
	backoffJitter = 0.2
)

// errCallLimit は1回の実行の呼び出し上限に達したことを表す（外部へは通信していない）。
var errCallLimit = errors.New("articlefetch: call limit reached")

// counter は外部への通信の回数を数える。max が0なら上限なし。
type counter struct {
	n   atomic.Int64
	max int64
}

// take は通信1回分を数える。上限を超えるなら数えずに false。
func (c *counter) take() bool {
	if c.max <= 0 {
		c.n.Add(1)
		return true
	}
	for {
		cur := c.n.Load()
		if cur >= c.max {
			return false
		}
		if c.n.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func (c *counter) count() int { return int(c.n.Load()) }

// caller は外部への呼び出しに、回数の上限・間隔・再試行をかぶせる。
type caller struct {
	clock  Clock
	jitter func() float64 // -1以上1未満。nilならゆらぎなし
	pacer  *pacer         // nilなら間隔を空けない
	calls  *counter
}

// do は fn を呼ぶ。一時的な失敗（期限切れ・通信失敗・5xx）だけ、待ちをはさんで最大 maxRetries 回やり直す。
// 4xx・応答不正・429は再試行しない。通信1回ごとに回数と間隔の管理を通る。
func (c *caller) do(ctx context.Context, fn func(ctx context.Context) error) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.pacer != nil {
			if err := c.pacer.wait(ctx); err != nil {
				return err
			}
		}
		if !c.calls.take() {
			return errCallLimit
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if attempt >= maxRetries || !retryable(err) {
			return err
		}
		select {
		case <-c.clock.After(c.backoff(attempt)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *caller) backoff(attempt int) time.Duration {
	d := backoffBase << attempt
	if c.jitter == nil {
		return d
	}
	return time.Duration(float64(d) * (1 + backoffJitter*c.jitter()))
}

// retryable は、やり直す価値のある一時的な失敗か。
func retryable(err error) bool {
	var pe *provider.Error
	if !errors.As(err, &pe) {
		return false
	}
	switch {
	case errors.Is(pe.Kind, provider.ErrTimeout):
		return true
	case errors.Is(pe.Kind, provider.ErrUnavailable):
		return pe.StatusCode == 0 || pe.StatusCode >= 500
	}
	return false
}
