package auth

import (
	"context"
	"time"
)

// Passwords はパスワードのハッシュ化と照合。
type Passwords interface {
	Hash(password string) (string, error)
	// Compare は一致すればnil、不一致や入力異常ならエラーを返す。
	Compare(hash, password string) error
}

// TokenGenerator はブラウザへ渡す平文トークンと、DBへ保存するSHA-256ハッシュを作る。
type TokenGenerator interface {
	New() (plain string, hash [32]byte, err error)
}

// Clock は現在時刻。テストで固定できるようにする。
type Clock interface {
	Now() time.Time
}

// UserRepository は利用者の永続化。
type UserRepository interface {
	// FindByUsername は無ければ ErrNotFound を返す。
	FindByUsername(ctx context.Context, username string) (UserWithPassword, error)
	// CreateWithInterestsAndSession は利用者・初期興味度・セッションを同一トランザクションで作る。
	// 同名の利用者がいれば ErrUsernameTaken を返し、何も残さない。
	CreateWithInterestsAndSession(ctx context.Context, username, passwordHash string, role Role,
		favoriteTags []string, tokenHash [32]byte, expiresAt time.Time) (User, error)
}

// SessionRepository はセッションの永続化。
type SessionRepository interface {
	Create(ctx context.Context, tokenHash [32]byte, userID int64, expiresAt time.Time) error
	// FindUser は期限内のセッションだけを返し、無ければ ErrNotFound を返す。
	FindUser(ctx context.Context, tokenHash [32]byte, now time.Time) (Session, error)
	// Delete は対象が無くてもエラーにしない。
	Delete(ctx context.Context, tokenHash [32]byte) error
	DeleteExpired(ctx context.Context, now time.Time) error
}
