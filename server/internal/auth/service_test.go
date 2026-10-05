package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakePasswords struct {
	compareCalls []string // 照合に渡されたhash
}

func (p *fakePasswords) Hash(pw string) (string, error) { return "hash:" + pw, nil }
func (p *fakePasswords) Compare(hash, pw string) error {
	p.compareCalls = append(p.compareCalls, hash)
	if hash == "hash:"+pw {
		return nil
	}
	return ErrInvalidCredentials
}

type seqTokens struct{ n byte }

func (g *seqTokens) New() (string, [32]byte, error) {
	g.n++
	plain := "token-" + string('a'+rune(g.n))
	return plain, HashToken(plain), nil
}

type fakeUser struct {
	User
	hash string
}

type fakeStore struct {
	users        map[string]fakeUser
	sessions     map[[32]byte]Session
	userIDs      map[[32]byte]int64
	nextID       int64
	createdTags  [][]string
	createdRoles []Role
	cleanupErr   error
	cleanupCalls []time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]fakeUser{}, sessions: map[[32]byte]Session{}, userIDs: map[[32]byte]int64{}, nextID: 1}
}

func (f *fakeStore) FindByUsername(_ context.Context, name string) (UserWithPassword, error) {
	u, ok := f.users[name]
	if !ok {
		return UserWithPassword{}, ErrNotFound
	}
	return UserWithPassword{User: u.User, PasswordHash: u.hash}, nil
}

func (f *fakeStore) CreateWithInterestsAndSession(_ context.Context, name, hash string, role Role,
	tags []string, token [32]byte, exp time.Time) (User, error) {
	if _, ok := f.users[name]; ok {
		return User{}, ErrUsernameTaken
	}
	u := User{ID: f.nextID, Username: name, Role: role}
	f.nextID++
	f.users[name] = fakeUser{User: u, hash: hash}
	f.sessions[token] = Session{User: u, ExpiresAt: exp}
	f.createdTags = append(f.createdTags, tags)
	f.createdRoles = append(f.createdRoles, role)
	return u, nil
}

func (f *fakeStore) Create(_ context.Context, token [32]byte, userID int64, exp time.Time) error {
	for _, u := range f.users {
		if u.ID == userID {
			f.sessions[token] = Session{User: u.User, ExpiresAt: exp}
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeStore) FindUser(_ context.Context, token [32]byte, now time.Time) (Session, error) {
	s, ok := f.sessions[token]
	if !ok || !s.ExpiresAt.After(now) {
		return Session{}, ErrNotFound
	}
	return s, nil
}

func (f *fakeStore) Delete(_ context.Context, token [32]byte) error {
	delete(f.sessions, token)
	return nil
}

func (f *fakeStore) DeleteExpired(_ context.Context, now time.Time) error {
	f.cleanupCalls = append(f.cleanupCalls, now)
	if f.cleanupErr != nil {
		return f.cleanupErr
	}
	for k, s := range f.sessions {
		if !s.ExpiresAt.After(now) {
			delete(f.sessions, k)
		}
	}
	return nil
}

type harness struct {
	svc   Service
	store *fakeStore
	pw    *fakePasswords
	clock *fakeClock
	logs  *bytes.Buffer
}

func newHarness() *harness {
	h := &harness{
		store: newFakeStore(),
		pw:    &fakePasswords{},
		clock: &fakeClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
		logs:  &bytes.Buffer{},
	}
	h.svc = Service{
		Users: h.store, Sessions: h.store, Passwords: h.pw, Tokens: &seqTokens{}, Clock: h.clock,
		SessionTTL: 24 * time.Hour, DummyHash: "dummy-hash",
		Logger: slog.New(slog.NewTextHandler(h.logs, nil)),
	}
	return h
}

func validSignup() SignupInput {
	return SignupInput{Username: "alice", Password: "pass-word", FavoriteTags: []string{"Go"}}
}

func TestServiceSignupValidation(t *testing.T) {
	manyTags := make([]string, 129)
	for i := range manyTags {
		manyTags[i] = string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	maxTags := manyTags[:128]
	cases := []struct {
		name   string
		mutate func(*SignupInput)
		field  string // 空なら成功
	}{
		{"ok", func(*SignupInput) {}, ""},
		{"username empty", func(in *SignupInput) { in.Username = "" }, "username"},
		{"username only spaces", func(in *SignupInput) { in.Username = "  \t " }, "username"},
		{"username 51 chars", func(in *SignupInput) { in.Username = strings.Repeat("a", 51) }, "username"},
		{"username 50 chars", func(in *SignupInput) { in.Username = strings.Repeat("a", 50) }, ""},
		{"username 50 multibyte chars", func(in *SignupInput) { in.Username = strings.Repeat("あ", 50) }, ""},
		{"username 51 multibyte chars", func(in *SignupInput) { in.Username = strings.Repeat("あ", 51) }, "username"},
		{"password empty", func(in *SignupInput) { in.Password = "" }, "password"},
		{"password 73 bytes", func(in *SignupInput) { in.Password = strings.Repeat("a", 73) }, "password"},
		{"password 72 bytes", func(in *SignupInput) { in.Password = strings.Repeat("a", 72) }, ""},
		{"password multibyte over 72 bytes", func(in *SignupInput) { in.Password = strings.Repeat("あ", 25) }, "password"},
		{"tags none", func(in *SignupInput) { in.FavoriteTags = nil }, "favorite_tags"},
		{"tags 129", func(in *SignupInput) { in.FavoriteTags = manyTags }, "favorite_tags"},
		{"tags 128", func(in *SignupInput) { in.FavoriteTags = maxTags }, ""},
		{"tag blank", func(in *SignupInput) { in.FavoriteTags = []string{"Go", "  "} }, "favorite_tags"},
		{"tag 51 chars", func(in *SignupInput) { in.FavoriteTags = []string{strings.Repeat("a", 51)} }, "favorite_tags"},
		{"tag 50 chars", func(in *SignupInput) { in.FavoriteTags = []string{strings.Repeat("a", 50)} }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			in := validSignup()
			tc.mutate(&in)
			_, _, _, err := h.svc.Signup(context.Background(), in)
			if tc.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if len(ve.Fields) == 0 || ve.Fields[0].Field != tc.field {
				t.Fatalf("fields = %+v, want %s", ve.Fields, tc.field)
			}
			if len(h.store.users) != 0 {
				t.Fatal("invalid input must not create a user")
			}
		})
	}
}

func TestServiceSignupNormalizesAndCreatesMember(t *testing.T) {
	h := newHarness()
	in := SignupInput{Username: "  alice  ", Password: "pass-word", FavoriteTags: []string{" Go ", "go", "GO", "Rust", "rust "}}
	user, plain, exp, err := h.svc.Signup(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "alice" || user.Role != RoleMember {
		t.Fatalf("user = %+v", user)
	}
	if want := []string{"Go", "Rust"}; !reflect.DeepEqual(h.store.createdTags[0], want) {
		t.Fatalf("tags = %v, want %v", h.store.createdTags[0], want)
	}
	if h.store.createdRoles[0] != RoleMember {
		t.Fatal("signup must always create a member")
	}
	if want := h.clock.now.Add(24 * time.Hour); !exp.Equal(want) {
		t.Fatalf("expires = %v, want %v", exp, want)
	}
	// 保存されているのはハッシュ化済みパスワードとトークンのハッシュだけ
	if h.store.users["alice"].hash != "hash:pass-word" {
		t.Fatal("password must be stored hashed")
	}
	if _, ok := h.store.sessions[HashToken(plain)]; !ok {
		t.Fatal("session must be stored under the token hash")
	}
	if s, err := h.svc.Authenticate(context.Background(), plain); err != nil || s.User != user {
		t.Fatalf("signup session must authenticate: %+v %v", s, err)
	}
}

func TestServiceSignupDuplicateUsername(t *testing.T) {
	h := newHarness()
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := h.svc.Signup(context.Background(), validSignup())
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("want ErrUsernameTaken, got %v", err)
	}
}

func TestServiceLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness()
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}

	h.pw.compareCalls = nil
	_, _, _, missingErr := h.svc.Login(context.Background(), "nobody", "pass-word")
	missingCalls := h.pw.compareCalls

	h.pw.compareCalls = nil
	_, _, _, wrongErr := h.svc.Login(context.Background(), "alice", "wrong-pass")
	wrongCalls := h.pw.compareCalls

	if !errors.Is(missingErr, ErrInvalidCredentials) || !errors.Is(wrongErr, ErrInvalidCredentials) {
		t.Fatalf("both must be ErrInvalidCredentials: %v / %v", missingErr, wrongErr)
	}
	if missingErr.Error() != wrongErr.Error() {
		t.Fatal("error text must be identical")
	}
	// どちらも照合が1回だけ行われる。不存在時は固定ハッシュを使う
	if !reflect.DeepEqual(missingCalls, []string{"dummy-hash"}) {
		t.Fatalf("missing user compare calls = %v", missingCalls)
	}
	if !reflect.DeepEqual(wrongCalls, []string{"hash:pass-word"}) {
		t.Fatalf("wrong password compare calls = %v", wrongCalls)
	}
	if len(h.store.sessions) != 1 {
		t.Fatalf("failed logins must not create sessions, have %d", len(h.store.sessions))
	}
}

func TestServiceLoginValidation(t *testing.T) {
	h := newHarness()
	for name, c := range map[string][2]string{
		"empty username":   {"", "pass-word"},
		"blank username":   {"   ", "pass-word"},
		"empty password":   {"alice", ""},
		"password 73 byte": {"alice", strings.Repeat("a", 73)},
	} {
		_, _, _, err := h.svc.Login(context.Background(), c[0], c[1])
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
	if len(h.pw.compareCalls) != 0 {
		t.Fatal("invalid input must be rejected before password comparison")
	}
}

func TestServiceLoginSuccessAndMultipleDevices(t *testing.T) {
	h := newHarness()
	signupUser, signupToken, _, err := h.svc.Signup(context.Background(), validSignup())
	if err != nil {
		t.Fatal(err)
	}
	user, tokenA, exp, err := h.svc.Login(context.Background(), " alice ", "pass-word")
	if err != nil {
		t.Fatal(err)
	}
	if user != signupUser {
		t.Fatalf("user = %+v", user)
	}
	if want := h.clock.now.Add(24 * time.Hour); !exp.Equal(want) {
		t.Fatalf("expires = %v, want %v", exp, want)
	}
	_, tokenB, _, err := h.svc.Login(context.Background(), "alice", "pass-word")
	if err != nil {
		t.Fatal(err)
	}
	if tokenA == tokenB || tokenA == signupToken {
		t.Fatal("each login must get a distinct token")
	}
	for _, tok := range []string{signupToken, tokenA, tokenB} {
		if _, err := h.svc.Authenticate(context.Background(), tok); err != nil {
			t.Fatalf("all device sessions must coexist: %v", err)
		}
	}
	// ログアウトは現在のtokenだけを消す
	if err := h.svc.Logout(context.Background(), tokenA); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Authenticate(context.Background(), tokenA); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("logged out token must fail: %v", err)
	}
	for _, tok := range []string{signupToken, tokenB} {
		if _, err := h.svc.Authenticate(context.Background(), tok); err != nil {
			t.Fatalf("other sessions must survive: %v", err)
		}
	}
}

func TestServiceAuthenticateRejectsExpiredAndUnknown(t *testing.T) {
	h := newHarness()
	_, tok, exp, err := h.svc.Signup(context.Background(), validSignup())
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = exp.Add(-time.Second)
	if _, err := h.svc.Authenticate(context.Background(), tok); err != nil {
		t.Fatalf("before expiry: %v", err)
	}
	h.clock.now = exp
	if _, err := h.svc.Authenticate(context.Background(), tok); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("at expiry must fail: %v", err)
	}
	for _, bad := range []string{"", "unknown-token"} {
		if _, err := h.svc.Authenticate(context.Background(), bad); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%q must fail: %v", bad, err)
		}
	}
}

func TestServiceLogoutAlwaysSucceeds(t *testing.T) {
	h := newHarness()
	_, tok, _, err := h.svc.Signup(context.Background(), validSignup())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := h.svc.Logout(context.Background(), tok); err != nil {
			t.Fatalf("logout #%d: %v", i, err)
		}
	}
	for _, tok := range []string{"", "never-existed"} {
		if err := h.svc.Logout(context.Background(), tok); err != nil {
			t.Fatalf("logout %q: %v", tok, err)
		}
	}
}

func TestServiceLoginCleansExpiredSessionsButToleratesFailure(t *testing.T) {
	h := newHarness()
	_, oldTok, exp, err := h.svc.Signup(context.Background(), validSignup())
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = exp.Add(time.Minute)
	if _, _, _, err := h.svc.Login(context.Background(), "alice", "pass-word"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.store.sessions[HashToken(oldTok)]; ok {
		t.Fatal("expired session must be cleaned on login")
	}
	if len(h.store.cleanupCalls) != 1 || !h.store.cleanupCalls[0].Equal(h.clock.now) {
		t.Fatalf("cleanup calls = %v", h.store.cleanupCalls)
	}

	// 清掃が失敗してもログインできる。警告にはパスワードやトークンを含めない
	h.store.cleanupErr = errors.New("boom: secret-detail")
	_, tok, _, err := h.svc.Login(context.Background(), "alice", "pass-word")
	if err != nil {
		t.Fatalf("cleanup failure must not fail login: %v", err)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "WARN") {
		t.Fatal("a warning must be logged")
	}
	for _, secret := range []string{"pass-word", tok, "secret-detail"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log must not contain %q: %s", secret, logs)
		}
	}
}
