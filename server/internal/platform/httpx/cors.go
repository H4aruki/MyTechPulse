package httpx

import (
	"net/http"
	"slices"
	"strings"
)

var (
	corsMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}
	corsHeaders = []string{"Content-Type", CSRFHeader}
)

// originAllowed は列挙した許可Originとの完全一致だけを許可する。ワイルドカードは扱わない。
func originAllowed(allowed []string, origin string) bool {
	return origin != "" && origin != "*" && slices.Contains(allowed, origin)
}

// CORS は許可Originだけに認証付きのクロスオリジン通信を許す。
// Originが無い通信(同一オリジンのGETやcurlなど)には何もしない。
// 許可していないOriginからのpreflightは403にする。
func CORS(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		ok := originAllowed(allowed, origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !ok {
				WriteProblem(w, Problem{
					Type:   "about:blank",
					Title:  "Forbidden",
					Status: http.StatusForbidden,
					Detail: "許可されていないOriginです",
					Code:   "origin_not_allowed",
				})
				return
			}
			setCORSOrigin(w, origin)
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(corsMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(corsHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if ok {
			setCORSOrigin(w, origin)
			w.Header().Set("Access-Control-Expose-Headers", requestIDHeader)
		}
		next.ServeHTTP(w, r)
	})
}

func setCORSOrigin(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
}
