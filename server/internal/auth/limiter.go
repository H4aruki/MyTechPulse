package auth

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ログイン試行制限の既定値。
const (
	// 利用者名ごと: 15分の間に5回失敗すると、1分・2分・4分…と倍々に伸びる締め出しに入る（最大15分）。
	accountMaxFailures = 5
	// IPごと: 15分の間に20回失敗すると、窓が終わるまで締め出す。
	ipMaxFailures = 20
	// 失敗を数える窓。
	limiterWindow = 15 * time.Minute
	// 締め出しの最初の長さと上限。
	lockoutBase = time.Minute
	lockoutMax  = 15 * time.Minute
	// 追跡する件数の上限。超えたら期限切れを掃除する。
	limiterSweepAt = 10000
)

// TooManyAttemptsError はログイン試行が多すぎて受け付けないことを表す。
type TooManyAttemptsError struct{ RetryAfter time.Duration }

func (e *TooManyAttemptsError) Error() string { return "auth: too many login attempts" }

type limitEntry struct {
	failures    int
	windowStart time.Time
	lockUntil   time.Time
	level       int
}

// LoginLimiter はログイン失敗の回数を、利用者名ごとと接続元IPごとに数える。
// プロセス内のメモリだけで持つ（API は1台構成）。再起動すると数え直しになる。
type LoginLimiter struct {
	Clock Clock

	mu       sync.Mutex
	accounts map[string]*limitEntry
	ips      map[string]*limitEntry
}

// NewLoginLimiter は制限器を作る。
func NewLoginLimiter(clock Clock) *LoginLimiter {
	return &LoginLimiter{Clock: clock, accounts: map[string]*limitEntry{}, ips: map[string]*limitEntry{}}
}

// accountKey は利用者の有無によらず同じ規則で作る（存在の有無を応答から読み取らせないため）。
func accountKey(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

type clientIPKey struct{}

// WithClientIP は接続元IPを処理の文脈へ載せる。
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// Check は照合の前に呼ぶ。締め出し中なら待ち時間つきのエラーを返す。
func (l *LoginLimiter) Check(username, ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	var wait time.Duration
	if e := l.accounts[accountKey(username)]; e != nil && now.Before(e.lockUntil) {
		wait = e.lockUntil.Sub(now)
	}
	if e := l.ips[ip]; e != nil && e.failures >= ipMaxFailures && now.Before(e.windowStart.Add(limiterWindow)) {
		if w := e.windowStart.Add(limiterWindow).Sub(now); w > wait {
			wait = w
		}
	}
	if wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	return nil
}

// Failure は認証に失敗したときに呼ぶ。
func (l *LoginLimiter) Failure(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	l.sweep(now)

	a := entryFor(l.accounts, accountKey(username), now)
	a.failures++
	// 利用者名は「最後の失敗から15分」で数え直す。続けて狙われている間は、締め出しの伸びが戻らない
	a.windowStart = now
	if a.failures >= accountMaxFailures {
		lock := lockoutBase << a.level
		if lock > lockoutMax || lock <= 0 {
			lock = lockoutMax
		}
		a.lockUntil = now.Add(lock)
		a.windowStart = a.lockUntil // 締め出しが明けてから15分、失敗が無ければ数え直す
		if a.level < 10 {
			a.level++
		}
	}
	entryFor(l.ips, ip, now).failures++
}

// Success は認証に成功したときに呼ぶ。利用者名の数え直しだけ行う（IPの失敗は残す）。
func (l *LoginLimiter) Success(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.accounts, accountKey(username))
}

// entryFor は窓が過ぎて締め出しも終わっていれば数え直した記録を返す。
func entryFor(m map[string]*limitEntry, key string, now time.Time) *limitEntry {
	e := m[key]
	if e == nil || (!now.Before(e.windowStart.Add(limiterWindow)) && !now.Before(e.lockUntil)) {
		e = &limitEntry{windowStart: now}
		m[key] = e
	}
	return e
}

func (l *LoginLimiter) sweep(now time.Time) {
	if len(l.accounts)+len(l.ips) < limiterSweepAt {
		return
	}
	for _, m := range []map[string]*limitEntry{l.accounts, l.ips} {
		for k, e := range m {
			if !now.Before(e.windowStart.Add(limiterWindow)) && !now.Before(e.lockUntil) {
				delete(m, k)
			}
		}
	}
}
