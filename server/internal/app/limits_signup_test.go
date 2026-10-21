package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func invalidSignupFrom(h http.Handler, ip string) *httptest.ResponseRecorder {
	headers := map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1", ipHeader: ip}
	return post(h, "/api/v1/auth/signup", `{"username":"x","password":"short","favorite_tags":["go"]}`, headers)
}

func TestSignupAttemptsAreLimitedPerIPWithBurst(t *testing.T) {
	clock := newStepClock()
	h := newLimitedHandler(clock)
	for i := 0; i < 20; i++ {
		if rec := signupFrom(h, "203.0.113.1", i); rec.Code != http.StatusCreated {
			t.Fatalf("signup %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := signupFrom(h, "203.0.113.1", 20)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st signup = %d: %s", rec.Code, rec.Body.String())
	}
	// 60秒で1回分戻る
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"code":"too_many_requests"`) || len(rec.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("body = %s, cookies = %v", rec.Body.String(), rec.Header().Values("Set-Cookie"))
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("request id must be present on 429")
	}

	// 別のIPは数えが別
	if other := signupFrom(h, "203.0.113.2", 0); other.Code != http.StatusCreated {
		t.Fatalf("other IP signup = %d", other.Code)
	}
	clock.Advance(60 * time.Second)
	if again := signupFrom(h, "203.0.113.1", 21); again.Code != http.StatusCreated {
		t.Fatalf("signup after waiting = %d", again.Code)
	}
	if rec := signupFrom(h, "203.0.113.1", 22); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("only one token should have been refilled: %d", rec.Code)
	}
}

func TestSignupFailureBucketBlocksEvenValidInput(t *testing.T) {
	clock := newStepClock()
	h := newLimitedHandler(clock)
	for i := 0; i < 10; i++ {
		if rec := invalidSignupFrom(h, "203.0.113.9"); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid signup %d = %d", i, rec.Code)
		}
	}
	// 11回目は、正しい入力でも断る。90秒で1回分戻る
	rec := signupFrom(h, "203.0.113.9", 0)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "90" {
		t.Fatalf("signup after 10 failures = %d %v", rec.Code, rec.Header())
	}
	// 断られた要求は失敗に数えず、待ち時間も延びない
	clock.Advance(30 * time.Second)
	rec = signupFrom(h, "203.0.113.9", 0)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("after 30s = %d %v", rec.Code, rec.Header())
	}
	// 別のIPは影響を受けない
	if other := signupFrom(h, "203.0.113.10", 0); other.Code != http.StatusCreated {
		t.Fatalf("other IP signup = %d", other.Code)
	}
	clock.Advance(60 * time.Second)
	if rec := signupFrom(h, "203.0.113.9", 1); rec.Code != http.StatusCreated {
		t.Fatalf("signup after waiting = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSignupConflictsCountAsFailures(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	if rec := signupFrom(h, "203.0.113.30", 0); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d", rec.Code)
	}
	for i := 0; i < 10; i++ {
		if rec := signupFrom(h, "203.0.113.30", 0); rec.Code != http.StatusConflict {
			t.Fatalf("duplicate %d = %d", i, rec.Code)
		}
	}
	if rec := signupFrom(h, "203.0.113.30", 1); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("signup after 10 conflicts = %d", rec.Code)
	}
}

func TestSignupSuccessesAreNotCountedAsFailures(t *testing.T) {
	clock := newStepClock()
	h := newLimitedHandler(clock)
	// 成功を20回(失敗の枠は10回)繰り返しても、失敗の枠では断られない
	for i := 0; i < 20; i++ {
		if rec := signupFrom(h, "203.0.113.40", i); rec.Code != http.StatusCreated {
			t.Fatalf("signup %d = %d", i, rec.Code)
		}
	}
	// 試行の枠が戻れば通る(断られた理由は試行の枠だけ)
	clock.Advance(time.Minute)
	if rec := signupFrom(h, "203.0.113.40", 20); rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d", rec.Code)
	}
}

func TestSignupFailureBucketIsPerIP(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	for i := 0; i < 10; i++ {
		invalidSignupFrom(h, "203.0.113.50")
	}
	if rec := invalidSignupFrom(h, "203.0.113.50"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th invalid = %d", rec.Code)
	}
	if rec := invalidSignupFrom(h, "203.0.113.51"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("other IP = %d", rec.Code)
	}
}

// 本文の形が壊れている(400)・大きすぎる(413)要求は、失敗の枠を使わない(試行の枠だけを使う)。
func TestSignupMalformedAndOversizedBodiesAreNotCountedAsFailures(t *testing.T) {
	clock := newStepClock()
	h := newLimitedHandler(clock)
	headers := map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1", ipHeader: "203.0.113.30"}
	for i := 0; i < 11; i++ {
		if rec := post(h, "/api/v1/auth/signup", `{"username":`, headers); rec.Code != http.StatusBadRequest {
			t.Fatalf("malformed %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	big := `{"username":"` + strings.Repeat("a", 70<<10) + `"}`
	if rec := post(h, "/api/v1/auth/signup", big, headers); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", rec.Code)
	}
	if rec := signupFrom(h, "203.0.113.30", 0); rec.Code != http.StatusCreated {
		t.Fatalf("valid signup after 400/413 = %d: %s", rec.Code, rec.Body.String())
	}
}
