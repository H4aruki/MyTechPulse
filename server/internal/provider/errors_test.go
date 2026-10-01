package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestErrorClassification(t *testing.T) {
	kinds := []error{ErrTimeout, ErrRateLimited, ErrInvalidResponse, ErrUnavailable, ErrInvalidRequest}
	for _, kind := range kinds {
		err := error(newError(kind, 0))
		for _, other := range kinds {
			if got, want := errors.Is(err, other), kind == other; got != want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", kind, other, got, want)
			}
		}
	}
}

func TestErrorRateLimitedKeepsStatusAndRetryAt(t *testing.T) {
	retryAt := time.Date(2026, 9, 14, 0, 0, 5, 0, time.UTC)
	var err error = &Error{Kind: ErrRateLimited, StatusCode: 429, RetryAt: retryAt}
	wrapped := fmt.Errorf("feed: %w", err)

	if !errors.Is(wrapped, ErrRateLimited) {
		t.Fatal("429はerrors.Isで判定できる必要があります")
	}
	var pe *Error
	if !errors.As(wrapped, &pe) {
		t.Fatal("errors.Asで取り出せる必要があります")
	}
	if pe.StatusCode != 429 || !pe.RetryAt.Equal(retryAt) {
		t.Fatalf("status/retryAtが保持されていません: %+v", pe)
	}
}

func TestErrorKeepsContextCause(t *testing.T) {
	err := &Error{Kind: ErrTimeout, cause: context.DeadlineExceeded}
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("分類とcontext由来の原因の両方を判定できる必要があります")
	}
}

func TestErrorMessageHasProviderAndStatusOnly(t *testing.T) {
	err := &Error{Provider: "qiita", Kind: ErrUnavailable, StatusCode: 503}
	if got, want := err.Error(), "qiita: provider unavailable (status 503)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestAnnotate(t *testing.T) {
	base := newError(ErrTimeout, 0)
	got := Annotate(fmt.Errorf("wrap: %w", base), "zenn")
	var pe *Error
	if !errors.As(got, &pe) || pe.Provider != "zenn" {
		t.Fatalf("提供元名が付いていません: %v", got)
	}
	if base.Provider != "" {
		t.Fatal("元のエラーを書き換えてはいけません")
	}
	if again := Annotate(got, "qiita"); !strings.Contains(again.Error(), "zenn") {
		t.Fatal("設定済みの提供元名は上書きしません")
	}
	plain := errors.New("plain")
	if Annotate(plain, "zenn") != plain {
		t.Fatal("*Error以外はそのまま返します")
	}
	if Annotate(nil, "zenn") != nil {
		t.Fatal("nilはnilのまま返します")
	}
}
