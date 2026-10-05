package auth

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type compatibilityFixture struct {
	Password string `json:"password"`
	Bcrypt2b string `json:"bcrypt_2b"`
}

func loadFixture(t *testing.T) compatibilityFixture {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/compatibility/auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var f compatibilityFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Bcrypt2b, "$2b$12$") {
		t.Fatal("fixture must be a Python-generated $2b$ cost 12 hash")
	}
	return f
}

func TestBcryptComparesPythonGeneratedHash(t *testing.T) {
	f := loadFixture(t)
	p := BcryptPasswords{}
	if err := p.Compare(f.Bcrypt2b, f.Password); err != nil {
		t.Fatalf("correct password must match: %v", err)
	}
	if err := p.Compare(f.Bcrypt2b, f.Password+"x"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password must fail: %v", err)
	}
}

func TestBcryptHashUsesCost12AndRoundTrips(t *testing.T) {
	p := BcryptPasswords{}
	hash, err := p.Hash("synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != 12 {
		t.Fatalf("cost = %d, err = %v", cost, err)
	}
	if err := p.Compare(hash, "synthetic-password"); err != nil {
		t.Fatal(err)
	}
}

func TestBcryptRejectsInvalidPasswords(t *testing.T) {
	p := BcryptPasswords{}
	f := loadFixture(t)
	tooLong := strings.Repeat("a", 73)
	cases := map[string]string{
		"empty":        "",
		"73 bytes":     tooLong,
		"invalid utf8": "\xff\xfe",
		// 24文字だが3バイト文字なので72バイトを超える
		"multibyte over 72 bytes": strings.Repeat("あ", 25),
	}
	for name, pw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := p.Hash(pw); err == nil {
				t.Fatal("Hash must reject")
			}
			if err := p.Compare(f.Bcrypt2b, pw); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Compare must fail as invalid credentials: %v", err)
			}
		})
	}
	if _, err := p.Hash(strings.Repeat("a", 72)); err != nil {
		t.Fatalf("72 bytes must be accepted: %v", err)
	}
	if _, err := p.Hash(strings.Repeat("あ", 24)); err != nil {
		t.Fatalf("24 multibyte chars (72 bytes) must be accepted: %v", err)
	}
}

func TestBcryptCompareRejectsMalformedHash(t *testing.T) {
	if err := (BcryptPasswords{}).Compare("not-a-hash", "password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("malformed hash must fail as invalid credentials: %v", err)
	}
}
