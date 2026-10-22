package articlefetch

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

func perr(kind error, status int) error { return &provider.Error{Kind: kind, StatusCode: status} }

func newTestCaller(clock Clock, max int64) *caller {
	return &caller{clock: clock, calls: &counter{max: max}}
}

func TestCallerRetriesTransientFailuresThenSucceeds(t *testing.T) {
	clock := newAutoClock(time.Now())
	c := newTestCaller(clock, 0)
	n := 0
	err := c.do(context.Background(), func(context.Context) error {
		n++
		if n < 4 {
			return perr(provider.ErrUnavailable, http.StatusServiceUnavailable)
		}
		return nil
	})
	if err != nil || n != 4 {
		t.Fatalf("err = %v, calls = %d", err, n)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	got := clock.Waits()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}

func TestCallerGivesUpAfterThreeRetries(t *testing.T) {
	c := newTestCaller(newAutoClock(time.Now()), 0)
	n := 0
	err := c.do(context.Background(), func(context.Context) error {
		n++
		return perr(provider.ErrTimeout, 0)
	})
	if !errors.Is(err, provider.ErrTimeout) || n != 4 {
		t.Fatalf("err = %v, calls = %d, want 4 calls (first + 3 retries)", err, n)
	}
	if c.calls.count() != 4 {
		t.Fatalf("counted = %d", c.calls.count())
	}
}

func TestCallerDoesNotRetryPermanentFailures(t *testing.T) {
	for name, err := range map[string]error{
		"404":   perr(provider.ErrUnavailable, 404),
		"401":   perr(provider.ErrUnavailable, 401),
		"429":   perr(provider.ErrRateLimited, 429),
		"応答不正":  perr(provider.ErrInvalidResponse, 200),
		"不正な要求": perr(provider.ErrInvalidRequest, 0),
		"その他":   errors.New("x"),
	} {
		t.Run(name, func(t *testing.T) {
			clock := newAutoClock(time.Now())
			c := newTestCaller(clock, 0)
			n := 0
			got := c.do(context.Background(), func(context.Context) error { n++; return err })
			if got != err || n != 1 || len(clock.Waits()) != 0 {
				t.Fatalf("got = %v, calls = %d, waits = %v", got, n, clock.Waits())
			}
		})
	}
}

func TestCallerRetriesConnectionFailureWithoutStatus(t *testing.T) {
	c := newTestCaller(newAutoClock(time.Now()), 0)
	n := 0
	_ = c.do(context.Background(), func(context.Context) error { n++; return perr(provider.ErrUnavailable, 0) })
	if n != 4 {
		t.Fatalf("calls = %d", n)
	}
}

func TestCallerBackoffJitterStaysWithinTwentyPercent(t *testing.T) {
	for _, j := range []float64{-1, 0, 0.999} {
		c := &caller{jitter: func() float64 { return j }}
		for attempt, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
			d := c.backoff(attempt)
			lo, hi := time.Duration(float64(base)*0.8), time.Duration(float64(base)*1.2)
			if d < lo || d > hi {
				t.Errorf("backoff(%d) with jitter %v = %v, want within [%v, %v]", attempt, j, d, lo, hi)
			}
		}
	}
}

func TestCallerStopsAtCallLimit(t *testing.T) {
	c := newTestCaller(newAutoClock(time.Now()), 3)
	n := 0
	err := c.do(context.Background(), func(context.Context) error { n++; return perr(provider.ErrUnavailable, 503) })
	if !errors.Is(err, errCallLimit) || n != 3 {
		t.Fatalf("err = %v, calls = %d", err, n)
	}
	// 上限に達した後は通信しない
	if err := c.do(context.Background(), func(context.Context) error { n++; return nil }); !errors.Is(err, errCallLimit) || n != 3 {
		t.Fatalf("err = %v, calls = %d", err, n)
	}
	if c.calls.count() != 3 {
		t.Fatalf("counted = %d", c.calls.count())
	}
}

func TestCallerReturnsContextErrorWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestCaller(newFakeClock(time.Now()), 0) // 待ちが終わらない時計
	done := make(chan error, 1)
	go func() {
		done <- c.do(ctx, func(context.Context) error { return perr(provider.ErrUnavailable, 503) })
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取り消しで戻らない")
	}
}
