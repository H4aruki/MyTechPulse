package ratelimit_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/ratelimit"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestBurstThenRefill(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(3, 10*time.Second, clock)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("burst %d rejected", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != 10*time.Second {
		t.Fatalf("4th = %v, %v; want rejected, 10s", ok, wait)
	}
	clock.Advance(4 * time.Second)
	if ok, wait := l.Allow("a"); ok || wait != 6*time.Second {
		t.Fatalf("after 4s = %v, %v; want rejected, 6s", ok, wait)
	}
	clock.Advance(6 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("after the full wait should be allowed")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have been refilled")
	}
}

func TestRejectedCallsAreNotCounted(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(1, time.Minute, clock)
	l.Allow("a")
	for i := 0; i < 100; i++ {
		l.Allow("a")
	}
	clock.Advance(time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("rejected calls must not push the recovery back")
	}
}

func TestRefillIsCappedAtBurst(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(2, time.Second, clock)
	l.Allow("a")
	clock.Advance(time.Hour)
	allowed := 0
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("a"); ok {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("allowed = %d, want 2", allowed)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := ratelimit.New(1, time.Hour, newClock())
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a again")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b must not be affected by a")
	}
}

func TestPerWindow(t *testing.T) {
	clock := newClock()
	l := ratelimit.PerWindow(5, time.Hour, clock)
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("%d", i)
		}
	}
	ok, wait := l.Allow("ip")
	if ok || wait != 12*time.Minute {
		t.Fatalf("6th = %v, %v; want rejected, 12m", ok, wait)
	}
	clock.Advance(time.Hour)
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("after an hour %d", i)
		}
	}
}

func TestWaitDoesNotConsumeNorCreateEntries(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(1, time.Minute, clock)
	if w := l.Wait("a"); w != 0 || l.Len() != 0 {
		t.Fatalf("absent key: wait=%v Len=%d", w, l.Len())
	}
	l.Allow("a")
	for i := 0; i < 5; i++ {
		if w := l.Wait("a"); w != time.Minute {
			t.Fatalf("Wait = %v, want 1m", w)
		}
	}
	clock.Advance(20 * time.Second)
	if w := l.Wait("a"); w != 40*time.Second {
		t.Fatalf("Wait after 20s = %v, want 40s", w)
	}
	clock.Advance(40 * time.Second)
	if w := l.Wait("a"); w != 0 {
		t.Fatalf("Wait after full = %v, want 0", w)
	}
	// Wait は使わないので、そのあとの Allow は通る
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("Wait must not consume")
	}
}

func TestTakeFloorsAtZeroAndRefills(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(2, time.Minute, clock)
	l.Take("a") // 記録が無くても作って1回分使う
	if l.Len() != 1 {
		t.Fatalf("Len = %d", l.Len())
	}
	if w := l.Wait("a"); w != 0 {
		t.Fatalf("1 token left, Wait = %v", w)
	}
	for i := 0; i < 5; i++ {
		l.Take("a")
	}
	// 0より下には減らないので、1回分戻るのは1分後
	if w := l.Wait("a"); w != time.Minute {
		t.Fatalf("Wait = %v, want 1m", w)
	}
	clock.Advance(time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("one token should have refilled")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have refilled")
	}
	if w := l.Wait("b"); w != 0 {
		t.Fatalf("other key Wait = %v", w)
	}
}

func TestTakeRespectsKeyCap(t *testing.T) {
	const maxKeys = 100000
	l := ratelimit.New(2, time.Hour, newClock())
	for i := 0; i < maxKeys+10; i++ {
		l.Take("k" + strconv.Itoa(i))
	}
	if l.Len() > maxKeys {
		t.Fatalf("Len = %d, want <= %d", l.Len(), maxKeys)
	}
}

func TestRetryAfterSecondsRoundsUpAndIsAtLeastOne(t *testing.T) {
	for in, want := range map[time.Duration]int{
		0: 1, time.Nanosecond: 1, 999 * time.Millisecond: 1, time.Second: 1, 1001 * time.Millisecond: 2, 12 * time.Minute: 720,
	} {
		if got := ratelimit.RetryAfterSeconds(in); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestSweepDropsIdleKeys(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(2, time.Second, clock)
	for i := 0; i < 50; i++ {
		l.Allow("k" + strconv.Itoa(i))
	}
	if l.Len() != 50 {
		t.Fatalf("Len = %d", l.Len())
	}
	// 使った分が回復して満タンに戻り、掃除の間隔も過ぎる
	clock.Advance(2 * time.Minute)
	l.Allow("fresh")
	if l.Len() != 1 {
		t.Fatalf("Len after sweep = %d, want 1", l.Len())
	}
}

func TestSweepKeepsKeysStillLimited(t *testing.T) {
	clock := newClock()
	l := ratelimit.New(1, time.Hour, clock)
	l.Allow("busy")
	clock.Advance(2 * time.Minute)
	l.Allow("other")
	if ok, _ := l.Allow("busy"); ok {
		t.Fatal("a key that has not recovered must stay limited after a sweep")
	}
}

func TestConcurrentAllowNeverExceedsBurst(t *testing.T) {
	l := ratelimit.New(50, time.Hour, newClock())
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if ok, _ := l.Allow("shared"); ok {
					allowed.Add(1)
				}
				l.Allow("own" + strconv.Itoa(i))
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 50 {
		t.Fatalf("allowed = %d, want 50", allowed.Load())
	}
}

func TestNilLimiterAllowsEverything(t *testing.T) {
	var l *ratelimit.Limiter
	if err := l.Check("x"); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckUser(1); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReturnsProblemWithRetryAfter(t *testing.T) {
	l := ratelimit.New(1, 90*time.Second, newClock())
	if err := l.CheckUser(7); err != nil {
		t.Fatal(err)
	}
	err := l.CheckUser(7)
	var se huma.StatusError
	if !errors.As(err, &se) || se.GetStatus() != http.StatusTooManyRequests {
		t.Fatalf("err = %#v", err)
	}
	var h huma.HeadersError
	if !errors.As(err, &h) || h.GetHeaders().Get("Retry-After") != "90" {
		t.Fatalf("headers = %#v", err)
	}
	if err := l.CheckUser(8); err != nil {
		t.Fatal("other user must not be limited")
	}
}

func TestWriteHTTP(t *testing.T) {
	rec := httptest.NewRecorder()
	ratelimit.WriteHTTP(rec, 1500*time.Millisecond)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != 429 || body.Code != "too_many_requests" || body.Detail != ratelimit.Message {
		t.Fatalf("body = %+v", body)
	}
}

// 追跡するキーの上限(10万件)に達したら、まず満タンに戻った記録を捨てる。それでも上限なら1つ捨てて、件数を増やさない。
func TestKeyCapDropsIdleKeysFirstAndNeverGrows(t *testing.T) {
	const maxKeys = 100000
	clock := newClock()
	l := ratelimit.New(2, time.Second, clock)
	for i := 0; i < maxKeys; i++ {
		l.Allow("k" + strconv.Itoa(i))
	}
	// 回復の途中(満タンでない)なら、新しいキーが来ても件数は上限のまま
	if ok, _ := l.Allow("new-1"); !ok || l.Len() != maxKeys {
		t.Fatalf("at cap: ok=%v Len=%d", ok, l.Len())
	}
	// 2秒で満タンに戻る。いつもの掃除の間隔(1分)より前でも、上限に達したら捨てる
	clock.Advance(2 * time.Second)
	if ok, _ := l.Allow("new-2"); !ok || l.Len() != 1 {
		t.Fatalf("after idle: ok=%v Len=%d, want 1", ok, l.Len())
	}
}
