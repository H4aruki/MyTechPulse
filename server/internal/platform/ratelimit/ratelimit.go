// Package ratelimit は、キーごとのトークンバケットで回数を制限する。標準ライブラリだけで書き、メモリ上だけで持つ。
// API は1台構成なので足りる。再起動すると数え直しになる。
package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

const (
	// 追跡するキーの数の上限。超えたら記録を1つ捨てる。
	maxKeys = 100000
	// 使われていない記録を掃除する間隔。
	sweepEvery = time.Minute
	epsilon    = 1e-9
)

// Message は429の本文に入れる利用者向けの説明。
const Message = "操作が多すぎます。しばらく待ってからもう一度お試しください"

// Clock は現在時刻。テストで進められるようにする。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter はキーごとに「最大 burst 回までまとめて、あとは every に1回ずつ」回復するバケットを持つ。
type Limiter struct {
	burst float64
	every time.Duration
	clock Clock

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// New は制限器を作る。バケットは burst 回分で始まり、every ごとに1回分ずつ回復する。clock が nil なら実際の時刻を使う。
func New(burst int, every time.Duration, clock Clock) *Limiter {
	if burst < 1 {
		burst = 1
	}
	if every <= 0 {
		every = time.Second
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &Limiter{burst: float64(burst), every: every, clock: clock, buckets: map[string]*bucket{}}
}

// PerWindow は「window の間に n 回」を、n 回までまとめて使え、window の間に使い切れる速さで回復する制限器にする。
func PerWindow(n int, window time.Duration, clock Clock) *Limiter {
	return New(n, window/time.Duration(max(n, 1)), clock)
}

// Allow は key の1回分を使えるなら (true, 0) を返す。使えないときは (false, 次に使えるまでの待ち時間) を返す。
// 使えなかった呼び出しは数えない。
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.touch(key, l.clock.Now())
	if b.tokens >= 1-epsilon { // 浮動小数点の誤差で、待ち時間ちょうどの呼び出しがはじかれないようにする
		b.tokens = math.Max(0, b.tokens-1)
		return true, 0
	}
	return false, l.untilOne(b)
}

// Wait は key の1回分を使えるまでの待ち時間を返す(使えるなら0)。回数は使わず、記録も作らない。
func (l *Limiter) Wait(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.sweep(now)
	b := l.buckets[key] // 無ければ(掃除で捨てられた＝満タンに戻った場合も)使える
	if b == nil {
		return 0
	}
	l.refill(b, now)
	if b.tokens >= 1-epsilon {
		return 0
	}
	return l.untilOne(b)
}

// Take は key の1回分を、残りがあるかどうかにかかわらず使う(0より下には減らさない)。
// 処理の結果が分かってから数えるときに使う。記録が無ければ満タンで作ってから使う。
func (l *Limiter) Take(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.touch(key, l.clock.Now())
	b.tokens = math.Max(0, b.tokens-1)
}

// touch は key の記録を(無ければ作って)返し、回復を反映する。呼び出し側が l.mu を持つこと。
func (l *Limiter) touch(key string, now time.Time) *bucket {
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxKeys {
			// まず満タンに戻った記録を、掃除の間隔を待たずに捨てる(全件を見るので、多くても1秒に1回)。
			// それでも上限なら、メモリを増やさないために1つ捨てる
			if now.Sub(l.lastSweep) >= time.Second {
				l.lastSweep = time.Time{}
				l.sweep(now)
			}
		}
		if len(l.buckets) >= maxKeys {
			for k := range l.buckets { // 順序は問わない
				delete(l.buckets, k)
				break
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	l.refill(b, now)
	return b
}

// refill は経過した時間の分だけ b を回復させる。呼び出し側が l.mu を持つこと。
func (l *Limiter) refill(b *bucket, now time.Time) {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+float64(elapsed)/float64(l.every))
	}
	b.last = now
}

// untilOne は b が1回分に戻るまでの待ち時間。
func (l *Limiter) untilOne(b *bucket) time.Duration {
	// マイクロ秒に丸めるのは、浮動小数点の誤差で1ナノ秒余計に待たせて、Retry-After が1秒ずれるのを防ぐため
	return time.Duration(math.Ceil((1 - b.tokens) * float64(l.every))).Round(time.Microsecond)
}

// sweep は満タンに戻った(＝使われていない)記録を捨てる。呼び出し側が l.mu を持つこと。
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if b.tokens+float64(now.Sub(b.last))/float64(l.every) >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// Len は追跡しているキーの数(テスト用)。
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// RetryAfterSeconds は待ち時間を、Retry-After に入れる秒(切り上げ、最小1)にする。
func RetryAfterSeconds(wait time.Duration) int {
	return max(int(math.Ceil(wait.Seconds())), 1)
}

// Check は l が nil でなければ key を数え、使えないときは 429 と Retry-After(秒)つきのエラーを返す(Humaの処理用)。
func (l *Limiter) Check(key string) error {
	if l == nil {
		return nil
	}
	if ok, wait := l.Allow(key); !ok {
		return Problem(wait)
	}
	return nil
}

// CheckUser は利用者IDごとに Check する。
func (l *Limiter) CheckUser(userID int64) error {
	if l == nil {
		return nil
	}
	return l.Check(strconv.FormatInt(userID, 10))
}

// Problem は 429 too_many_requests と Retry-After(秒)を返すエラーを作る。ログインの締め出しと同じ形。
func Problem(wait time.Duration) error {
	return huma.ErrorWithHeaders(
		httpx.NewProblem(http.StatusTooManyRequests, "too_many_requests", Message),
		http.Header{"Retry-After": {strconv.Itoa(RetryAfterSeconds(wait))}})
}

// WriteHTTP は Huma を通らない経路で、Problem に当たる 429 と Retry-After(秒)を書き出す。
func WriteHTTP(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds(wait)))
	httpx.WriteProblem(w, httpx.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(http.StatusTooManyRequests),
		Status: http.StatusTooManyRequests,
		Detail: Message,
		Code:   "too_many_requests",
	})
}
