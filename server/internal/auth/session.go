package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const tokenBytes = 32

// RandomTokenGenerator は暗号学的乱数32バイトから不透明トークンを作る。
type RandomTokenGenerator struct{}

// New は URL-safe な平文と、その文字列のSHA-256を返す。平文は保存しない。
func (RandomTokenGenerator) New() (string, [32]byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, err
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashToken(plain), nil
}

// HashToken はCookieの値からDBの検索キーを作る。
func HashToken(plain string) [32]byte {
	return sha256.Sum256([]byte(plain))
}
