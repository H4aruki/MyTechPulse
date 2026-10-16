package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// パスキーのログインが使う公開部品(IPだけの制限・セッション発行・Cookie・エラー変換)を確かめる。

func TestLimiterCheckIPLocksOutAfterTwentyFailuresAndEndsWithTheWindow(t *testing.T) {
	clock := &fakeClock{now: sweepNow}
	l := NewLoginLimiter(clock)
	for i := 0; i < 19; i++ {
		l.FailureIP("9.9.9.9")
	}
	if err := l.CheckIP("9.9.9.9"); err != nil {
		t.Fatalf("19回ではまだ使える: %v", err)
	}
	l.FailureIP("9.9.9.9")
	clock.now = clock.now.Add(time.Minute)
	var tm *TooManyAttemptsError
	if err := l.CheckIP("9.9.9.9"); !errors.As(err, &tm) || tm.RetryAfter != 14*time.Minute {
		t.Fatalf("20回で窓が終わるまで締め出す: %v", err)
	}
	if err := l.CheckIP("8.8.8.8"); err != nil {
		t.Fatalf("別のIPは影響を受けない: %v", err)
	}
	clock.now = sweepNow.Add(limiterWindow)
	if err := l.CheckIP("9.9.9.9"); err != nil {
		t.Fatalf("窓が終われば使える: %v", err)
	}
}

func TestLimiterFailureIPAndPasswordFailuresShareOneIPBucket(t *testing.T) {
	l := NewLoginLimiter(&fakeClock{now: sweepNow})
	for i := 0; i < 10; i++ {
		l.Failure("user"+string(rune('a'+i)), "9.9.9.9") // パスワードの失敗(利用者名は別々で、利用者名ごとの上限には届かない)
		l.FailureIP("9.9.9.9")                           // パスキーの失敗
	}
	if err := l.CheckIP("9.9.9.9"); err == nil {
		t.Fatal("合算して20回で、パスキーの側も締め出す")
	}
	if err := l.Check("someone-else", "9.9.9.9"); err == nil {
		t.Fatal("パスキーの失敗は、パスワードの側の締め出しにも効く")
	}
}

func TestLimiterFailureIPCreatesNoAccountEntry(t *testing.T) {
	l := NewLoginLimiter(&fakeClock{now: sweepNow})
	for i := 0; i < ipMaxFailures+5; i++ {
		l.FailureIP("9.9.9.9")
	}
	if len(l.accounts) != 0 {
		t.Fatalf("利用者名の記録を作ってはいけない(空の利用者名に全員の失敗が集まる): %d", len(l.accounts))
	}
	// 実際に、利用者名の締め出しは誰にもかからない
	if err := l.Check("alice", "1.1.1.1"); err != nil {
		t.Fatalf("別のIPの利用者は影響を受けない: %v", err)
	}
	if err := l.Check("", "1.1.1.1"); err != nil {
		t.Fatalf("空の利用者名にも影響しない: %v", err)
	}
}

func TestLimiterSuccessKeepsIPFailures(t *testing.T) {
	l := NewLoginLimiter(&fakeClock{now: sweepNow})
	for i := 0; i < ipMaxFailures; i++ {
		l.FailureIP("9.9.9.9")
	}
	l.Success("alice")
	if err := l.CheckIP("9.9.9.9"); err == nil {
		t.Fatal("成功してもIPの失敗は残す")
	}
}

func TestLimiterCheckIPSweepsLikeFailure(t *testing.T) {
	l, _ := filledLimiter(limiterSweepAt / 2)
	l.FailureIP("new-ip")
	if len(l.accounts)+len(l.ips) >= limiterSweepAt {
		t.Fatalf("FailureIP でも期限切れの記録を掃除する: %d", len(l.accounts)+len(l.ips))
	}
}

func TestLimiterAllowBeginLimitsPerIPAndRecoversWithTheWindow(t *testing.T) {
	clock := &fakeClock{now: sweepNow}
	l := NewLoginLimiter(clock)
	for i := 0; i < ipMaxBegins; i++ {
		if err := l.AllowBegin("9.9.9.9"); err != nil {
			t.Fatalf("%d回目は通る: %v", i+1, err)
		}
	}
	clock.now = clock.now.Add(time.Minute)
	var tm *TooManyAttemptsError
	if err := l.AllowBegin("9.9.9.9"); !errors.As(err, &tm) || tm.RetryAfter != 14*time.Minute {
		t.Fatalf("上限の次は、窓が終わるまで待たせる: %v", err)
	}
	if err := l.AllowBegin("8.8.8.8"); err != nil {
		t.Fatalf("別のIPは影響を受けない: %v", err)
	}
	clock.now = sweepNow.Add(limiterWindow)
	if err := l.AllowBegin("9.9.9.9"); err != nil {
		t.Fatalf("窓が終われば使える: %v", err)
	}
}

func TestLimiterAllowBeginIsSeparateFromFailureCounting(t *testing.T) {
	l := NewLoginLimiter(&fakeClock{now: sweepNow})
	for i := 0; i < ipMaxBegins+5; i++ {
		_ = l.AllowBegin("9.9.9.9")
	}
	if err := l.CheckIP("9.9.9.9"); err != nil {
		t.Fatalf("開始の枠を使い切っても、失敗の締め出しにはならない: %v", err)
	}
	if err := l.Check("alice", "9.9.9.9"); err != nil {
		t.Fatalf("パスワードの側にも効かない: %v", err)
	}

	m := NewLoginLimiter(&fakeClock{now: sweepNow})
	for i := 0; i < ipMaxFailures; i++ {
		m.FailureIP("9.9.9.9")
	}
	if err := m.AllowBegin("9.9.9.9"); err != nil {
		t.Fatalf("失敗の数は、開始の枠を減らさない(締め出しの判断は CheckIP): %v", err)
	}
}

func TestLimiterAllowBeginIsSweptLikeOtherEntries(t *testing.T) {
	l, _ := filledLimiter(limiterSweepAt / 2)
	l.begins["old-ip"] = &limitEntry{failures: 1, windowStart: sweepNow.Add(-2 * limiterWindow)}
	if err := l.AllowBegin("new-ip"); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.begins["old-ip"]; ok {
		t.Error("期限切れの開始の記録も掃除する")
	}
}

func TestStartSessionIssuesTheSameSessionAsLogin(t *testing.T) {
	h := newHarness()
	h.store.users["alice"] = fakeUser{User: User{ID: 7, Username: "alice", Role: RoleMember}, hash: "hash:pw"}
	h.store.cleanupCalls = nil

	user, plain, expires, err := h.svc.StartSession(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if user != (User{ID: 7, Username: "alice", Role: RoleMember}) {
		t.Fatalf("user = %+v", user)
	}
	if want := h.clock.now.Add(24 * time.Hour); !expires.Equal(want) {
		t.Fatalf("expires = %v, want %v(SessionTTL)", expires, want)
	}
	if len(h.store.cleanupCalls) != 1 || !h.store.cleanupCalls[0].Equal(h.clock.now) {
		t.Fatalf("期限切れセッションを清掃する: %v", h.store.cleanupCalls)
	}
	if plain != "token-b" {
		t.Fatalf("TokenGenerator のトークンを使う: %q", plain)
	}
	if h.store.sessions[HashToken(plain)].User.ID != 7 {
		t.Fatal("セッションはトークンのハッシュで保存される")
	}
	if s, err := h.svc.Authenticate(context.Background(), plain); err != nil || s.User.ID != 7 {
		t.Fatalf("発行したセッションで本人確認できる: %+v %v", s, err)
	}

	// パスワードログインの結果と同じ形(同じ期限・同じ清掃)
	_, loginPlain, loginExpires, err := h.svc.Login(context.Background(), "alice", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if !loginExpires.Equal(expires) || loginPlain == plain {
		t.Fatalf("Login = %q %v, StartSession = %q %v", loginPlain, loginExpires, plain, expires)
	}
}

func TestStartSessionDoesNotTouchTheLimiter(t *testing.T) {
	h := newHarness()
	h.store.users["alice"] = fakeUser{User: User{ID: 7, Username: "alice", Role: RoleMember}, hash: "hash:pw"}
	h.svc.Limiter = NewLoginLimiter(h.clock)
	for i := 0; i < accountMaxFailures; i++ {
		h.svc.Limiter.Failure("alice", "1.1.1.1")
	}
	if _, _, _, err := h.svc.StartSession(context.Background(), 7); err != nil {
		t.Fatalf("パスワードで締め出された利用者も、パスキーでは入れる: %v", err)
	}
	if err := h.svc.Limiter.Check("alice", "2.2.2.2"); err == nil {
		t.Fatal("利用者名の締め出しは解除しない(パスワードの側の話)")
	}
}

func TestStartSessionUnknownUserIsInvalidCredentials(t *testing.T) {
	h := newHarness()
	user, plain, expires, err := h.svc.StartSession(context.Background(), 99)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
	assertNothingIssued(t, user, plain, expires)
	if len(h.store.sessions) != 0 {
		t.Fatal("セッションを作ってはいけない")
	}
}

func TestStartSessionFailuresIssueNothingAndHideCause(t *testing.T) {
	cases := map[string]func(*faultyStore, *harness){
		"利用者の検索":   func(f *faultyStore, _ *harness) { f.findByIDErr = errDB },
		"セッションの保存": func(f *faultyStore, _ *harness) { f.createSessionErr = errDB },
		"トークンの生成":  func(_ *faultyStore, h *harness) { h.svc.Tokens = errTokens{err: errDB} },
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			h, f := faultHarness()
			inject(f, h)
			user, plain, expires, err := h.svc.StartSession(context.Background(), 1)
			if err == nil || errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("内部の失敗は、認証の失敗と区別する: %v", err)
			}
			assertNothingIssued(t, user, plain, expires)
			if name == "トークンの生成" && strings.Contains(err.Error(), "secret-detail") {
				t.Fatalf("トークン生成の原因を返してはいけない: %v", err)
			}
		})
	}
}

func TestStartSessionCleanupFailureDoesNotStopTheLogin(t *testing.T) {
	h, _ := faultHarness()
	h.store.cleanupErr = errDB
	if _, _, _, err := h.svc.StartSession(context.Background(), 1); err != nil {
		t.Fatalf("清掃だけの失敗でログインを止めない: %v", err)
	}
	if !strings.Contains(h.logs.String(), "expired session cleanup failed") || strings.Contains(h.logs.String(), "secret-detail") {
		t.Fatalf("警告だけを記録する: %s", h.logs.String())
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	expires := sweepNow.Add(24 * time.Hour)
	local := SessionCookie("mtp_session", false, 24*time.Hour, "tok", expires)
	if local.Name != "mtp_session" || local.Value != "tok" || local.Path != "/" || local.MaxAge != 86400 ||
		!local.Expires.Equal(expires) || !local.HttpOnly || local.Secure || local.SameSite != http.SameSiteLaxMode {
		t.Fatalf("local = %+v", local)
	}
	prod := SessionCookie("__Host-mtp_session", true, time.Hour, "tok", expires)
	if prod.Name != "__Host-mtp_session" || !prod.Secure || !prod.HttpOnly || prod.MaxAge != 3600 || prod.Domain != "" || prod.Path != "/" {
		t.Fatalf("prod = %+v", prod)
	}
	// ハンドラーが発行するCookieと同じ
	h := Handler{Service: Service{SessionTTL: 24 * time.Hour}, CookieName: "mtp_session"}
	if got := h.sessionCookie("tok", expires); got.String() != local.String() {
		t.Fatalf("handler = %q, shared = %q", got.String(), local.String())
	}
}

func TestLoginProblemMapsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"認証の失敗", ErrInvalidCredentials, http.StatusUnauthorized},
		{"締め出し", &TooManyAttemptsError{RetryAfter: 1500 * time.Millisecond}, http.StatusTooManyRequests},
		{"内部の失敗", errDB, http.StatusInternalServerError},
	}
	for _, c := range cases {
		err := LoginProblem(c.err)
		var se interface{ GetStatus() int }
		if !errors.As(err, &se) || se.GetStatus() != c.code {
			t.Errorf("%s: %v", c.name, err)
		}
		if strings.Contains(err.Error(), "secret-detail") {
			t.Errorf("%s: 内部の詳細が出た: %v", c.name, err)
		}
	}
}
