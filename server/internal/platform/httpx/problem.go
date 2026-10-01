// Package httpx はHTTP共通処理(request ID、panic回復、アクセスログ、Problem Details)を提供する。
package httpx

import (
	"encoding/json"
	"net/http"
)

// FieldError は入力項目ごとのエラーを表す。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem は RFC 9457 の Problem Details。
type Problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Code     string       `json:"code,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// WriteProblem は p を application/problem+json として書き出す。
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
