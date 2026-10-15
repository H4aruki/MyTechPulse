package auth

import (
	_ "embed"
	"strings"
	"sync"
	"unicode/utf8"
)

// MinPasswordChars は新しく決めるパスワードの最小文字数（NIST SP 800-63B）。
// 文字種の組み合わせ規則は設けない。ログインの照合には適用しない（既存の短いパスワードの利用者が入れなくなるため）。
const MinPasswordChars = 12

//go:embed commonpasswords.txt
var commonPasswordsRaw string

var (
	commonPasswordsOnce sync.Once
	commonPasswords     map[string]struct{}
)

func commonPasswordSet() map[string]struct{} {
	commonPasswordsOnce.Do(func() {
		commonPasswords = map[string]struct{}{}
		for _, line := range strings.Split(commonPasswordsRaw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			commonPasswords[strings.ToLower(line)] = struct{}{}
		}
	})
	return commonPasswords
}

// IsCommonPassword は、よく使われるパスワードの一覧に含まれるかを大文字小文字を無視して返す。
func IsCommonPassword(password string) bool {
	_, ok := commonPasswordSet()[strings.ToLower(password)]
	return ok
}

// validateNewPassword は会員登録・パスワード変更で新しく決めるパスワードを検証する。
// username は正規化済み（前後の空白を除いた）の利用者名。
func validateNewPassword(password, username string) *FieldError {
	switch {
	case !utf8.ValidString(password) || len(password) > MaxPasswordBytes:
		return &FieldError{Field: "password", Message: "パスワードはUTF-8で72バイト以内で入力してください"}
	case utf8.RuneCountInString(password) < MinPasswordChars:
		return &FieldError{Field: "password", Message: "パスワードは12文字以上で入力してください"}
	case username != "" && strings.EqualFold(password, username):
		return &FieldError{Field: "password", Message: "パスワードに利用者名と同じ文字列は使えません"}
	case IsCommonPassword(password):
		return &FieldError{Field: "password", Message: "よく使われるパスワードのため使えません。別のパスワードにしてください"}
	}
	return nil
}
