package passkey_test

// Service を直接呼ぶ、失敗系・境界の試験。依存と時計は handler_paths_test.go の包みを使う。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
)

// svcBegin は Service で登録を始め、端末へ渡す publicKey の中身を返す。
func (e *pathsEnv) svcBegin(userID int64, username string) (string, map[string]any) {
	e.t.Helper()
	token, creation, err := e.svc.BeginRegistration(context.Background(), userID, username)
	if err != nil {
		e.t.Fatalf("BeginRegistration: %v", err)
	}
	raw, err := json.Marshal(creation)
	if err != nil {
		e.t.Fatal(err)
	}
	var body struct {
		PublicKey map[string]any `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.PublicKey == nil {
		e.t.Fatalf("publicKey が無い: %v %s", err, raw)
	}
	return token, body.PublicKey
}

func (e *pathsEnv) credJSON(pub map[string]any, credID []byte) []byte {
	e.t.Helper()
	b, err := json.Marshal(attest(e.t, pub, credID, testOrigin))
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func TestServiceBeginRegistrationStopsOnStoreFailureWithoutSideEffects(t *testing.T) {
	boom := errors.New("db down")
	for name, inject := range map[string]func(e *pathsEnv){
		"ハンドルの保存": func(e *pathsEnv) { e.pk.errUserHandle = boom },
		"既存の一覧":   func(e *pathsEnv) { e.pk.errList = boom },
	} {
		t.Run(name, func(t *testing.T) {
			e := newPathsEnv(t)
			inject(e)
			token, creation, err := e.svc.BeginRegistration(context.Background(), 1, "alice")
			if !errors.Is(err, boom) || token != "" || creation != nil {
				t.Fatalf("token=%q creation=%v err=%v", token, creation, err)
			}
			if len(e.ch.created) != 0 || len(e.ch.cleanedAt) != 0 {
				t.Error("読み出しに失敗したのに、チャレンジを作った・清掃した")
			}
		})
	}
}

func TestServiceBeginRegistrationRefusesAnUnusableUserHandle(t *testing.T) {
	e := newPathsEnv(t)
	e.pk.emptyHandle = true // 空のハンドルを検証器は受け付けない
	token, creation, err := e.svc.BeginRegistration(context.Background(), 1, "alice")
	if err == nil || token != "" || creation != nil {
		t.Fatalf("token=%q creation=%v err=%v", token, creation, err)
	}
	if errors.Is(err, passkey.ErrLimitReached) || errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("別の原因として扱われている: %v", err)
	}
	if len(e.ch.created) != 0 {
		t.Error("開始できなかったのにチャレンジを保存した")
	}
	// HTTPでは、内部エラーとして原因を出さずに返す
	if r := e.do(http.MethodPost, base+"/register/begin", "tok-alice", "", nil, true); r.Code != http.StatusInternalServerError || r.json()["challengeToken"] != nil {
		t.Errorf("begin = %d %s", r.Code, r.Body)
	}
}

func TestServiceBeginRegistrationSurvivesCleanupFailureAndKeepsDetailsOutOfLogs(t *testing.T) {
	e := newPathsEnv(t)
	e.ch.errDeleteExpired = errors.New("cleanup failed: host=10.0.0.5")
	token, creation, err := e.svc.BeginRegistration(context.Background(), 1, "alice")
	if err != nil || token == "" || creation == nil {
		t.Fatalf("清掃の失敗で登録を止めてはいけない: token=%q err=%v", token, err)
	}
	if len(e.ch.created) != 1 {
		t.Fatalf("チャレンジが保存されていない: %d", len(e.ch.created))
	}
	if len(e.ch.cleanedAt) != 1 || !e.ch.cleanedAt[0].Equal(e.clock.now) {
		t.Errorf("清掃の時刻 = %v", e.ch.cleanedAt)
	}
	out := e.logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "cleanup failed") {
		t.Errorf("清掃の失敗が記録されていない: %q", out)
	}
	for _, secret := range []string{"10.0.0.5", token} {
		if strings.Contains(out, secret) {
			t.Errorf("ログに %q が含まれる: %s", secret, out)
		}
	}
}

func TestServiceBeginRegistrationReturnsNoTokenWhenChallengeCannotBeStored(t *testing.T) {
	e := newPathsEnv(t)
	boom := errors.New("insert failed")
	e.ch.errCreate = boom
	token, creation, err := e.svc.BeginRegistration(context.Background(), 1, "alice")
	if !errors.Is(err, boom) || token != "" || creation != nil {
		t.Fatalf("token=%q creation=%v err=%v", token, creation, err)
	}
}

func TestServiceFinishRegistrationChecksNameBeforeConsumingChallenge(t *testing.T) {
	e := newPathsEnv(t)
	token, pub := e.svcBegin(1, "alice")
	cred := e.credJSON(pub, newCredID())

	_, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, strings.Repeat("あ", 65), cred)
	if !errors.Is(err, passkey.ErrInvalidName) {
		t.Fatalf("err = %v", err)
	}
	if e.ch.consumeCalls != 0 {
		t.Fatal("名前が不正なだけでチャレンジを使い切ってはいけない")
	}
	// 空白だけの名前は「省略」と同じ扱い
	saved, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, " 　\t", cred)
	if err != nil || saved.Name != passkey.DefaultName {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestServiceFinishRegistrationNeverConsultsStoreWithoutToken(t *testing.T) {
	e := newPathsEnv(t)
	_, err := e.svc.FinishRegistration(context.Background(), 1, "alice", "", "x", []byte(`{}`))
	if !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("err = %v", err)
	}
	if e.ch.consumeCalls != 0 {
		t.Error("空のトークンで保存先を引いてはいけない")
	}
}

func TestServiceFinishRegistrationRejectsChallengesNotMadeForRegistration(t *testing.T) {
	e := newPathsEnv(t)
	userID := int64(1)
	otherUser := int64(2)
	live := e.clock.now.Add(passkey.ChallengeTTL)

	for name, c := range map[string]struct {
		purpose passkey.Purpose
		userID  *int64
		data    string
	}{
		"ログイン用のチャレンジ":      {passkey.PurposeLogin, &userID, `{}`},
		"利用者が結び付いていない":     {passkey.PurposeRegister, nil, `{}`},
		"別の利用者のチャレンジ":      {passkey.PurposeRegister, &otherUser, `{}`},
		"保存されたセッションが壊れている": {passkey.PurposeRegister, &userID, `{not json`},
	} {
		token, id, err := passkey.NewChallengeToken()
		if err != nil {
			t.Fatal(err)
		}
		if err := e.ch.Create(context.Background(), id, c.purpose, c.userID, []byte(c.data), live); err != nil {
			t.Fatal(err)
		}
		_, err = e.svc.FinishRegistration(context.Background(), 1, "alice", token, "x", []byte(`{}`))
		if !errors.Is(err, passkey.ErrChallengeInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if e.passkeys.count(1) != 0 || e.passkeys.count(2) != 0 {
		t.Error("誰にも登録されてはいけない")
	}
}

func TestServiceFinishRegistrationBurnsChallengeWhenUserLookupFails(t *testing.T) {
	e := newPathsEnv(t)
	token, pub := e.svcBegin(1, "alice")
	cred := e.credJSON(pub, newCredID())

	boom := errors.New("db down")
	e.pk.errUserHandle = boom
	if _, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, "x", cred); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	e.pk.errUserHandle = nil
	// 障害のあとに同じチャレンジを再利用させない
	if _, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, "x", cred); !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("再利用 err = %v", err)
	}
	if e.passkeys.count(1) != 0 {
		t.Error("登録されてはいけない")
	}
}

// 目的: Service が注入された時計の「いま」をチャレンジ保存先へ渡すこと。
// 期限の判定そのものはメモリ上の保存先(fake)の実装なので、ここでは本物の判定は確かめていない。
func TestServiceChallengeLivesUntilJustBeforeTTL(t *testing.T) {
	e := newPathsEnv(t)
	token, pub := e.svcBegin(1, "alice")
	e.clock.now = e.clock.now.Add(passkey.ChallengeTTL - time.Second)
	saved, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, "", e.credJSON(pub, newCredID()))
	if err != nil || saved.ID == 0 {
		t.Fatalf("期限の直前なら通る: saved=%+v err=%v", saved, err)
	}
}

// 目的: Service が名前を整えて検証してから保存先を呼び、利用者IDを取り違えずに渡すこと。
// 「他人のものは ErrNotFound」の判定はメモリ上の保存先(fake)の実装で、Service はそのエラーを素通しすることだけを見ている。
func TestServiceRenameTrimsAndValidatesBeforeTouchingTheStore(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "old")

	if err := e.svc.Rename(context.Background(), 1, id, "  new 　"); err != nil {
		t.Fatal(err)
	}
	if got := e.nameOf(1); got[0] != "new" {
		t.Errorf("name = %q", got[0])
	}
	e.pk.renameCalls = nil
	for _, bad := range []string{"", " \t ", strings.Repeat("x", 65)} {
		if err := e.svc.Rename(context.Background(), 1, id, bad); !errors.Is(err, passkey.ErrInvalidName) {
			t.Errorf("Rename(%q) = %v", bad, err)
		}
	}
	if len(e.pk.renameCalls) != 0 {
		t.Errorf("不正な名前で保存先へ進んだ: %q", e.pk.renameCalls)
	}
	// 他人のものは、保存先の ErrNotFound をそのまま返す(HTTPで404にする)
	if err := e.svc.Rename(context.Background(), 2, id, "x"); !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("他人のもの = %v", err)
	}
}

func TestServiceDeleteIsLoggedOnlyWhenItSucceeded(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	e.logs.Reset()

	if err := e.svc.Delete(context.Background(), 2, id); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("他人の削除 = %v", err)
	}
	boom := errors.New("db down")
	e.pk.errDelete = boom
	if err := e.svc.Delete(context.Background(), 1, id); !errors.Is(err, boom) {
		t.Fatalf("障害 = %v", err)
	}
	if strings.Contains(e.logs.String(), "passkey_deleted") {
		t.Errorf("削除できていないのに、削除の記録がある: %s", e.logs)
	}
	e.pk.errDelete = nil
	if err := e.svc.Delete(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.logs.String(), "passkey_deleted") {
		t.Errorf("削除の記録が無い: %s", e.logs)
	}
}

func TestServiceWithoutLoggerUsesDefaultSlog(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")

	// slog.SetDefault はプロセス全体を書き換えるので、この試験(とパッケージ内の他の試験)では t.Parallel を使わない。
	// t.Cleanup で、試験が終わったら元のロガーに戻す。
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	svc := *e.svc
	svc.Logger = nil
	if err := svc.Delete(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"passkey_deleted", "user_id=1", "passkey_id=" + strconv.FormatInt(id, 10)} {
		if !strings.Contains(out, want) {
			t.Errorf("既定のログに %q が無い: %s", want, out)
		}
	}
}

func TestServiceRejectedRegistrationIsLoggedWithoutSecrets(t *testing.T) {
	e := newPathsEnv(t)
	token, pub := e.svcBegin(1, "alice")
	credID := newCredID()
	cred := attest(t, pub, credID, "https://evil.example") // Originが違う
	raw, _ := json.Marshal(cred)

	_, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token, "x", raw)
	if !errors.Is(err, passkey.ErrVerificationFailed) {
		t.Fatalf("err = %v", err)
	}
	// 壊れた応答も同じ扱い
	token2, _ := e.svcBegin(1, "alice")
	if _, err := e.svc.FinishRegistration(context.Background(), 1, "alice", token2, "x", []byte(`{"id":"x"}`)); !errors.Is(err, passkey.ErrVerificationFailed) {
		t.Fatalf("壊れた応答 err = %v", err)
	}

	out := e.logs.String()
	if strings.Count(out, "passkey_register_failed") != 2 || !strings.Contains(out, "user_id=1") {
		t.Errorf("失敗の記録が足りない: %s", out)
	}
	response := cred["response"].(map[string]any)
	for name, secret := range map[string]string{
		"token":         token,
		"credential_id": b64.EncodeToString(credID),
		"challenge":     pub["challenge"].(string),
		"clientData":    response["clientDataJSON"].(string),
		"attestation":   response["attestationObject"].(string),
	} {
		if strings.Contains(out, secret) {
			t.Errorf("ログに %s が含まれる", name)
		}
	}
	if e.passkeys.count(1) != 0 {
		t.Error("登録されてはいけない")
	}
}
