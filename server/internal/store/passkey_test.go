package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

func TestPasskeyUserID32(t *testing.T) {
	for _, id := range []int64{0, -1, math.MinInt64, math.MaxInt32 + 1, math.MaxInt64} {
		if _, err := passkeyUserID32(id); !errors.Is(err, passkey.ErrNotFound) {
			t.Errorf("id=%d: err = %v, want ErrNotFound", id, err)
		}
	}
	for _, id := range []int64{1, 42, math.MaxInt32} {
		got, err := passkeyUserID32(id)
		if err != nil || int64(got) != id {
			t.Errorf("id=%d: got (%d, %v)", id, got, err)
		}
	}
}

func TestToCredentialMapsEveryField(t *testing.T) {
	created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	used := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	row := dbgen.AuthPasskey{
		ID: 7, UserID: 3, CredentialID: []byte{1, 2}, PublicKey: []byte{3}, SignCount: 9,
		Transports: []string{"internal"}, Aaguid: []byte{4}, BackupEligible: true, BackupState: true, Name: "手元の鍵",
		CreatedAt:  pgtype.Timestamptz{Time: created, Valid: true},
		LastUsedAt: pgtype.Timestamptz{Time: used, Valid: true},
	}
	c := toCredential(row)
	if c.ID != 7 || c.UserID != 3 || c.SignCount != 9 || c.Name != "手元の鍵" || !c.BackupEligible || !c.BackupState {
		t.Errorf("scalar fields = %+v", c)
	}
	if string(c.CredentialID) != "\x01\x02" || string(c.PublicKey) != "\x03" || string(c.AAGUID) != "\x04" || len(c.Transports) != 1 {
		t.Errorf("slice fields = %+v", c)
	}
	if !c.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v", c.CreatedAt)
	}
	if c.LastUsedAt == nil || !c.LastUsedAt.Equal(used) {
		t.Errorf("LastUsedAt = %v", c.LastUsedAt)
	}
}

func TestToCredentialLeavesLastUsedAtNilWhenNeverUsed(t *testing.T) {
	if c := toCredential(dbgen.AuthPasskey{}); c.LastUsedAt != nil {
		t.Errorf("LastUsedAt = %v, want nil", c.LastUsedAt)
	}
}

func TestToCredentialClampsSignCountToUint32(t *testing.T) {
	for name, tc := range map[string]struct {
		in   int64
		want uint32
	}{
		"負":    {-5, 0},
		"0":    {0, 0},
		"通常":   {123, 123},
		"上限":   {math.MaxUint32, math.MaxUint32},
		"上限超え": {math.MaxUint32 + 10, math.MaxUint32},
	} {
		if got := toCredential(dbgen.AuthPasskey{SignCount: tc.in}).SignCount; got != tc.want {
			t.Errorf("%s: SignCount(%d) = %d, want %d", name, tc.in, got, tc.want)
		}
	}
}

func TestTimestamptzIsValid(t *testing.T) {
	now := time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC)
	if got := timestamptz(now); !got.Valid || !got.Time.Equal(now) {
		t.Errorf("timestamptz = %+v", got)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	unique := &pgconn.PgError{Code: pgUniqueViolation}
	if !isUniqueViolation(unique) || !isUniqueViolation(errors.Join(errors.New("wrap"), unique)) {
		t.Error("一意制約違反を見分けられない")
	}
	if isUniqueViolation(&pgconn.PgError{Code: "23503"}) || isUniqueViolation(errors.New("x")) || isUniqueViolation(nil) {
		t.Error("一意制約違反以外を誤って見分けた")
	}
}

// 範囲外の利用者IDは、DBへ触れる前に ErrNotFound で返る(接続プールが無くても動く)。
func TestPasskeyRejectsOutOfRangeUserIDsBeforeTouchingTheDB(t *testing.T) {
	p := &Passkey{}
	ctx := context.Background()
	for _, id := range []int64{0, -3, math.MaxInt32 + 1} {
		checks := map[string]error{
			"UserHandle": func() error { _, err := p.UserHandle(ctx, id, []byte{1}); return err }(),
			"Create":     func() error { _, err := p.Create(ctx, passkey.Credential{UserID: id}); return err }(),
			"ListByUser": func() error { _, err := p.ListByUser(ctx, id); return err }(),
			"Rename":     p.Rename(ctx, id, 1, "x"),
			"Delete":     p.Delete(ctx, id, 1),
		}
		for name, err := range checks {
			if !errors.Is(err, passkey.ErrNotFound) {
				t.Errorf("%s(userID=%d): err = %v, want ErrNotFound", name, id, err)
			}
		}
	}
}

func TestPasskeyChallengeCreateRejectsOutOfRangeUserID(t *testing.T) {
	c := &PasskeyChallenge{}
	bad := int64(0)
	err := c.Create(context.Background(), [32]byte{}, passkey.PurposeRegister, &bad, nil, time.Now())
	if !errors.Is(err, passkey.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// 接続できないプールと取り消し済みの文脈で、DB障害の分岐を確かめる。
// 障害は固定の文言にまとめ、DBの文言を外へ出さない。
func TestPasskeyDatabaseFailuresAreMaskedAndNotNotFound(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewPasskey(pool)
	c := NewPasskeyChallenge(pool)
	now := time.Now()
	uid := int64(1)

	checks := map[string]error{
		"UserHandle":              func() error { _, err := p.UserHandle(ctx, 1, []byte{1}); return err }(),
		"UserIDByHandle":          func() error { _, err := p.UserIDByHandle(ctx, []byte{1}); return err }(),
		"Create":                  func() error { _, err := p.Create(ctx, passkey.Credential{UserID: 1}); return err }(),
		"ListByUser":              func() error { _, err := p.ListByUser(ctx, 1); return err }(),
		"FindByCredential":        func() error { _, err := p.FindByCredentialID(ctx, []byte{1}); return err }(),
		"RecordUse":               p.RecordUse(ctx, []byte{1}, 1, false, now),
		"Rename":                  p.Rename(ctx, 1, 1, "x"),
		"Delete":                  p.Delete(ctx, 1, 1),
		"Challenge.Create":        c.Create(ctx, [32]byte{}, passkey.PurposeLogin, &uid, nil, now),
		"Challenge.Create(利用者なし)": c.Create(ctx, [32]byte{}, passkey.PurposeLogin, nil, nil, now),
		"Consume":                 func() error { _, err := c.Consume(ctx, [32]byte{}, passkey.PurposeLogin, now); return err }(),
		"DeleteExpired":           c.DeleteExpired(ctx, now),
	}
	for name, err := range checks {
		switch {
		case err == nil:
			t.Errorf("%s: DB障害なのにエラーが返らない", name)
		case errors.Is(err, passkey.ErrNotFound), errors.Is(err, passkey.ErrChallengeInvalid), errors.Is(err, passkey.ErrLimitReached), errors.Is(err, passkey.ErrCredentialExists):
			t.Errorf("%s: DB障害を業務上のエラーに取り違えている: %v", name, err)
		case errors.Is(err, context.Canceled):
			t.Errorf("%s: DBの原因をそのまま外へ返している: %v", name, err)
		}
	}
}
