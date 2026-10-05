package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestTokenGeneratorReturnsPlainAndHash(t *testing.T) {
	plain, hash, err := (RandomTokenGenerator{}).New()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil || len(raw) != 32 {
		t.Fatal("invalid token")
	}
	if sha256.Sum256([]byte(plain)) != hash {
		t.Fatal("hash mismatch")
	}
	if HashToken(plain) != hash {
		t.Fatal("HashToken must match the stored hash")
	}
}

func TestTokenGeneratorIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		plain, _, err := (RandomTokenGenerator{}).New()
		if err != nil {
			t.Fatal(err)
		}
		if seen[plain] {
			t.Fatal("token repeated")
		}
		seen[plain] = true
	}
}
