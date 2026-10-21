package passkey_test

import (
	"net/http"
	"testing"
	"time"
)

// 登録の開始・完了、名前変更、削除は、利用者ごとに1分に10回まで。一覧は対象外。
func TestManagementIsLimitedPerUser(t *testing.T) {
	clk := &clock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	e := newEnvWithLimitClock(t, clk)

	for i := 0; i < 10; i++ {
		if r := e.do(http.MethodPatch, base+"/999", "tok-alice", "", map[string]any{"name": "x"}, true); r.Code != http.StatusNotFound {
			t.Fatalf("rename %d = %d %s", i, r.Code, r.Body.String())
		}
	}
	r := e.do(http.MethodPost, base+"/register/begin", "tok-alice", "", nil, true)
	if r.Code != http.StatusTooManyRequests || r.code() != "too_many_requests" || r.Header().Get("Retry-After") != "6" {
		t.Fatalf("11th = %d %v %s", r.Code, r.Header(), r.Body.String())
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodDelete, base + "/999", nil},
		{http.MethodPatch, base + "/999", map[string]any{"name": "x"}},
		{http.MethodPost, base + "/register/finish", map[string]any{"challengeToken": "x", "credential": map[string]any{}}},
	} {
		if r := e.do(c.method, c.path, "tok-alice", "", c.body, true); r.Code != http.StatusTooManyRequests {
			t.Errorf("%s %s = %d", c.method, c.path, r.Code)
		}
	}
	// 一覧は数えない
	if r := e.do(http.MethodGet, base, "tok-alice", "", nil, false); r.Code != http.StatusOK {
		t.Fatalf("list = %d", r.Code)
	}
	// 別の利用者には影響しない。認証できない要求は今までどおり401
	if r := e.do(http.MethodDelete, base+"/999", "tok-bob", "", nil, true); r.Code != http.StatusNotFound {
		t.Fatalf("other user = %d", r.Code)
	}
	if r := e.do(http.MethodDelete, base+"/999", "", "", nil, true); r.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", r.Code)
	}
	clk.now = clk.now.Add(6 * time.Second)
	if r := e.do(http.MethodDelete, base+"/999", "tok-alice", "", nil, true); r.Code != http.StatusNotFound {
		t.Fatalf("after waiting = %d", r.Code)
	}
}
