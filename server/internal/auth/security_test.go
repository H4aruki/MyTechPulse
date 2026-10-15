package auth

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// newBcryptHarness は、パスワードだけ本物のbcryptを使う。
// 既存のfakePasswordsは「hash:」+平文という元に戻せる偽物なので、不可逆性はこちらで確かめる。
func newBcryptHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness()
	h.svc.Passwords = BcryptPasswords{}
	dummy, err := NewDummyHash(BcryptPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	h.svc.DummyHash = dummy
	return h
}

func TestSignupStoresIrreversiblePasswordHash(t *testing.T) {
	h := newBcryptHarness(t)
	const password = "synthetic-plain-pass"
	in := SignupInput{Username: "alice", Password: password, FavoriteTags: []string{"go"}}
	if _, _, _, err := h.svc.Signup(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	stored := h.store.users["alice"].hash
	if stored == "" || stored == password || strings.Contains(stored, password) {
		t.Fatalf("plaintext password must not be stored: %q", stored)
	}
	if !strings.HasPrefix(stored, "$2") {
		t.Fatalf("stored value is not a bcrypt hash: %q", stored)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)); err != nil {
		t.Fatalf("stored hash must verify the original password: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password+"x")); err == nil {
		t.Fatal("stored hash must reject a different password")
	}
	// ソルトが毎回変わるので、同じパスワードでもハッシュは一致しない
	again, err := BcryptPasswords{}.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if again == stored {
		t.Fatal("same password must produce different hashes (salt)")
	}
}

func TestLoginUnknownUserAndWrongPasswordAreIdenticalWithBcrypt(t *testing.T) {
	h := newBcryptHarness(t)
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}
	sessionsBefore := len(h.store.sessions)

	uMissing, tokMissing, expMissing, errMissing := h.svc.Login(context.Background(), "nobody", "pass-word-long-ok")
	uWrong, tokWrong, expWrong, errWrong := h.svc.Login(context.Background(), "alice", "wrong-pass")

	if errMissing == nil || errWrong == nil {
		t.Fatalf("both must fail: %v / %v", errMissing, errWrong)
	}
	if errMissing != errWrong { //nolint:errorlint // 同一の値であること自体が仕様
		t.Fatalf("errors must be the very same value: %v / %v", errMissing, errWrong)
	}
	if !errors.Is(errMissing, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", errMissing)
	}
	if uMissing != (User{}) || uWrong != (User{}) || tokMissing != "" || tokWrong != "" ||
		!expMissing.IsZero() || !expWrong.IsZero() {
		t.Fatal("failed logins must return no user, token or expiry")
	}
	if len(h.store.sessions) != sessionsBefore {
		t.Fatal("failed logins must not create sessions")
	}
}

func TestHandlerLoginFailureResponsesAreIdenticalIncludingHeaders(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)

	missing := a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"nobody","password":"synthetic-pass-12"}`)
	wrong := a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"synthetic-user","password":"wrong-password"}`)

	if missing.Code != http.StatusUnauthorized || wrong.Code != missing.Code {
		t.Fatalf("status = %d / %d, want both 401", missing.Code, wrong.Code)
	}
	if !reflect.DeepEqual(missing.Header(), wrong.Header()) {
		t.Fatalf("headers differ:\n%v\n%v", missing.Header(), wrong.Header())
	}
	if missing.Body.String() != wrong.Body.String() {
		t.Fatalf("bodies differ:\n%s\n%s", missing.Body.String(), wrong.Body.String())
	}
	// 本文から、利用者名の存在や入力値は読み取れない
	for _, leak := range []string{"nobody", "synthetic-user", "synthetic-pass", "wrong-password"} {
		if strings.Contains(missing.Body.String(), leak) {
			t.Fatalf("response must not contain %q: %s", leak, missing.Body.String())
		}
	}
}

func TestSessionsAreCreatedWithExpiryAndStoredByTokenHash(t *testing.T) {
	h := newHarness()
	h.svc.SessionTTL = 90 * time.Minute
	want := h.clock.now.Add(90 * time.Minute)

	_, signupTok, signupExp, err := h.svc.Signup(context.Background(), validSignup())
	if err != nil {
		t.Fatal(err)
	}
	_, loginTok, loginExp, err := h.svc.Login(context.Background(), "alice", "pass-word-long-ok")
	if err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		token string
		exp   time.Time
	}{"signup": {signupTok, signupExp}, "login": {loginTok, loginExp}} {
		if !c.exp.Equal(want) {
			t.Fatalf("%s: returned expiry = %v, want now+TTL = %v", name, c.exp, want)
		}
		stored, ok := h.store.sessions[HashToken(c.token)]
		if !ok {
			t.Fatalf("%s: session must be stored under the SHA-256 of the token", name)
		}
		if !stored.ExpiresAt.Equal(want) {
			t.Fatalf("%s: stored expiry = %v, want %v", name, stored.ExpiresAt, want)
		}
	}
	if len(h.store.sessions) != 2 {
		t.Fatalf("want 2 sessions, have %d", len(h.store.sessions))
	}
}

func TestExpiredLoginSessionIsRejectedAndFreshLoginStillWorks(t *testing.T) {
	h := newHarness()
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}
	_, tok, exp, err := h.svc.Login(context.Background(), "alice", "pass-word-long-ok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	h.clock.now = exp.Add(-time.Nanosecond)
	if _, err := h.svc.Authenticate(ctx, tok); err != nil {
		t.Fatalf("session must be valid just before expiry: %v", err)
	}
	for _, at := range []time.Time{exp, exp.Add(time.Second), exp.Add(365 * 24 * time.Hour)} {
		h.clock.now = at
		if _, err := h.svc.Authenticate(ctx, tok); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("session must be rejected at %v: %v", at, err)
		}
	}
	// 期限切れの後でも、ログインし直せば新しいセッションが使える
	_, fresh, _, err := h.svc.Login(ctx, "alice", "pass-word-long-ok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Authenticate(ctx, fresh); err != nil {
		t.Fatalf("fresh session must authenticate: %v", err)
	}
}

func TestHandlerRejectsExpiredSessionCookieOnProtectedEndpoint(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	c := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))

	a.clock.now = a.clock.now.Add(24*time.Hour - time.Second)
	if rec := a.do(http.MethodGet, "/api/v1/auth/me", "", c); rec.Code != http.StatusOK {
		t.Fatalf("before expiry = %d", rec.Code)
	}
	a.clock.now = a.clock.now.Add(time.Second)
	rec := a.do(http.MethodGet, "/api/v1/auth/me", "", c)
	assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	if strings.Contains(rec.Body.String(), "synthetic-user") {
		t.Fatal("rejected response must not reveal the user")
	}
}
