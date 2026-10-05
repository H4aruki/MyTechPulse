package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// 入力の上限。API仕様(設計書 7.2)と同じ。
const (
	maxUsernameChars = 50
	maxTagChars      = 50
	maxFavoriteTags  = 128
)

// Service は登録・ログイン・認証・ログアウトの用途を実装する。
type Service struct {
	Users      UserRepository
	Sessions   SessionRepository
	Passwords  Passwords
	Tokens     TokenGenerator
	Clock      Clock
	SessionTTL time.Duration
	// DummyHash は利用者が存在しないときも同じ照合処理を行うための固定ハッシュ。起動時に1度だけ作る。
	DummyHash string
	// Logger は清掃失敗などの警告用。nilなら標準のslogを使う。パスワードやトークンは渡さない。
	Logger *slog.Logger
}

func (s Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func validateUsername(username string) (string, *FieldError) {
	name := strings.TrimSpace(username)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxUsernameChars || !utf8.ValidString(name) {
		return "", &FieldError{Field: "username", Message: "利用者名は1〜50文字で入力してください"}
	}
	return name, nil
}

func validatePassword(password string) *FieldError {
	if !ValidPassword(password) {
		return &FieldError{Field: "password", Message: "パスワードは1文字以上、UTF-8で72バイト以内で入力してください"}
	}
	return nil
}

// normalizeTags は件数と文字数を検証し、前後の空白を除いて大文字小文字を無視した重複を取り除く。
func normalizeTags(tags []string) ([]string, *FieldError) {
	if len(tags) < 1 || len(tags) > maxFavoriteTags {
		return nil, &FieldError{Field: "favorite_tags", Message: "興味のあるタグは1〜128件で指定してください"}
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if n := utf8.RuneCountInString(tag); n < 1 || n > maxTagChars || !utf8.ValidString(tag) {
			return nil, &FieldError{Field: "favorite_tags", Message: "タグは1〜50文字で入力してください"}
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	return out, nil
}

func invalid(fields ...*FieldError) error {
	var list []FieldError
	for _, f := range fields {
		if f != nil {
			list = append(list, *f)
		}
	}
	if len(list) == 0 {
		return nil
	}
	return &ValidationError{Fields: list}
}

// Signup は利用者を member として登録し、最初のセッションの平文トークンと期限を返す。
func (s Service) Signup(ctx context.Context, in SignupInput) (User, string, time.Time, error) {
	username, userErr := validateUsername(in.Username)
	passErr := validatePassword(in.Password)
	tags, tagErr := normalizeTags(in.FavoriteTags)
	if err := invalid(userErr, passErr, tagErr); err != nil {
		return User{}, "", time.Time{}, err
	}
	hash, err := s.Passwords.Hash(in.Password)
	if err != nil {
		return User{}, "", time.Time{}, errors.New("auth: パスワードを処理できません")
	}
	plain, tokenHash, err := s.Tokens.New()
	if err != nil {
		return User{}, "", time.Time{}, errors.New("auth: セッションを作成できません")
	}
	expires := s.Clock.Now().Add(s.SessionTTL)
	user, err := s.Users.CreateWithInterestsAndSession(ctx, username, hash, RoleMember, tags, tokenHash, expires)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, plain, expires, nil
}

// Login は利用者名とパスワードを照合し、新しいセッションの平文トークンと期限を返す。
// 利用者が存在しない場合も誤パスワードの場合も、同じ ErrInvalidCredentials を返す。
func (s Service) Login(ctx context.Context, username, password string) (User, string, time.Time, error) {
	name, userErr := validateUsername(username)
	passErr := validatePassword(password)
	if err := invalid(userErr, passErr); err != nil {
		return User{}, "", time.Time{}, err
	}

	found, err := s.Users.FindByUsername(ctx, name)
	hash := found.PasswordHash
	switch {
	case errors.Is(err, ErrNotFound):
		// 存在有無で処理時間が大きく変わらないよう、固定ハッシュとも1回照合する
		hash = s.DummyHash
	case err != nil:
		return User{}, "", time.Time{}, err
	}
	compareErr := s.Passwords.Compare(hash, password)
	if err != nil || compareErr != nil {
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}

	now := s.Clock.Now()
	if err := s.Sessions.DeleteExpired(ctx, now); err != nil {
		// 清掃だけの失敗でログインは止めない
		s.logger().Warn("expired session cleanup failed")
	}
	plain, tokenHash, err := s.Tokens.New()
	if err != nil {
		return User{}, "", time.Time{}, errors.New("auth: セッションを作成できません")
	}
	expires := now.Add(s.SessionTTL)
	if err := s.Sessions.Create(ctx, tokenHash, found.ID, expires); err != nil {
		return User{}, "", time.Time{}, err
	}
	return found.User, plain, expires, nil
}

// Authenticate はCookieの値から有効なセッションを探す。無い・期限切れは ErrInvalidCredentials。
func (s Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidCredentials
	}
	session, err := s.Sessions.FindUser(ctx, HashToken(token), s.Clock.Now())
	if errors.Is(err, ErrNotFound) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

// Logout は現在のセッションだけを削除する。既に無くても成功する。
func (s Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.Sessions.Delete(ctx, HashToken(token)); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}
