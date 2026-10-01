// Package logging は slog のJSON出力設定を提供する。
package logging

import (
	"io"
	"log/slog"
)

// New は指定レベル以上をJSON1行で w へ出力するロガーを返す。
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
