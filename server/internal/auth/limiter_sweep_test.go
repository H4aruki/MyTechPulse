package auth

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var sweepNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// filledLimiter は、窓が過ぎた記録を count 件ずつ(利用者名・IPそれぞれ)持つ制限器を返す。
func filledLimiter(count int) (*LoginLimiter, *fakeClock) {
	clock := &fakeClock{now: sweepNow}
	l := NewLoginLimiter(clock)
	expired := &limitEntry{failures: 3, windowStart: sweepNow.Add(-limiterWindow - time.Second)}
	for i := 0; i < count; i++ {
		e := *expired
		l.accounts[fmt.Sprintf("old-user-%d", i)] = &e
		e2 := *expired
		l.ips[fmt.Sprintf("old-ip-%d", i)] = &e2
	}
	return l, clock
}

func TestLimiterSweepDropsExpiredEntriesButKeepsActiveOnes(t *testing.T) {
	l, _ := filledLimiter(limiterSweepAt / 2)
	l.accounts["window-ends-now"] = &limitEntry{failures: 1, windowStart: sweepNow.Add(-limiterWindow)}
	l.accounts["window-open"] = &limitEntry{failures: 1, windowStart: sweepNow.Add(-limiterWindow + time.Second)}
	// 窓は終わっているが、締め出しがまだ続いている
	l.accounts["still-locked"] = &limitEntry{failures: 5, windowStart: sweepNow.Add(-time.Hour), lockUntil: sweepNow.Add(time.Second), level: 3}
	l.accounts["lock-ends-now"] = &limitEntry{failures: 5, windowStart: sweepNow.Add(-time.Hour), lockUntil: sweepNow}
	l.ips["ip-open"] = &limitEntry{failures: 20, windowStart: sweepNow.Add(-time.Minute)}

	l.sweep(sweepNow)

	wantAccounts := map[string]bool{"window-open": true, "still-locked": true}
	if len(l.accounts) != len(wantAccounts) {
		t.Fatalf("accounts left = %d, want %d", len(l.accounts), len(wantAccounts))
	}
	for k := range wantAccounts {
		if l.accounts[k] == nil {
			t.Errorf("account %q must be kept", k)
		}
	}
	if len(l.ips) != 1 || l.ips["ip-open"] == nil {
		t.Fatalf("ips left = %d, want only ip-open", len(l.ips))
	}
}

func TestLimiterSweepDoesNothingBelowThreshold(t *testing.T) {
	l, _ := filledLimiter(limiterSweepAt/2 - 1) // 合計 limiterSweepAt - 2 件
	before := len(l.accounts) + len(l.ips)
	l.sweep(sweepNow)
	if got := len(l.accounts) + len(l.ips); got != before {
		t.Fatalf("entries = %d, want %d (no sweep below the threshold)", got, before)
	}

	// ちょうど上限に達したら掃除する
	l.accounts["one"] = &limitEntry{windowStart: sweepNow.Add(-limiterWindow)}
	l.ips["two"] = &limitEntry{windowStart: sweepNow.Add(-limiterWindow)}
	l.sweep(sweepNow)
	if got := len(l.accounts) + len(l.ips); got != 0 {
		t.Fatalf("entries = %d, want 0 after the sweep", got)
	}
}

func TestLimiterFailureSweepsExpiredEntriesAndKeepsLockouts(t *testing.T) {
	l, _ := filledLimiter(limiterSweepAt / 2)
	l.accounts["locked"] = &limitEntry{failures: 5, windowStart: sweepNow.Add(-time.Hour), lockUntil: sweepNow.Add(10 * time.Minute), level: 4}

	l.Failure("fresh-user", "203.0.113.7")

	if len(l.accounts) != 2 || l.accounts["fresh-user"] == nil || l.accounts["locked"] == nil {
		t.Fatalf("%d accounts left after the sweep, want only fresh-user and locked", len(l.accounts))
	}
	if len(l.ips) != 1 || l.ips["203.0.113.7"] == nil {
		t.Fatalf("%d ips left after the sweep, want only the new one", len(l.ips))
	}
	if got := retryAfter(t, l.Check("locked", "198.51.100.1")); got != 10*time.Minute {
		t.Fatalf("an active lockout must survive the sweep, retry after = %v", got)
	}
	if err := l.Check("old-user-0", "old-ip-0"); err != nil {
		t.Fatalf("an expired record must not block: %v", err)
	}
}

func TestLimiterBelowThresholdKeepsExpiredEntriesUntilReused(t *testing.T) {
	l, _ := filledLimiter(5)
	l.Failure("fresh-user", "203.0.113.7")
	if len(l.accounts) != 6 || len(l.ips) != 6 {
		t.Fatalf("accounts = %d, ips = %d: small tables are not swept", len(l.accounts), len(l.ips))
	}
	// 窓が過ぎた記録は、再び失敗したときに数え直しから始まる(5回目で初めて締め出す)
	for i := 0; i < 4; i++ {
		l.Failure("old-user-0", "old-ip-0")
		if err := l.Check("old-user-0", "old-ip-0"); err != nil {
			t.Fatalf("failure %d must not lock yet: %v", i+1, err)
		}
	}
	l.Failure("old-user-0", "old-ip-0")
	if err := l.Check("old-user-0", "old-ip-0"); !errors.As(err, new(*TooManyAttemptsError)) {
		t.Fatalf("5th failure must lock: %v", err)
	}
}
