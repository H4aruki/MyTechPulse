package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func csrfRequest(method, path, origin, csrf string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set(CSRFHeader, csrf)
	}
	return req
}

func TestCSRFRequiresOriginAndHeaderForStateChanges(t *testing.T) {
	cases := []struct {
		name         string
		origin, csrf string
		want         int
		wantCode     string
	}{
		{"both ok", allowedOrigin, "1", http.StatusOK, ""},
		{"origin missing", "", "1", http.StatusForbidden, "origin_not_allowed"},
		{"origin mismatch", "https://evil.example", "1", http.StatusForbidden, "origin_not_allowed"},
		{"origin null", "null", "1", http.StatusForbidden, "origin_not_allowed"},
		{"header missing", allowedOrigin, "", http.StatusForbidden, "csrf_header_required"},
		{"header wrong value", allowedOrigin, "0", http.StatusForbidden, "csrf_header_required"},
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, tc := range cases {
			t.Run(method+" "+tc.name, func(t *testing.T) {
				h := CSRF([]string{allowedOrigin}, okHandler())
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, csrfRequest(method, "/api/v1/auth/login", tc.origin, tc.csrf))
				if rec.Code != tc.want {
					t.Fatalf("status = %d, want %d", rec.Code, tc.want)
				}
				if tc.wantCode == "" {
					return
				}
				var p Problem
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != tc.wantCode {
					t.Fatalf("problem = %+v, %v", p, err)
				}
			})
		}
	}
}

func TestCSRFDoesNotRequireHeaderForSafeMethods(t *testing.T) {
	h := CSRF([]string{allowedOrigin}, okHandler())
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, csrfRequest(method, "/api/v1/auth/me", "", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", method, rec.Code)
		}
	}
}

func TestCSRFOnlyGuardsAPIPaths(t *testing.T) {
	h := CSRF([]string{allowedOrigin}, okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, csrfRequest(http.MethodPost, "/health/live", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("non-API path must not be guarded, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, csrfRequest(http.MethodPost, "/api/v1/anything", "", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("API path must be guarded, got %d", rec.Code)
	}
}
