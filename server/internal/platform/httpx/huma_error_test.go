package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestNewHumaErrorDropsValuesAndSetsCode(t *testing.T) {
	err := newHumaError(http.StatusUnprocessableEntity, "validation failed",
		&huma.ErrorDetail{Message: "expected length <= 50", Location: "body.password", Value: "leaked-value"},
		errors.New("plain"),
		nil,
	)
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(data), "leaked-value") {
		t.Fatalf("input value leaked: %s", data)
	}
	var p Problem
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Status != 422 || p.Code != "validation_failed" || p.Type != "about:blank" || p.Title != "Unprocessable Entity" {
		t.Fatalf("problem = %+v", p)
	}
	if len(p.Errors) != 2 || p.Errors[0].Field != "password" || p.Errors[1].Message != "plain" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestCodeForStatus(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:            "bad_request",
		http.StatusUnauthorized:          "unauthenticated",
		http.StatusForbidden:             "forbidden",
		http.StatusNotFound:              "not_found",
		http.StatusMethodNotAllowed:      "method_not_allowed",
		http.StatusNotAcceptable:         "not_acceptable",
		http.StatusConflict:              "conflict",
		http.StatusRequestEntityTooLarge: "payload_too_large",
		http.StatusUnsupportedMediaType:  "unsupported_media_type",
		http.StatusUnprocessableEntity:   "validation_failed",
		http.StatusTooManyRequests:       "too_many_requests",
		http.StatusInternalServerError:   "internal_error",
		http.StatusServiceUnavailable:    "service_unavailable",
		http.StatusTeapot:                "error",
		0:                                "error",
	}
	for status, want := range cases {
		if got := codeForStatus(status); got != want {
			t.Errorf("status %d = %q, want %q", status, got, want)
		}
	}
}

func TestErrorModelReportsStatusDetailAndContentType(t *testing.T) {
	p := NewProblem(http.StatusUnauthorized, "invalid_credentials", "認証に失敗しました")
	if p.Error() != "認証に失敗しました" {
		t.Fatalf("Error() = %q", p.Error())
	}
	if p.GetStatus() != http.StatusUnauthorized {
		t.Fatalf("status = %d", p.GetStatus())
	}
	if p.ContentType("application/json") != "application/problem+json" {
		t.Fatalf("json content type = %q", p.ContentType("application/json"))
	}
	if p.ContentType("text/plain") != "text/plain" || p.ContentType("") != "" {
		t.Fatalf("other content types must pass through: %q %q", p.ContentType("text/plain"), p.ContentType(""))
	}
}

func TestNewHumaErrorHidesBadRequestParserText(t *testing.T) {
	fragment := "leaked-fragment"
	err := newHumaError(http.StatusBadRequest, "bad request",
		&huma.ErrorDetail{Message: "invalid character '" + fragment + "'", Location: "body", Value: fragment},
	)
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(data), fragment) {
		t.Fatalf("parser text leaked: %s", data)
	}
	var p Problem
	if uErr := json.Unmarshal(data, &p); uErr != nil {
		t.Fatal(uErr)
	}
	if p.Code != "bad_request" || p.Status != http.StatusBadRequest || len(p.Errors) != 1 {
		t.Fatalf("problem = %+v", p)
	}
	if p.Errors[0].Field != "body" || p.Errors[0].Message != "リクエスト本文を解釈できません" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestNewHumaErrorUsesLocationSuffixAsField(t *testing.T) {
	err := newHumaError(http.StatusUnprocessableEntity, "validation failed",
		&huma.ErrorDetail{Message: "required", Location: "query.tag"},
		&huma.ErrorDetail{Message: "required", Location: "path.article_id"},
		&huma.ErrorDetail{Message: "required", Location: "header.X-Request-ID"},
		&huma.ErrorDetail{Message: "required", Location: "cookie.session"},
		&huma.ErrorDetail{Message: "required", Location: "username"},
		&huma.ErrorDetail{Message: "required", Location: "body.user.name"},
	)
	var p Problem
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if uErr := json.Unmarshal(data, &p); uErr != nil {
		t.Fatal(uErr)
	}
	want := []string{"tag", "article_id", "X-Request-ID", "session", "username", "user.name"}
	if len(p.Errors) != len(want) {
		t.Fatalf("errors = %+v", p.Errors)
	}
	for i, field := range want {
		if p.Errors[i].Field != field || p.Errors[i].Message != "required" {
			t.Fatalf("errors[%d] = %+v, want field %q", i, p.Errors[i], field)
		}
	}
}

func TestUseProblemErrorsInstallsStableProblemShape(t *testing.T) {
	prev, prevCtx := huma.NewError, huma.NewErrorWithContext
	t.Cleanup(func() {
		huma.NewError = prev
		huma.NewErrorWithContext = prevCtx
		// 関数だけ戻すと、Once は実行済みのまま残る。
		// 同じプロセスで繰り返す go test -count の2回目以降は差し替えが起きない。
		resetProblemErrorsOnce()
	})
	// 実行順で先に Once が済んでいても、この試験の中で差し替え直す。
	resetProblemErrorsOnce()

	UseProblemErrors()
	UseProblemErrors()

	fragment := "leaked-fragment"
	err := huma.NewError(http.StatusBadRequest, "bad request", &huma.ErrorDetail{
		Message:  "invalid character '" + fragment + "'",
		Location: "body.password",
		Value:    fragment,
	})
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(data), fragment) {
		t.Fatalf("installed error leaked input: %s", data)
	}
	if err.Error() != "bad request" || err.GetStatus() != http.StatusBadRequest {
		t.Fatalf("error = %q status %d", err.Error(), err.GetStatus())
	}
	var p Problem
	if uErr := json.Unmarshal(data, &p); uErr != nil {
		t.Fatal(uErr)
	}
	if p.Code != "bad_request" || len(p.Errors) != 1 || p.Errors[0].Message != "リクエスト本文を解釈できません" || p.Errors[0].Field != "password" {
		t.Fatalf("problem = %+v", p)
	}
	filter, ok := err.(interface{ ContentType(string) string })
	if !ok || filter.ContentType("application/json") != "application/problem+json" {
		t.Fatal("installed error must use problem+json")
	}

	missing := huma.NewErrorWithContext(nil, http.StatusNotFound, "見つかりません")
	data, mErr = json.Marshal(missing)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if uErr := json.Unmarshal(data, &p); uErr != nil {
		t.Fatal(uErr)
	}
	if missing.GetStatus() != http.StatusNotFound || p.Code != "not_found" || p.Detail != "見つかりません" {
		t.Fatalf("context error = %+v", p)
	}
}

// resetProblemErrorsOnce は UseProblemErrors の sync.Once を未実行に戻す。
// 関数ポインタだけを元に戻すと、次の UseProblemErrors は何もしない。
func resetProblemErrorsOnce() {
	installProblemErrorsOnce = sync.Once{}
}
