package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
		wantMsg  string // 空なら成功
	}{
		{"short 11 chars", "alice", strings.Repeat("x", 11), "12文字以上"},
		{"12 chars ok", "alice", "x7Kp-2mQ9vLz", ""},
		{"12 multibyte chars ok", "alice", strings.Repeat("あ", 12), ""},
		{"11 multibyte chars rejected", "alice", strings.Repeat("あ", 11), "12文字以上"},
		{"72 bytes ok", "alice", strings.Repeat("a", 72), ""},
		{"73 bytes rejected", "alice", strings.Repeat("a", 73), "72バイト"},
		{"common", "alice", "password1234", "よく使われる"},
		{"common mixed case", "alice", "PassWord1234", "よく使われる"},
		{"same as username", "alice-the-user", "alice-the-user", "利用者名"},
		{"same as username ignoring case", "Alice-The-User", "alice-the-user", "利用者名"},
		{"no composition rule", "alice", "alllowercaseletters", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := validateNewPassword(tt.password, tt.username)
			if tt.wantMsg == "" {
				if got != nil {
					t.Fatalf("unexpected error: %+v", got)
				}
				return
			}
			if got == nil || got.Field != "password" || !strings.Contains(got.Message, tt.wantMsg) {
				t.Fatalf("got %+v, want message containing %q", got, tt.wantMsg)
			}
		})
	}
}

func TestCommonPasswordListIsUsable(t *testing.T) {
	set := commonPasswordSet()
	if len(set) < 100 {
		t.Fatalf("list too small: %d", len(set))
	}
	for p := range set {
		if p != strings.ToLower(p) {
			t.Fatalf("entry %q must be lower case", p)
		}
	}
}

func TestSignupAppliesPolicy(t *testing.T) {
	h := newHarness()
	in := validSignup()
	in.Password = "short-pw"
	_, _, _, err := h.svc.Signup(context.Background(), in)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields[0].Field != "password" {
		t.Fatalf("err = %v", err)
	}
	in.Password = "qwerty123456"
	if _, _, _, err := h.svc.Signup(context.Background(), in); !errors.As(err, &ve) {
		t.Fatalf("common password must be rejected: %v", err)
	}
	in.Password = "alice"
	in.Username = "alice"
	if _, _, _, err := h.svc.Signup(context.Background(), in); !errors.As(err, &ve) {
		t.Fatalf("password == username must be rejected: %v", err)
	}
	in.Username, in.Password = "alice", "x7Kp-2mQ9vLz"
	if _, _, _, err := h.svc.Signup(context.Background(), in); err != nil {
		t.Fatalf("12 chars must pass: %v", err)
	}
}

// 方針ができる前に作られた短いパスワードの利用者も、ログインできる。
func TestLoginDoesNotApplyPolicy(t *testing.T) {
	h := newHarness()
	h.store.users["old"] = fakeUser{User: User{ID: 7, Username: "old", Role: RoleMember}, hash: "hash:abc"}
	if _, _, _, err := h.svc.Login(context.Background(), "old", "abc"); err != nil {
		t.Fatalf("existing short password must still log in: %v", err)
	}
}

func limiterHarness() (*harness, *LoginLimiter) {
	h := newHarness()
	l := NewLoginLimiter(h.clock)
	h.svc.Limiter = l
	h.store.users["alice"] = fakeUser{User: User{ID: 1, Username: "alice", Role: RoleMember}, hash: "hash:right-password-1"}
	return h, l
}

func loginAs(h *harness, ip, user, pw string) error {
	_, _, _, err := h.svc.Login(WithClientIP(context.Background(), ip), user, pw)
	return err
}

func retryAfter(t *testing.T, err error) time.Duration {
	t.Helper()
	var tm *TooManyAttemptsError
	if !errors.As(err, &tm) {
		t.Fatalf("want TooManyAttemptsError, got %v", err)
	}
	return tm.RetryAfter
}

func TestLimiterAccountLockoutBacksOffExponentially(t *testing.T) {
	h, _ := limiterHarness()
	for i := 0; i < 5; i++ {
		if err := loginAs(h, "1.1.1.1", "alice", "bad"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// 5回目の失敗で1分の締め出し。正しいパスワードでも通らない
	if got := retryAfter(t, loginAs(h, "1.1.1.1", "alice", "right-password-1")); got != time.Minute {
		t.Fatalf("first lockout = %v", got)
	}
	h.clock.now = h.clock.now.Add(30 * time.Second)
	if got := retryAfter(t, loginAs(h, "1.1.1.1", "alice", "bad")); got != 30*time.Second {
		t.Fatalf("remaining = %v", got)
	}
	// 1分たつと再び試せるが、もう1回失敗すると2分、4分、8分、15分(上限)
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	h.clock.now = h.clock.now.Add(30 * time.Second)
	for i, w := range want {
		if err := loginAs(h, "1.1.1.1", "alice", "bad"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("step %d: %v", i, err)
		}
		if got := retryAfter(t, loginAs(h, "1.1.1.1", "alice", "bad")); got != w {
			t.Fatalf("step %d lockout = %v, want %v", i, got, w)
		}
		h.clock.now = h.clock.now.Add(w)
	}
}

func TestLimiterWindowExpiryForgetsOldFailures(t *testing.T) {
	h, _ := limiterHarness()
	for i := 0; i < 4; i++ {
		_ = loginAs(h, "1.1.1.1", "alice", "bad")
	}
	h.clock.now = h.clock.now.Add(15 * time.Minute)
	for i := 0; i < 4; i++ {
		if err := loginAs(h, "1.1.1.1", "alice", "bad"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("old failures must be forgotten: %v", err)
		}
	}
}

func TestLimiterSuccessResetsAccountCounter(t *testing.T) {
	h, _ := limiterHarness()
	for i := 0; i < 4; i++ {
		_ = loginAs(h, "1.1.1.1", "alice", "bad")
	}
	if err := loginAs(h, "1.1.1.1", "alice", "right-password-1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := loginAs(h, "1.1.1.1", "alice", "bad"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("counter must restart after success: %v", err)
		}
	}
}

func TestLimiterIPLimit(t *testing.T) {
	h, _ := limiterHarness()
	// 別々の利用者名で20回失敗する(利用者名ごとの上限には届かない)
	for i := 0; i < 20; i++ {
		if err := loginAs(h, "9.9.9.9", "user"+string(rune('a'+i)), "bad"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if got := retryAfter(t, loginAs(h, "9.9.9.9", "alice", "right-password-1")); got != 15*time.Minute {
		t.Fatalf("ip lockout = %v", got)
	}
	// 別のIPは影響を受けない
	if err := loginAs(h, "8.8.8.8", "alice", "right-password-1"); err != nil {
		t.Fatalf("other ip: %v", err)
	}
	h.clock.now = h.clock.now.Add(15 * time.Minute)
	if err := loginAs(h, "9.9.9.9", "alice", "right-password-1"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestLimiterSameBehaviorForUnknownUser(t *testing.T) {
	h, _ := limiterHarness()
	for i := 0; i < 5; i++ {
		_ = loginAs(h, "1.1.1.1", "nobody", "bad")
		_ = loginAs(h, "2.2.2.2", "alice", "bad")
	}
	ghost := retryAfter(t, loginAs(h, "3.3.3.3", "nobody", "bad"))
	real := retryAfter(t, loginAs(h, "4.4.4.4", "alice", "bad"))
	if ghost != real || ghost != time.Minute {
		t.Fatalf("unknown = %v, existing = %v", ghost, real)
	}
	// 利用者名の大文字小文字違いでも同じ数えになる
	if err := loginAs(h, "5.5.5.5", " ALICE ", "bad"); retryAfter(t, err) == 0 {
		t.Fatal("case/space variants must share the counter")
	}
}

func TestLimiterValidationErrorsAreNotCounted(t *testing.T) {
	h, _ := limiterHarness()
	for i := 0; i < 30; i++ {
		_ = loginAs(h, "1.1.1.1", "alice", "")
	}
	if err := loginAs(h, "1.1.1.1", "alice", "right-password-1"); err != nil {
		t.Fatalf("invalid input must not count: %v", err)
	}
}

func TestHandlerLoginReturns429WithRetryAfter(t *testing.T) {
	httpx.UseProblemErrors()
	h := newHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.CreateHooks = nil
	Handler{Service: h.svc, CookieName: "mtp_session"}.Register(humago.New(mux, cfg))
	a := &apiHarness{harness: h, mux: mux}

	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	bad := `{"username":"synthetic-user","password":"wrong-password"}`
	for i := 0; i < 5; i++ {
		assertProblem(t, a.do(http.MethodPost, "/api/v1/auth/login", bad), http.StatusUnauthorized, "invalid_credentials")
	}
	rec := a.do(http.MethodPost, "/api/v1/auth/login", loginBody)
	assertProblem(t, rec, http.StatusTooManyRequests, "too_many_requests")
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q", got)
	}
	if len(setCookies(rec)) != 0 {
		t.Fatal("429 must not set a cookie")
	}
}

func TestClientIPSelection(t *testing.T) {
	if got := clientIP("203.0.113.5 ", "10.0.0.2:5555"); got != "203.0.113.5" {
		t.Fatalf("header = %q", got)
	}
	if got := clientIP("", "10.0.0.2:5555"); got != "10.0.0.2" {
		t.Fatalf("remote = %q", got)
	}
	if got := clientIP("", "[::1]:80"); got != "::1" {
		t.Fatalf("ipv6 = %q", got)
	}
}
