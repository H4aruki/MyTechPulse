package articlefetch

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPacerSpacesStartsEvenWithTwoGoroutines(t *testing.T) {
	// 時計を止めた状態で10回開始を予約すると、待ち時間が 0, 0.5s, 1s, ... と0.5秒ずつ増える。
	clock := newAutoClock(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	p := newPacer(clock, 500*time.Millisecond)
	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if err := p.wait(context.Background()); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	waits := clock.Waits() // 最初の1回は待たない
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	if len(waits) != 9 {
		t.Fatalf("waits = %v", waits)
	}
	for i, w := range waits {
		if want := time.Duration(i+1) * 500 * time.Millisecond; w != want {
			t.Fatalf("waits[%d] = %v, want %v (all: %v)", i, w, want, waits)
		}
	}
}

func TestPacerWaitStopsWhenCanceled(t *testing.T) {
	clock := newFakeClock(time.Now())
	p := newPacer(clock, time.Hour)
	if err := p.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.wait(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("取り消しでエラーにならない")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取り消しで戻らない")
	}
}
