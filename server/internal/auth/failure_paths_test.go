package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// 内部の失敗(DB障害・乱数や照合の失敗)の経路を確かめる。
// 既存の fakeStore に、失敗を差し込める層をかぶせて使う。

var errDB = errors.New("db: connection refused secret-detail")

type faultyStore struct {
	*fakeStore
	findByUsernameErr error
	createUserErr     error
	createSessionErr  error
	findUserErr       error
	deleteErr         error
	deleteCalls       int
}

func (f *faultyStore) FindByUsername(ctx context.Context, name string) (UserWithPassword, error) {
	if f.findByUsernameErr != nil {
		return UserWithPassword{}, f.findByUsernameErr
	}
	return f.fakeStore.FindByUsername(ctx, name)
}

func (f *faultyStore) CreateWithInterestsAndSession(ctx context.Context, name, hash string, role Role,
	tags []string, token [32]byte, exp time.Time) (User, error) {
	if f.createUserErr != nil {
		return User{}, f.createUserErr
	}
	return f.fakeStore.CreateWithInterestsAndSession(ctx, name, hash, role, tags, token, exp)
}

func (f *faultyStore) Create(ctx context.Context, token [32]byte, userID int64, exp time.Time) error {
	if f.createSessionErr != nil {
		return f.createSessionErr
	}
	return f.fakeStore.Create(ctx, token, userID, exp)
}

func (f *faultyStore) FindUser(ctx context.Context, token [32]byte, now time.Time) (Session, error) {
	if f.findUserErr != nil {
		return Session{}, f.findUserErr
	}
	return f.fakeStore.FindUser(ctx, token, now)
}

func (f *faultyStore) Delete(ctx context.Context, token [32]byte) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.fakeStore.Delete(ctx, token)
}

// faultHarness は alice(パスワード right-password-1)が登録済みで、失敗を差し込める harness を返す。
func faultHarness() (*harness, *faultyStore) {
	h := newHarness()
	f := &faultyStore{fakeStore: h.store}
	h.svc.Users, h.svc.Sessions = f, f
	h.store.users["alice"] = fakeUser{User: User{ID: 1, Username: "alice", Role: RoleMember}, hash: "hash:right-password-1"}
	return h, f
}

type errPasswords struct{ err error }

func (p errPasswords) Hash(string) (string, error)  { return "", p.err }
func (p errPasswords) Compare(string, string) error { return p.err }

type errTokens struct{ err error }

func (g errTokens) New() (string, [32]byte, error) { return "", [32]byte{}, g.err }

func assertNothingIssued(t *testing.T, user User, token string, exp time.Time) {
	t.Helper()
	if user != (User{}) || token != "" || !exp.IsZero() {
		t.Fatalf("a failure must return no user, token or expiry: %+v %q %v", user, token, exp)
	}
}

func TestSignupInternalFailuresCreateNothingAndHideCause(t *testing.T) {
	cases := map[string]func(*harness){
		"password hashing fails": func(h *harness) { h.svc.Passwords = errPasswords{errors.New("bcrypt: secret-detail")} },
		"token generation fails": func(h *harness) { h.svc.Tokens = errTokens{errors.New("rand: secret-detail")} },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			setup(h)
			user, tok, exp, err := h.svc.Signup(context.Background(), validSignup())
			if err == nil {
				t.Fatal("want error")
			}
			assertNothingIssued(t, user, tok, exp)
			var ve *ValidationError
			if errors.As(err, &ve) || errors.Is(err, ErrUsernameTaken) || errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("an internal failure must not look like a client error: %v", err)
			}
			if strings.Contains(err.Error(), "secret-detail") {
				t.Fatalf("cause must not leak: %v", err)
			}
			if len(h.store.users) != 0 || len(h.store.sessions) != 0 {
				t.Fatal("nothing may be stored when signup fails")
			}
		})
	}
}

func TestSignupPassesThroughStoreFailure(t *testing.T) {
	h, f := faultHarness()
	f.createUserErr = errDB
	in := validSignup()
	in.Username = "bob"
	user, tok, exp, err := h.svc.Signup(context.Background(), in)
	if !errors.Is(err, errDB) {
		t.Fatalf("store failure must reach the caller: %v", err)
	}
	assertNothingIssued(t, user, tok, exp)
}

func TestLoginStoreOutageIsNotCountedAsFailedAttempt(t *testing.T) {
	h, f := faultHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	f.findByUsernameErr = errDB
	// 締め出しの回数(5回)を超えて、DB障害のログインを繰り返す
	for i := 0; i < 10; i++ {
		user, tok, exp, err := h.svc.Login(WithClientIP(context.Background(), "1.1.1.1"), "alice", "right-password-1")
		if !errors.Is(err, errDB) {
			t.Fatalf("attempt %d: store failure must reach the caller: %v", i, err)
		}
		if errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("a store outage must not look like bad credentials")
		}
		assertNothingIssued(t, user, tok, exp)
	}
	if len(h.pw.compareCalls) != 0 {
		t.Fatalf("no password comparison without a user record: %v", h.pw.compareCalls)
	}
	// 障害が直れば、正しい利用者はすぐログインできる(障害中の試行で締め出されていない)
	f.findByUsernameErr = nil
	if err := loginAs(h, "1.1.1.1", "alice", "right-password-1"); err != nil {
		t.Fatalf("outage attempts must not lock the account or ip: %v", err)
	}
}

func TestLoginSessionIssueFailuresReturnNoCredentials(t *testing.T) {
	t.Run("token generation fails", func(t *testing.T) {
		h, _ := faultHarness()
		h.svc.Tokens = errTokens{errors.New("rand: secret-detail")}
		user, tok, exp, err := h.svc.Login(context.Background(), "alice", "right-password-1")
		if err == nil || errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("want an internal error, got %v", err)
		}
		if strings.Contains(err.Error(), "secret-detail") {
			t.Fatalf("cause must not leak: %v", err)
		}
		assertNothingIssued(t, user, tok, exp)
		if len(h.store.sessions) != 0 {
			t.Fatal("no session may exist")
		}
	})
	t.Run("session store fails", func(t *testing.T) {
		h, f := faultHarness()
		f.createSessionErr = errDB
		user, tok, exp, err := h.svc.Login(context.Background(), "alice", "right-password-1")
		if !errors.Is(err, errDB) {
			t.Fatalf("store failure must reach the caller: %v", err)
		}
		assertNothingIssued(t, user, tok, exp)
		if len(h.store.sessions) != 0 {
			t.Fatal("no session may exist")
		}
	})
}

func TestAuthenticateStoreFailureIsNotReportedAsLoggedOut(t *testing.T) {
	h, f := faultHarness()
	_, tok, _, err := h.svc.Login(context.Background(), "alice", "right-password-1")
	if err != nil {
		t.Fatal(err)
	}
	f.findUserErr = errDB
	session, err := h.svc.Authenticate(context.Background(), tok)
	if !errors.Is(err, errDB) || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("a store failure must not be reported as an invalid session: %v", err)
	}
	if session != (Session{}) {
		t.Fatalf("no session on failure: %+v", session)
	}
	// 障害が直れば、同じセッションがそのまま使える
	f.findUserErr = nil
	if _, err := h.svc.Authenticate(context.Background(), tok); err != nil {
		t.Fatalf("session must survive a transient failure: %v", err)
	}
}

func TestLogoutStoreFailureIsReportedAndKeepsSession(t *testing.T) {
	h, f := faultHarness()
	_, tok, _, err := h.svc.Login(context.Background(), "alice", "right-password-1")
	if err != nil {
		t.Fatal(err)
	}
	f.deleteErr = errDB
	if err := h.svc.Logout(context.Background(), tok); !errors.Is(err, errDB) {
		t.Fatalf("a failed revocation must not be reported as success: %v", err)
	}

	// 対象が既に無い(ErrNotFound)ことは成功として扱う
	f.deleteErr = ErrNotFound
	if err := h.svc.Logout(context.Background(), tok); err != nil {
		t.Fatalf("ErrNotFound must be tolerated: %v", err)
	}
}

func TestLogoutWithoutTokenDoesNotTouchTheStore(t *testing.T) {
	h, f := faultHarness()
	f.deleteErr = errDB
	if err := h.svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("no cookie must be a no-op: %v", err)
	}
	if f.deleteCalls != 0 {
		t.Fatalf("store was called %d times for an empty token", f.deleteCalls)
	}
}

func TestLogoutUnknownTokenKeepsExistingSession(t *testing.T) {
	h, f := faultHarness()
	_, other, _, err := h.svc.Login(context.Background(), "alice", "right-password-1")
	if err != nil {
		t.Fatal(err)
	}
	// 存在しないトークンのログアウトは、他のセッションに影響しない
	if err := h.svc.Logout(context.Background(), "unknown-token"); err != nil {
		t.Fatal(err)
	}
	if f.deleteCalls != 1 {
		t.Fatalf("delete calls = %d", f.deleteCalls)
	}
	if _, err := h.svc.Authenticate(context.Background(), other); err != nil {
		t.Fatalf("another session must survive: %v", err)
	}
}

func TestServiceLoggerFallsBackToSlogDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := newHarness()
	h.svc.Logger = nil
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}
	h.store.cleanupErr = errors.New("boom secret-detail")
	_, tok, _, err := h.svc.Login(context.Background(), "alice", "pass-word-long-ok")
	if err != nil {
		t.Fatalf("cleanup failure must not fail login: %v", err)
	}
	logs := buf.String()
	if !strings.Contains(logs, "WARN") || !strings.Contains(logs, "expired session cleanup failed") {
		t.Fatalf("warning must go to the default logger: %q", logs)
	}
	for _, secret := range []string{"pass-word-long-ok", tok, "secret-detail"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log must not contain %q: %s", secret, logs)
		}
	}
}

func TestLoginUnknownUserComparesAgainstDummyHashAndCountsFailure(t *testing.T) {
	h, _ := faultHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	h.svc.DummyHash = "dummy-hash"
	for i := 0; i < 5; i++ {
		if err := loginAs(h, "1.1.1.1", "ghost", "right-password-1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	for i, hash := range h.pw.compareCalls {
		if hash != "dummy-hash" {
			t.Fatalf("compare #%d used %q, want the dummy hash", i, hash)
		}
	}
	if len(h.pw.compareCalls) != 5 {
		t.Fatalf("each attempt must compare exactly once, got %d", len(h.pw.compareCalls))
	}
	// 存在しない利用者名でも、存在する場合と同じように締め出される
	if got := retryAfter(t, loginAs(h, "1.1.1.1", "ghost", "right-password-1")); got != time.Minute {
		t.Fatalf("lockout = %v", got)
	}
}

func TestNewDummyHashPropagatesHashFailure(t *testing.T) {
	cause := errors.New("hash failed")
	hash, err := NewDummyHash(errPasswords{cause})
	if !errors.Is(err, cause) || hash != "" {
		t.Fatalf("hash = %q, err = %v", hash, err)
	}
}
