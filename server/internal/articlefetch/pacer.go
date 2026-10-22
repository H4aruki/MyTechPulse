package articlefetch

import (
	"context"
	"sync"
	"time"
)

// pacer は、複数のゴルーチンの呼び出しの開始を、全体で gap 以上あける。
// 待つ間はロックを持たない（開始してよい時刻を先に予約する）。
type pacer struct {
	clock Clock
	gap   time.Duration

	mu   sync.Mutex
	next time.Time
}

func newPacer(clock Clock, gap time.Duration) *pacer { return &pacer{clock: clock, gap: gap} }

// wait は、次に開始してよい時刻まで待つ。ctx が終わったらそのエラーを返す。
func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := p.clock.Now()
	start := p.next
	if start.Before(now) {
		start = now
	}
	p.next = start.Add(p.gap)
	p.mu.Unlock()

	d := start.Sub(now)
	if d <= 0 {
		return nil
	}
	select {
	case <-p.clock.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
