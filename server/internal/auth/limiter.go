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
	// パスキーのログイン開始: IPごとに15分で60回まで。開始は未ログインで呼べてDBへ書くので、失敗とは別に数える。
	ipMaxBegins = 60
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
	// begins はパスキーのログイン開始の回数(IPごと)。失敗の数え方(ips)とは別のカウンタ。
	begins map[string]*limitEntry
}

// NewLoginLimiter は制限器を作る。
func NewLoginLimiter(clock Clock) *LoginLimiter {
	return &LoginLimiter{Clock: clock, accounts: map[string]*limitEntry{}, ips: map[string]*limitEntry{}, begins: map[string]*limitEntry{}}
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

// ClientIPFrom は ClientIPMiddleware が載せた接続元IPを返す。
func ClientIPFrom(ctx context.Context) string { return clientIPFrom(ctx) }

// Check は照合の前に呼ぶ。締め出し中なら待ち時間つきのエラーを返す。
func (l *LoginLimiter) Check(username, ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	var wait time.Duration
	if e := l.accounts[accountKey(username)]; e != nil && now.Before(e.lockUntil) {
		wait = e.lockUntil.Sub(now)
	}
	if w := l.ipWait(ip, now); w > wait {
		wait = w
	}
	if wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	return nil
}

// CheckIP は利用者名が分からない照合(パスキー)の前に呼ぶ。IPの窓で締め出し中なら待ち時間つきのエラーを返す。
func (l *LoginLimiter) CheckIP(ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if wait := l.ipWait(ip, l.Clock.Now()); wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	return nil
}

// AllowBegin はパスキーのログイン開始のたびに呼ぶ。IPごとに最初の呼び出しから15分の窓で数え、
// 上限を超える呼び出しには待ち時間つきのエラーを返す。成功・失敗では戻さず、失敗の数え方(Check/Failure)とは別に数える。
// 上限を超えた呼び出しは数えない。
func (l *LoginLimiter) AllowBegin(ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	l.sweep(now)
	e := entryFor(l.begins, ip, now)
	if e.failures >= ipMaxBegins {
		return &TooManyAttemptsError{RetryAfter: e.windowStart.Add(limiterWindow).Sub(now)}
	}
	e.failures++
	return nil
}

// ipWait はIPが締め出し中ならその残り時間を、そうでなければ0を返す。呼び出し側が l.mu を持つこと。
func (l *LoginLimiter) ipWait(ip string, now time.Time) time.Duration {
	e := l.ips[ip]
	if e == nil || e.failures < ipMaxFailures || !now.Before(e.windowStart.Add(limiterWindow)) {
		return 0
	}
	return e.windowStart.Add(limiterWindow).Sub(now)
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

// FailureIP は利用者名が分からない照合(パスキー)に失敗したときに呼ぶ。IPの失敗だけを数える。
// 空の利用者名で Failure を呼ぶと、全員の失敗が1つの記録に集まって全員が締め出されるため使わない。
func (l *LoginLimiter) FailureIP(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	l.sweep(now)
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
	if len(l.accounts)+len(l.ips)+len(l.begins) < limiterSweepAt {
		return
	}
	for _, m := range []map[string]*limitEntry{l.accounts, l.ips, l.begins} {
		for k, e := range m {
			if !now.Before(e.windowStart.Add(limiterWindow)) && !now.Before(e.lockUntil) {
				delete(m, k)
			}
		}
	}
}
