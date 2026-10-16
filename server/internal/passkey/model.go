// Package passkey はパスキー(WebAuthn)の土台を持つ。型・永続化の窓口・検証器の組み立てだけで、
// HTTPの経路にはまだ繋いでいない。
package passkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
)

const (
	// MaxPasskeysPerUser は1人が登録できるパスキーの上限。
	MaxPasskeysPerUser = 10
	// ChallengeTTL はチャレンジの有効期間。
	ChallengeTTL = 5 * time.Minute
	// UserHandleSize はWebAuthnの利用者ハンドル(乱数)のバイト数。
	UserHandleSize = 32
	// MaxNameLength はパスキーの名前の最大文字数。
	MaxNameLength = 64
)

// Purpose はチャレンジの用途。
type Purpose string

const (
	PurposeRegister Purpose = "register"
	PurposeLogin    Purpose = "login"
)

var (
	// ErrNotFound は対象が無い、または他人のものであること。区別して返さない。
	ErrNotFound = errors.New("passkey: 見つかりません")
	// ErrLimitReached は登録数が MaxPasskeysPerUser に達していること。
	ErrLimitReached = errors.New("passkey: 登録できる上限に達しています")
	// ErrCredentialExists は同じ認証情報IDが登録済みであること。
	ErrCredentialExists = errors.New("passkey: すでに登録されています")
	// ErrChallengeInvalid はチャレンジが無い・期限切れ・使用済みのいずれかであること。区別して返さない。
	ErrChallengeInvalid = errors.New("passkey: チャレンジが無効です")
)

// Credential は登録済みのパスキー。
type Credential struct {
	ID             int64
	UserID         int64
	CredentialID   []byte
	PublicKey      []byte
	SignCount      uint32
	Transports     []string
	AAGUID         []byte
	BackupEligible bool
	BackupState    bool
	Name           string
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

// Challenge は消費したチャレンジの中身。登録では UserID を持ち、ログインでは持たない。
type Challenge struct {
	UserID      *int64
	SessionData []byte
}

// NewUserHandle は利用者ハンドルになる暗号論的乱数を作る。利用者IDからは作らない。
func NewUserHandle() ([]byte, error) {
	handle := make([]byte, UserHandleSize)
	if _, err := rand.Read(handle); err != nil {
		return nil, errors.New("passkey: 乱数を作れません")
	}
	return handle, nil
}

// NewChallengeToken はブラウザへ渡す平文トークンと、DBへ保存するSHA-256ハッシュ(チャレンジのid)を作る。
func NewChallengeToken() (plain string, id [32]byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", id, errors.New("passkey: 乱数を作れません")
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashChallengeToken(plain), nil
}

// HashChallengeToken は平文トークンからチャレンジのidを求める。
func HashChallengeToken(plain string) [32]byte {
	return sha256.Sum256([]byte(plain))
}
