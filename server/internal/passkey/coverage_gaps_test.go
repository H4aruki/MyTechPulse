package passkey_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
)

// 空のチャレンジトークンは、保存先を引かずにログイン失敗として数える。
func TestFinishLoginEmptyTokenIsCountedAsFailure(t *testing.T) {
	e := newLoginEnv(t)
	limiter := auth.NewLoginLimiter(e.clock)
	svc := passkey.Service{
		Passkeys: e.pk, Challenges: &memChallenges{rows: map[[32]byte]*memChallenge{}, consumeErr: errors.New("呼ばれてはいけない")},
		Clock: e.clock, Limiter: limiter,
	}
	for i := 0; i < 20; i++ {
		if _, err := svc.FinishLogin(context.Background(), "203.0.113.9", "", []byte("{}")); !errors.Is(err, passkey.ErrLoginFailed) {
			t.Fatalf("%d回目: err = %v, want ErrLoginFailed", i+1, err)
		}
	}
	// 失敗が数えられ、上限で締め出される
	_, err := svc.FinishLogin(context.Background(), "203.0.113.9", "", []byte("{}"))
	var tooMany *auth.TooManyAttemptsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("20回失敗したIPは締め出される: err = %v", err)
	}
}

// 署名の検証まで通っても、更新が0行(同時ログインで先を越された・削除された)なら、同じ401にして使えなくする。
func TestLoginRecordConflictIsTheSameFailure(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, false, false)
	e.pk.recordErr = passkey.ErrNotFound

	r := e.signIn(a, 6, reqOpts{ip: "198.51.100.7"})
	e.wantLoginFailure(r, "更新が競合したログイン")
	if !strings.Contains(e.logs.String(), "record_conflict") {
		t.Errorf("競合の分類が記録されていない: %s", e.logs)
	}
}

// 更新の障害(競合以外)は、失敗として数えず、サーバーエラーにする。
func TestLoginRecordFailureIsServerError(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, false, false)
	e.pk.recordErr = errors.New("db down")

	r := e.signIn(a, 6, reqOpts{ip: "198.51.100.8"})
	if r.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", r.Code, r.Body)
	}
}

// ZWJで連結できる絵文字の範囲(時計・手、記号、星などの2B00台)を、名前として受け付ける。
func TestValidateNameAcceptsZWJSequencesInEveryPictographicRange(t *testing.T) {
	const zwj = "‍"
	cases := map[string]string{
		"時計と絵文字": "⌚" + zwj + "\U0001F525",
		"星":      "⭐" + zwj + "\U0001F525",
		"記号":     "♀" + zwj + "\U0001F525",
		"絵文字と星":  "\U0001F525" + zwj + "⭐",
	}
	for name, v := range cases {
		if err := passkey.ValidateName(v); err != nil {
			t.Errorf("%s: %q を受け付けるべき: %v", name, v, err)
		}
	}
}

// 範囲の外の記号は、ZWJの相手にならない。
func TestValidateNameRejectsZWJNextToNonPictographic(t *testing.T) {
	const zwj = "‍"
	for name, v := range map[string]string{
		"左が文字":   "a" + zwj + "\U0001F525",
		"右が文字":   "\U0001F525" + zwj + "a",
		"範囲外の下":  "⋿" + zwj + "\U0001F525",
		"範囲外の上":  "\U0001F525" + zwj + "Ⰰ",
		"先頭のZWJ": zwj + "\U0001F525",
		"末尾のZWJ": "\U0001F525" + zwj,
	} {
		if err := passkey.ValidateName(v); err == nil {
			t.Errorf("%s: %q は拒否されるべき", name, v)
		}
	}
}
