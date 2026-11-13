package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblem(rec, Problem{
		Type:   "about:blank",
		Title:  "Bad Request",
		Status: http.StatusBadRequest,
		Detail: "入力が不正です",
		Code:   "invalid_input",
		Errors: []FieldError{{Field: "name", Message: "必須です"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q", ct)
	}
	var got Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "invalid_input" || len(got.Errors) != 1 || got.Errors[0].Field != "name" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

type failWriter struct {
	header http.Header
	code   int
}

func (f *failWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *failWriter) WriteHeader(code int) { f.code = code }

func (f *failWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteProblemKeepsStatusWhenBodyWriteFails(t *testing.T) {
	w := &failWriter{}
	WriteProblem(w, Problem{
		Type:   "about:blank",
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Detail: "サーバー内部でエラーが発生しました",
		Code:   "internal_error",
	})
	if w.code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q", ct)
	}
}
