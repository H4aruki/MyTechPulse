package httpx

import (
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
)

// ErrorModel はHumaが返す4xx/5xxのProblem Details(RFC 9457)。
// 標準のHuma版と違い、安定した code を持ち、入力値そのものは応答へ含めない
// (パスワードなどが入力不正の応答に載らないようにするため)。
type ErrorModel struct {
	Type     string       `json:"type" example:"about:blank" doc:"問題の種類を示すURI。固有の種類が無いときはabout:blank"`
	Title    string       `json:"title" example:"Unauthorized" doc:"HTTPステータスの短い名前"`
	Status   int          `json:"status" example:"401" doc:"HTTPステータス"`
	Detail   string       `json:"detail,omitempty" doc:"今回の問題の説明(利用者向け)"`
	Instance string       `json:"instance,omitempty" doc:"問題の発生箇所を示すURI(使用しない場合は省略)"`
	Code     string       `json:"code" example:"invalid_credentials" doc:"機械が判定できる安定した業務エラーコード"`
	Errors   []FieldError `json:"errors,omitempty" doc:"入力項目ごとのエラー"`
}

func (e *ErrorModel) Error() string { return e.Detail }

// GetStatus はHumaがHTTPステータスを決めるために使う。
func (e *ErrorModel) GetStatus() int { return e.Status }

// ContentType はJSONの応答を application/problem+json にする。
func (e *ErrorModel) ContentType(ct string) string {
	if ct == "application/json" {
		return "application/problem+json"
	}
	return ct
}

// NewProblem は業務エラーコード付きのProblemを作る。
func NewProblem(status int, code, detail string, fields ...FieldError) *ErrorModel {
	return &ErrorModel{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
		Errors: fields,
	}
}

var statusCodes = map[int]string{
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
}

func codeForStatus(status int) string {
	if code, ok := statusCodes[status]; ok {
		return code
	}
	return "error"
}

// fieldFromLocation は "body.username" のような場所を "username" にする。
func fieldFromLocation(location string) string {
	for _, prefix := range []string{"body.", "query.", "path.", "header.", "cookie."} {
		if strings.HasPrefix(location, prefix) {
			return strings.TrimPrefix(location, prefix)
		}
	}
	return location
}

func newHumaError(status int, msg string, errs ...error) huma.StatusError {
	p := NewProblem(status, codeForStatus(status), msg)
	for _, err := range errs {
		if err == nil {
			continue
		}
		if d, ok := err.(huma.ErrorDetailer); ok {
			detail := d.ErrorDetail()
			message := detail.Message
			if status == http.StatusBadRequest {
				// JSONの構文エラーなどは、解析器の文言に入力の一部が含まれ得るため固定文にする
				message = "リクエスト本文を解釈できません"
			}
			// 入力値(Value)は応答へ含めない
			p.Errors = append(p.Errors, FieldError{Field: fieldFromLocation(detail.Location), Message: message})
			continue
		}
		p.Errors = append(p.Errors, FieldError{Message: err.Error()})
	}
	return p
}

var installProblemErrorsOnce sync.Once

// UseProblemErrors はHumaのエラー応答を ErrorModel に置き換える。何度呼んでもよい。
func UseProblemErrors() {
	installProblemErrorsOnce.Do(func() {
		huma.NewError = newHumaError
		huma.NewErrorWithContext = func(_ huma.Context, status int, msg string, errs ...error) huma.StatusError {
			return newHumaError(status, msg, errs...)
		}
	})
}
