package app

import (
	"bytes"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/ratelimit"
)

// 回数制限の数値(#263)。利用者数が少ない今の規模に合わせた控えめな値で、ふつうの画面操作では当たらない
// (フィードは5分は取り直さない、クリックは記事を開くたび1回、アンケートは3記事を開いたあとに1回、など)。
const (
	// 会員登録: 接続元IPごとに1時間に5回(成功・失敗とも数える)。
	signupPerHour = 5
	// /api/v1/ 全体の保険: 接続元IPごとに1分に300回。
	ipPerMinute = 300
	// 記事クリック: 利用者ごとに1分に60回、まとめ押しは20回まで。
	clicksPerMinute = 60
	clicksBurst     = 20
	// フィード取得: 利用者ごとに1分に30回(外部サイトを取りに行くので重い)。
	feedPerMinute = 30
	// アンケートの表示・回答・追加回答・閉じる: 利用者ごとに1分に10回。
	feedbackPerMinute = 10
	// パスキーの登録・名前変更・削除: 利用者ごとに1分に10回。
	passkeyManagePerMinute = 10

	// リクエスト本文の上限。Huma の既定(1MB)より小さくする。WebAuthn の登録・認証の本文も数KBに収まる。
	maxBodyBytes = 64 << 10

	signupPath = "/api/v1/auth/signup"
)

// limits は app が持つ回数制限器。
type limits struct {
	signupIP, globalIP                  *ratelimit.Limiter
	feed, click, userFeedback, passkeys *ratelimit.Limiter
}

func newLimits(clock ratelimit.Clock) limits {
	return limits{
		signupIP:     ratelimit.PerWindow(signupPerHour, time.Hour, clock),
		globalIP:     ratelimit.PerWindow(ipPerMinute, time.Minute, clock),
		feed:         ratelimit.PerWindow(feedPerMinute, time.Minute, clock),
		click:        ratelimit.New(clicksBurst, time.Minute/clicksPerMinute, clock),
		userFeedback: ratelimit.PerWindow(feedbackPerMinute, time.Minute, clock),
		passkeys:     ratelimit.PerWindow(passkeyManagePerMinute, time.Minute, clock),
	}
}

// ipKey は制限に使うIPの文字列。IPv6は1つの契約で多数のアドレスを使えるので、上位64ビット(/64)をまとめて数える。
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Is4() || addr.Is4In6() {
		return ip
	}
	if p, err := addr.Prefix(64); err == nil {
		return p.String()
	}
	return ip
}

// requestLimits は /api/v1/ の要求に、接続元IPごとの回数制限とリクエスト本文の上限を掛ける。
// 接続元IPは、ログインの制限と同じ規則(auth.ClientIPOfRequest)で決める。/health は対象外。
// CSRF の検査のあとに置く(他サイトから送らせた要求で、利用者の枠を使い切らせないため)。
func requestLimits(clientIPHeader string, l limits, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		key := ipKey(auth.ClientIPOfRequest(clientIPHeader, r))
		if ok, wait := l.globalIP.Allow(key); !ok {
			ratelimit.WriteHTTP(w, wait)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == signupPath {
			if ok, wait := l.signupIP.Allow(key); !ok {
				ratelimit.WriteHTTP(w, wait)
				return
			}
		}
		if !limitBody(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody は本文が maxBodyBytes を超えるときに 413 を返して false を返す。
// 長さが分からない(チャンク)本文は、上限+1バイトまで読んで確かめ、読んだ分を戻す。
func limitBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	if r.ContentLength > maxBodyBytes {
		writeTooLarge(w)
		return false
	}
	if r.ContentLength >= 0 {
		// 申告された長さより多くは読まれないが、念のため上限で包む
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		return true
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err == nil && len(data) > maxBodyBytes {
		writeTooLarge(w)
		return false
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), errReader{err}))
	return true
}

// errReader は読み込み中に起きたエラーを、読み終えたあとの読み込みで返す(エラーが無ければ EOF)。
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	return 0, io.EOF
}

func writeTooLarge(w http.ResponseWriter) {
	httpx.WriteProblem(w, httpx.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(http.StatusRequestEntityTooLarge),
		Status: http.StatusRequestEntityTooLarge,
		Detail: "リクエスト本文が大きすぎます",
		Code:   "payload_too_large",
	})
}
