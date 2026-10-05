package httpx

import (
	"net/http"
	"strings"
)

const (
	// CSRFHeader は更新系リクエストに付ける専用ヘッダー名。値は CSRFHeaderValue。
	CSRFHeader      = "X-MTP-CSRF"
	CSRFHeaderValue = "1"

	apiPrefix = "/api/v1/"
)

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// CSRF は /api/v1/ の更新系リクエストに、許可Originと専用ヘッダーの両方を要求する。
// Cookie認証のため、他サイトのフォームやスクリプトからの送信を拒否する。GETなどは対象外。
func CSRF(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, apiPrefix) || !isStateChanging(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if !originAllowed(allowed, r.Header.Get("Origin")) {
			writeCSRFRejection(w, "origin_not_allowed", "許可されていないOriginからの更新操作です")
			return
		}
		if r.Header.Get(CSRFHeader) != CSRFHeaderValue {
			writeCSRFRejection(w, "csrf_header_required", "更新操作には "+CSRFHeader+" ヘッダーが必要です")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeCSRFRejection(w http.ResponseWriter, code, detail string) {
	WriteProblem(w, Problem{
		Type:   "about:blank",
		Title:  "Forbidden",
		Status: http.StatusForbidden,
		Detail: detail,
		Code:   code,
	})
}
