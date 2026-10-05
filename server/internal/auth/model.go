// Package auth は利用者登録・ログイン・サーバー側セッションの用途と型を持つ。
// PostgreSQLやHTTPの詳細は持たず、外側の実装がこのパッケージのインターフェースを満たす。
package auth

import (
	"errors"
	"time"
)

// Role は利用者の役割。
type Role string

const (
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

// Valid は定義済みの役割かどうかを返す。
func (r Role) Valid() bool { return r == RoleMember || r == RoleAdmin }

// User は公開してよい利用者情報。
type User struct {
	ID       int64
	Username string
	Role     Role
}

// UserWithPassword はログイン照合に使う。パスワードハッシュは応答やログへ出さない。
type UserWithPassword struct {
	User
	PasswordHash string
}

// Session は認証済みの主体と、そのセッションの有効期限。
type Session struct {
	User      User
	ExpiresAt time.Time
}

// SignupInput は会員登録の入力。
type SignupInput struct {
	Username     string
	Password     string
	FavoriteTags []string
}

var (
	// ErrNotFound は対象の利用者またはセッションが無いことを表す。
	ErrNotFound = errors.New("auth: not found")
	// ErrUsernameTaken は利用者名が既に使われていることを表す。
	ErrUsernameTaken = errors.New("auth: username taken")
	// ErrInvalidCredentials は利用者不存在・誤パスワード・期限切れ・未ログインをまとめて表す。
	// 呼び出し側が原因を区別できないよう、1つの値にする。
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
)

// FieldError は入力項目ごとの不正理由。
type FieldError struct {
	Field   string
	Message string
}

// ValidationError は入力不正。入力値そのものは含めない。
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return "auth: invalid input" }
