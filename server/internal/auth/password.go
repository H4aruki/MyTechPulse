package auth

import (
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost は現行Python版(Passlib)と同じ12に固定する。
const BcryptCost = 12

// MaxPasswordBytes はbcryptが扱える上限。
const MaxPasswordBytes = 72

var errInvalidPassword = errors.New("auth: invalid password")

// ValidPassword は1文字以上かつUTF-8で72バイト以内かを返す。
func ValidPassword(password string) bool {
	return password != "" && len(password) <= MaxPasswordBytes && utf8.ValidString(password)
}

// NewDummyHash は、存在しない利用者のログインでも照合を行うための固定ハッシュを作る。
// 元のパスワードは乱数で、どこにも保存しないため、誰もこのハッシュに一致できない。
func NewDummyHash(p Passwords) (string, error) {
	plain, _, err := (RandomTokenGenerator{}).New()
	if err != nil {
		return "", err
	}
	return p.Hash(plain)
}

// BcryptPasswords は既存の$2b$ハッシュをそのまま照合できるbcrypt実装。
type BcryptPasswords struct{}

// Hash は新しいハッシュを作る。入力が不正なら生成しない。
func (BcryptPasswords) Hash(password string) (string, error) {
	if !ValidPassword(password) {
		return "", errInvalidPassword
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Compare は一致すればnilを返す。入力異常も認証失敗として同じエラーにする。
func (BcryptPasswords) Compare(hash, password string) error {
	if !ValidPassword(password) {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}
