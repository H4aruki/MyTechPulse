package passkey

import (
	"context"
	"time"
)

// IPLimiter は接続元IPごとの失敗回数と開始回数の制限。パスキーのログインは利用者名を受け取らないため、IPだけで数える。
// *auth.LoginLimiter がこれを満たし、失敗はパスワードログインと同じ数え方(同じ窓)を共有する。
type IPLimiter interface {
	// AllowBegin はログイン開始のたびに呼ぶ。失敗とは別の枠で数え、上限なら *auth.TooManyAttemptsError を返す。
	AllowBegin(ip string) error
	// CheckIP は締め出し中なら *auth.TooManyAttemptsError を返す。
	CheckIP(ip string) error
	// FailureIP は認証に失敗したときに呼ぶ。
	FailureIP(ip string)
}

// Repository はパスキーと利用者ハンドルの永続化。
type Repository interface {
	// UserHandle は利用者のハンドルを返す。無ければ newHandle を保存して返す(同時に呼ばれても1つに決まる)。
	UserHandle(ctx context.Context, userID int64, newHandle []byte) ([]byte, error)
	// UserIDByHandle はハンドルから利用者IDを返す。無ければ ErrNotFound。
	UserIDByHandle(ctx context.Context, handle []byte) (int64, error)
	// Create は上限を確かめて登録する。上限なら ErrLimitReached、同じ認証情報IDなら ErrCredentialExists。
	Create(ctx context.Context, c Credential) (Credential, error)
	ListByUser(ctx context.Context, userID int64) ([]Credential, error)
	// FindByCredentialID は無ければ ErrNotFound。
	FindByCredentialID(ctx context.Context, credentialID []byte) (Credential, error)
	// RecordUse はログイン成功後に、認証回数・同期の状態・最後に使った日時を更新する。
	RecordUse(ctx context.Context, credentialID []byte, signCount uint32, backupState bool, now time.Time) error
	// Rename は本人のパスキーだけを変える。他人のもの・無いものは ErrNotFound。
	Rename(ctx context.Context, userID, id int64, name string) error
	// Delete は本人のパスキーだけを物理削除する。他人のもの・無いものは ErrNotFound。
	Delete(ctx context.Context, userID, id int64) error
}

// ChallengeRepository はチャレンジの永続化。
type ChallengeRepository interface {
	Create(ctx context.Context, id [32]byte, purpose Purpose, userID *int64, sessionData []byte, expiresAt time.Time) error
	// Consume は未使用かつ期限内で用途が合うときだけ、使用済みにして返す。それ以外は ErrChallengeInvalid。
	// 同時に呼ばれても成功するのは1回だけ。
	Consume(ctx context.Context, id [32]byte, purpose Purpose, now time.Time) (Challenge, error)
	// DeleteExpired は期限切れと使用済みのチャレンジを消す。
	DeleteExpired(ctx context.Context, now time.Time) error
}
