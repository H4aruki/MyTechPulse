package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// フロントの LEGAL_VERSION とサーバーの CurrentTermsVersion が、ずれると登録が全部422になる。値の一致を確かめる。
func TestCurrentTermsVersionMatchesFrontend(t *testing.T) {
	// このファイルは server/internal/auth/ にあるので、リポジトリの根は3つ上。
	path := filepath.Join("..", "..", "..", "frontend", "src", "lib", "legal.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("legal.ts を読めません: %v", err)
	}
	m := regexp.MustCompile(`(?m)^export const LEGAL_VERSION\s*=\s*'([^']+)'`).FindSubmatch(src)
	if m == nil {
		t.Fatalf("%s に LEGAL_VERSION の定義が見つかりません", path)
	}
	if got := string(m[1]); got != CurrentTermsVersion {
		t.Fatalf("LEGAL_VERSION = %q, CurrentTermsVersion = %q: 同じ値にしてください", got, CurrentTermsVersion)
	}
}

func TestServiceSignupRequiresCurrentTermsVersion(t *testing.T) {
	for name, version := range map[string]string{
		"missing":   "",
		"old":       "2020-01-old",
		"different": CurrentTermsVersion + "x",
		"spaces":    " " + CurrentTermsVersion,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			in := validSignup()
			in.AgreedTermsVersion = version
			_, _, _, err := h.svc.Signup(context.Background(), in)
			var ve *ValidationError
			if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Field != "agreed_terms_version" {
				t.Fatalf("err = %v", err)
			}
			if len(h.store.users) != 0 || len(h.store.createdTerms) != 0 {
				t.Fatal("同意がないときは利用者を作らない")
			}
		})
	}
}

func TestServiceSignupRecordsAgreedTermsVersion(t *testing.T) {
	h := newHarness()
	if _, _, _, err := h.svc.Signup(context.Background(), validSignup()); err != nil {
		t.Fatal(err)
	}
	if got := h.store.createdTerms; len(got) != 1 || got[0] != CurrentTermsVersion {
		t.Fatalf("createdTerms = %v", got)
	}
}

func TestHandlerSignupRejectsMissingOrStaleAgreement(t *testing.T) {
	const base = `{"username":"synthetic-user","password":"synthetic-pass-12","favorite_tags":["go"]`
	cases := map[string]string{
		"absent": base + `}`,
		"empty":  base + `,"agreed_terms_version":""}`,
		"stale":  base + `,"agreed_terms_version":"2020-01-old"}`,
		"null":   base + `,"agreed_terms_version":null}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			a := newAPIHarness("mtp_session", false)
			rec := a.do(http.MethodPost, "/api/v1/auth/signup", body)
			assertProblem(t, rec, http.StatusUnprocessableEntity, "validation_failed")
			if len(setCookies(rec)) != 0 {
				t.Fatal("同意がないときはCookieを発行しない")
			}
			if len(a.store.users) != 0 || len(a.store.createdTerms) != 0 {
				t.Fatal("同意がないときは利用者も記録も作らない")
			}
		})
	}
}

func TestHandlerSignupRecordsAgreement(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	rec := a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := a.store.createdTerms; len(got) != 1 || got[0] != CurrentTermsVersion {
		t.Fatalf("createdTerms = %v", got)
	}
	if strings.Contains(rec.Body.String(), "agreed_terms_version") {
		t.Fatal("応答本文に同意の版を出さない")
	}
}
