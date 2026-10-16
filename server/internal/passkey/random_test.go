package passkey

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestUserHandlesAreUniqueFixedLengthAndNotZero(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		h, err := NewUserHandle()
		if err != nil {
			t.Fatal(err)
		}
		if len(h) != UserHandleSize {
			t.Fatalf("handle length = %d, want %d", len(h), UserHandleSize)
		}
		allZero := true
		for _, b := range h {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatal("handle must not be all zero bytes")
		}
		if seen[string(h)] {
			t.Fatalf("handle repeated after %d calls", i)
		}
		seen[string(h)] = true
	}
}

func TestChallengeTokensAreURLSafeUniqueAndHashedFromThePlainText(t *testing.T) {
	plains := map[string]bool{}
	ids := map[[32]byte]bool{}
	for i := 0; i < 1000; i++ {
		plain, id, err := NewChallengeToken()
		if err != nil {
			t.Fatal(err)
		}
		// 32バイトの乱数をパディング無しのURL-safe Base64にした43文字
		if len(plain) != 43 || strings.ContainsAny(plain, "+/=") {
			t.Fatalf("token %q must be 43 URL-safe characters without padding", plain)
		}
		raw, err := base64.RawURLEncoding.DecodeString(plain)
		if err != nil || len(raw) != 32 {
			t.Fatalf("token must decode to 32 bytes: %d bytes, err %v", len(raw), err)
		}
		// DBに保存するidは、ブラウザへ渡す文字列そのもののSHA-256
		if want := sha256.Sum256([]byte(plain)); id != want {
			t.Fatalf("id = %x, want sha256 of the plain text %x", id, want)
		}
		if id == sha256.Sum256(raw) {
			t.Fatal("id must hash the encoded token, not the raw bytes")
		}
		if plains[plain] || ids[id] {
			t.Fatalf("token or id repeated after %d calls", i)
		}
		plains[plain], ids[id] = true, true
	}
}

func TestHashChallengeTokenIsDeterministicAndInputSensitive(t *testing.T) {
	if HashChallengeToken("abc") != HashChallengeToken("abc") {
		t.Fatal("same input must give the same id")
	}
	if HashChallengeToken("abc") == HashChallengeToken("abd") || HashChallengeToken("abc") == HashChallengeToken("ABC") {
		t.Fatal("different input must give a different id")
	}
	// SHA-256("") の既知の値で、アルゴリズムが変わっていないことを確かめる
	const emptyHex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	got := HashChallengeToken("")
	if hexOf(got[:]) != emptyHex {
		t.Fatalf("HashChallengeToken(\"\") = %s", hexOf(got[:]))
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0x0f])
	}
	return string(out)
}
