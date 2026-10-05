package provider

import (
	"errors"
	"strconv"
	"time"
)

// 外部失敗の分類。errors.Is で判定する。
var (
	ErrTimeout         = errors.New("provider timeout")
	ErrRateLimited     = errors.New("provider rate limited")
	ErrInvalidResponse = errors.New("provider invalid response")
	ErrUnavailable     = errors.New("provider unavailable")
	// ErrInvalidRequest は、通信前に不正と分かった問い合わせ（空のタグ、許可外URLなど）。
	ErrInvalidRequest = errors.New("provider invalid request")
)

// Error は分類済みの外部失敗。応答本文、認証情報、検索語は保持しない。
type Error struct {
	Provider   string    // 提供元名（例: "qiita"）。未設定のこともある
	Kind       error     // 上の分類のいずれか
	StatusCode int       // 外部のHTTP状態。通信できなかった場合は0
	RetryAt    time.Time // 429のRetry-Afterから求めた再試行可能時刻。無ければゼロ値
	cause      error     // context由来のものだけを保持する（秘密を含まない）
}

func (e *Error) Error() string {
	msg := e.Kind.Error()
	if e.Provider != "" {
		msg = e.Provider + ": " + msg
	}
	if e.StatusCode != 0 {
		msg += " (status " + strconv.Itoa(e.StatusCode) + ")"
	}
	return msg
}

// Unwrap は分類と、あればcontext由来の原因を返す。
func (e *Error) Unwrap() []error {
	if e.cause != nil {
		return []error{e.Kind, e.cause}
	}
	return []error{e.Kind}
}

// Annotate は err が *Error で提供元名が未設定なら、名前を付けた複製を返す。
func Annotate(err error, providerName string) error {
	var pe *Error
	if !errors.As(err, &pe) || pe.Provider != "" {
		return err
	}
	cp := *pe
	cp.Provider = providerName
	return &cp
}

func newError(kind error, status int) *Error {
	return &Error{Kind: kind, StatusCode: status}
}
