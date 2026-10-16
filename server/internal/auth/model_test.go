package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestRoleValid(t *testing.T) {
	cases := map[Role]bool{
		RoleMember: true,
		RoleAdmin:  true,
		"":         false,
		"Member":   false, // 大文字小文字は区別する
		"ADMIN":    false,
		"root":     false,
		"member ":  false,
	}
	for role, want := range cases {
		if got := role.Valid(); got != want {
			t.Errorf("Role(%q).Valid() = %v, want %v", role, got, want)
		}
	}
}

func TestValidationErrorTextDoesNotExposeFields(t *testing.T) {
	err := error(&ValidationError{Fields: []FieldError{{Field: "password", Message: "secret-input-value"}}})
	wrapped := fmt.Errorf("signup: %w", err)

	var ve *ValidationError
	if !errors.As(wrapped, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != "password" {
		t.Fatalf("errors.As must find the ValidationError: %v", wrapped)
	}
	// ログへそのまま出されても入力値が漏れないよう、本文は固定の文言にする
	if got := err.Error(); got == "" || strings.Contains(got, "secret-input-value") || strings.Contains(got, "password") {
		t.Fatalf("Error() = %q", got)
	}
}

func TestTooManyAttemptsErrorText(t *testing.T) {
	err := error(&TooManyAttemptsError{RetryAfter: 42 * time.Second})
	var tm *TooManyAttemptsError
	if !errors.As(fmt.Errorf("login: %w", err), &tm) || tm.RetryAfter != 42*time.Second {
		t.Fatal("errors.As must recover RetryAfter")
	}
	if got := err.Error(); got == "" || strings.Contains(got, "42") {
		t.Fatalf("Error() = %q", got)
	}
	// 429は認証失敗(401)とは別の扱い
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("too many attempts must not be ErrInvalidCredentials")
	}
}

func TestNewDummyHashIsARealBcryptHashOfAnUnguessablePassword(t *testing.T) {
	// 存在しない利用者の照合も、実在の利用者と同じ重さ(cost 12)にそろえる
	hash, err := NewDummyHash(BcryptPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != BcryptCost {
		t.Fatalf("cost = %d, err = %v", cost, err)
	}
	if (BcryptPasswords{}).Compare(hash, "password") == nil {
		t.Fatal("dummy hash must not match a common password")
	}
}

type recordingPasswords struct{ hashed []string }

func (p *recordingPasswords) Hash(pw string) (string, error) {
	p.hashed = append(p.hashed, pw)
	return "hash:" + pw, nil
}
func (p *recordingPasswords) Compare(string, string) error { return ErrInvalidCredentials }

func TestNewDummyHashHashesAFreshRandomSecretEachTime(t *testing.T) {
	rec := &recordingPasswords{}
	for range 3 {
		if _, err := NewDummyHash(rec); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, pw := range rec.hashed {
		// 32バイトの乱数のBase64(URL-safe、パディング無し)は43文字。固定の文字列ではない
		if len(pw) != 43 || !ValidPassword(pw) || seen[pw] {
			t.Fatalf("dummy secret %q must be a fresh 43-char random token", pw)
		}
		seen[pw] = true
	}
}
