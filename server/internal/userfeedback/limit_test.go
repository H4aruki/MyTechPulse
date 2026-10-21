package userfeedback_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestFeedbackWritesAreLimitedPerUser(t *testing.T) {
	h := newHandler(&fakeRepo{form: validForm()})
	body := `{"prompt_id":"` + promptID + `"}`
	for i := 0; i < 10; i++ {
		if got := send(h, http.MethodPost, "/api/v1/user-feedback/dismissals", body, true, true); got.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited too early", i)
		}
	}
	got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", body, true, true)
	if got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") != "6" || !strings.Contains(got.Body.String(), `"code":"too_many_requests"`) {
		t.Fatalf("11th = %d %v %s", got.Code, got.Header(), got.Body.String())
	}
	// 認証できない要求は、枠を使わず今までどおり401
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", body, false, true); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", got.Code)
	}
	// 状態の確認は、この枠の対象ではない
	if got := send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false); got.Code == http.StatusTooManyRequests {
		t.Fatalf("status must not be limited: %d", got.Code)
	}
}
