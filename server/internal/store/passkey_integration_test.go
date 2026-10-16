package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

func newPasskeyUser(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO "user"(user_name, password) VALUES ($1, 'h') RETURNING "user_ID"`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func credential(userID int64, n byte) passkey.Credential {
	return passkey.Credential{
		UserID: userID, CredentialID: []byte{n, 1}, PublicKey: []byte{n, 2}, SignCount: 3,
		Transports: []string{"internal"}, AAGUID: bytes.Repeat([]byte{n}, 16), BackupEligible: true, Name: fmt.Sprintf("key%d", n),
	}
}

func TestPasskeyUserHandleIsStableAndUnique(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")

	first, _ := passkey.NewUserHandle()
	got, err := repo.UserHandle(ctx, alice, first)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("first handle = %x, %v", got, err)
	}
	second, _ := passkey.NewUserHandle()
	got, err = repo.UserHandle(ctx, alice, second)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("handle must stay the stored one: %x, %v", got, err)
	}
	bobHandle, _ := passkey.NewUserHandle()
	if got, _ := repo.UserHandle(ctx, bob, bobHandle); bytes.Equal(got, first) {
		t.Fatal("users must have different handles")
	}
	id, err := repo.UserIDByHandle(ctx, first)
	if err != nil || id != alice {
		t.Fatalf("UserIDByHandle = %d, %v", id, err)
	}
	if _, err := repo.UserIDByHandle(ctx, bytes.Repeat([]byte{7}, 32)); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("unknown handle must be ErrNotFound: %v", err)
	}
}

func TestPasskeyCreateListFindAndRecordUse(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")

	created, err := repo.Create(ctx, credential(alice, 1))
	if err != nil || created.ID == 0 || created.LastUsedAt != nil || created.CreatedAt.IsZero() {
		t.Fatalf("Create = %+v, %v", created, err)
	}
	if _, err := repo.Create(ctx, credential(alice, 1)); !errors.Is(err, passkey.ErrCredentialExists) {
		t.Fatalf("duplicate credential id must be ErrCredentialExists: %v", err)
	}
	other := newPasskeyUser(t, pool, "bob")
	if _, err := repo.Create(ctx, credential(other, 1)); !errors.Is(err, passkey.ErrCredentialExists) {
		t.Fatalf("credential id must be unique across users: %v", err)
	}
	if _, err := repo.Create(ctx, credential(999, 9)); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("unknown user must be ErrNotFound: %v", err)
	}
	if _, err := repo.Create(ctx, credential(alice, 2)); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListByUser(ctx, alice)
	if err != nil || len(list) != 2 || list[0].Name != "key1" || list[1].Name != "key2" {
		t.Fatalf("ListByUser = %+v, %v", list, err)
	}
	if got, _ := repo.ListByUser(ctx, other); len(got) != 0 {
		t.Fatalf("other user's list must be empty: %+v", got)
	}

	found, err := repo.FindByCredentialID(ctx, []byte{1, 1})
	if err != nil || found.UserID != alice || found.SignCount != 3 || !found.BackupEligible ||
		len(found.Transports) != 1 || !bytes.Equal(found.AAGUID, bytes.Repeat([]byte{1}, 16)) {
		t.Fatalf("FindByCredentialID = %+v, %v", found, err)
	}
	if _, err := repo.FindByCredentialID(ctx, []byte{9, 9}); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("unknown credential must be ErrNotFound: %v", err)
	}

	if err := repo.RecordUse(ctx, []byte{1, 1}, 4, true, fixedNow); err != nil {
		t.Fatal(err)
	}
	found, _ = repo.FindByCredentialID(ctx, []byte{1, 1})
	if found.SignCount != 4 || !found.BackupState || found.LastUsedAt == nil || !found.LastUsedAt.Equal(fixedNow) {
		t.Fatalf("after RecordUse = %+v", found)
	}
	if err := repo.RecordUse(ctx, []byte{9, 9}, 1, false, fixedNow); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("RecordUse on unknown credential must be ErrNotFound: %v", err)
	}
}

func TestPasskeyLimitIsEnforcedEvenWhenConcurrent(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")

	var wg sync.WaitGroup
	var ok, limited atomic.Int32
	for i := 0; i < passkey.MaxPasskeysPerUser+5; i++ {
		wg.Add(1)
		go func(n byte) {
			defer wg.Done()
			switch _, err := repo.Create(ctx, credential(alice, n)); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, passkey.ErrLimitReached):
				limited.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(byte(i + 1))
	}
	wg.Wait()
	if ok.Load() != passkey.MaxPasskeysPerUser || limited.Load() != 5 {
		t.Fatalf("ok=%d limited=%d", ok.Load(), limited.Load())
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_passkey WHERE "user_ID" = $1`, alice); n != passkey.MaxPasskeysPerUser {
		t.Fatalf("stored = %d", n)
	}
}

func TestPasskeyRenameAndDeleteAreScopedToOwner(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	mine, _ := repo.Create(ctx, credential(alice, 1))

	if err := repo.Rename(ctx, bob, mine.ID, "hijack"); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("renaming someone else's passkey must be ErrNotFound: %v", err)
	}
	if err := repo.Delete(ctx, bob, mine.ID); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("deleting someone else's passkey must be ErrNotFound: %v", err)
	}
	if got, _ := repo.FindByCredentialID(ctx, mine.CredentialID); got.Name != "key1" {
		t.Fatalf("passkey must be untouched: %+v", got)
	}
	if err := repo.Rename(ctx, alice, mine.ID, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.FindByCredentialID(ctx, mine.CredentialID); got.Name != "iPhone" {
		t.Fatalf("name = %q", got.Name)
	}
	if err := repo.Delete(ctx, alice, mine.ID); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_passkey`); n != 0 {
		t.Fatalf("passkey must be physically deleted, rows = %d", n)
	}
	if err := repo.Delete(ctx, alice, mine.ID); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("second delete must be ErrNotFound: %v", err)
	}
}

func TestPasskeyChallengeIsSingleUseAndExpires(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskeyChallenge(pool)
	alice := newPasskeyUser(t, pool, "alice")
	expires := fixedNow.Add(passkey.ChallengeTTL)

	if err := repo.Create(ctx, hashOf(1), passkey.PurposeRegister, &alice, []byte(`{"challenge":"x"}`), expires); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, hashOf(1), passkey.PurposeRegister, &alice, []byte(`{}`), expires); err == nil {
		t.Fatal("duplicate id must be rejected")
	}
	// 用途が違う・期限切れでは使えず、使えなかった試行はチャレンジを消費しない
	if _, err := repo.Consume(ctx, hashOf(1), passkey.PurposeLogin, fixedNow); !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("wrong purpose must be invalid: %v", err)
	}
	if _, err := repo.Consume(ctx, hashOf(1), passkey.PurposeRegister, expires); !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("expired must be invalid: %v", err)
	}
	if _, err := repo.Consume(ctx, hashOf(2), passkey.PurposeRegister, fixedNow); !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("unknown must be invalid: %v", err)
	}
	got, err := repo.Consume(ctx, hashOf(1), passkey.PurposeRegister, fixedNow)
	if err != nil || got.UserID == nil || *got.UserID != alice || !bytes.Contains(got.SessionData, []byte(`"x"`)) {
		t.Fatalf("Consume = %+v (%s), %v", got, got.SessionData, err)
	}
	if _, err := repo.Consume(ctx, hashOf(1), passkey.PurposeRegister, fixedNow); !errors.Is(err, passkey.ErrChallengeInvalid) {
		t.Fatalf("reuse must be invalid: %v", err)
	}
}

func TestPasskeyChallengeConsumeSucceedsOnlyOnceWhenConcurrent(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskeyChallenge(pool)
	if err := repo.Create(ctx, hashOf(1), passkey.PurposeLogin, nil, []byte(`{}`), fixedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := repo.Consume(ctx, hashOf(1), passkey.PurposeLogin, fixedNow)
			if err == nil {
				if c.UserID != nil {
					t.Errorf("login challenge must have no user: %+v", c)
				}
				ok.Add(1)
			} else if !errors.Is(err, passkey.ErrChallengeInvalid) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("consume succeeded %d times, want 1", ok.Load())
	}
}

func TestPasskeyChallengeDeleteExpired(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskeyChallenge(pool)
	for i, expires := range []time.Time{fixedNow.Add(-time.Minute), fixedNow.Add(time.Minute), fixedNow.Add(time.Minute)} {
		if err := repo.Create(ctx, hashOf(byte(i+1)), passkey.PurposeLogin, nil, []byte(`{}`), expires); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Consume(ctx, hashOf(3), passkey.PurposeLogin, fixedNow); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteExpired(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	// 期限切れ(1)と使用済み(3)が消え、有効なもの(2)だけ残る
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_webauthn_challenge`); n != 1 {
		t.Fatalf("remaining = %d, want 1", n)
	}
	if _, err := repo.Consume(ctx, hashOf(2), passkey.PurposeLogin, fixedNow); err != nil {
		t.Fatalf("valid challenge must survive: %v", err)
	}
}

func TestPasskeyRowsCascadeWithUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")
	handle, _ := passkey.NewUserHandle()
	if _, err := repo.UserHandle(ctx, alice, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, credential(alice, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE "user_ID" = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT (SELECT count(*) FROM auth_passkey) + (SELECT count(*) FROM auth_webauthn_user)`); n != 0 {
		t.Fatalf("rows must cascade, got %d", n)
	}
}

func TestPasskeyRecordUseRefusesToMoveTheSignCountBackwards(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")
	if _, err := repo.Create(ctx, credential(alice, 1)); err != nil { // 保存の認証回数は3
		t.Fatal(err)
	}
	id := []byte{1, 1}
	later := fixedNow.Add(time.Hour)

	for _, recv := range []uint32{0, 2, 3} {
		if err := repo.RecordUse(ctx, id, recv, true, later); !errors.Is(err, passkey.ErrNotFound) {
			t.Fatalf("3 → %d は更新しない(ErrNotFound): %v", recv, err)
		}
	}
	got, _ := repo.FindByCredentialID(ctx, id)
	if got.SignCount != 3 || got.BackupState || got.LastUsedAt != nil {
		t.Fatalf("拒否した更新で値が変わった: %+v", got)
	}

	if err := repo.RecordUse(ctx, id, 4, true, later); err != nil {
		t.Fatalf("3 → 4 は更新する: %v", err)
	}
	got, _ = repo.FindByCredentialID(ctx, id)
	if got.SignCount != 4 || !got.BackupState || got.LastUsedAt == nil || !got.LastUsedAt.Equal(later) {
		t.Fatalf("3 → 4 の後: %+v", got)
	}
	if err := repo.RecordUse(ctx, id, 4, false, later.Add(time.Hour)); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("4 → 4 は更新しない: %v", err)
	}
}

func TestPasskeyRecordUseAllowsAuthenticatorsThatAlwaysReportZero(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")
	synced := credential(alice, 2)
	synced.SignCount = 0 // 同期パスキーは、認証回数を0のまま返すことがある
	if _, err := repo.Create(ctx, synced); err != nil {
		t.Fatal(err)
	}
	id := synced.CredentialID

	for i, now := range []time.Time{fixedNow, fixedNow.Add(time.Hour)} {
		if err := repo.RecordUse(ctx, id, 0, true, now); err != nil {
			t.Fatalf("0 → 0 (%d回目)は更新する: %v", i+1, err)
		}
		got, _ := repo.FindByCredentialID(ctx, id)
		if got.SignCount != 0 || !got.BackupState || got.LastUsedAt == nil || !got.LastUsedAt.Equal(now) {
			t.Fatalf("0 → 0 の後: %+v", got)
		}
	}
	if err := repo.RecordUse(ctx, id, 1, true, fixedNow); err != nil {
		t.Fatalf("0 → 1 は更新する: %v", err)
	}
	if err := repo.RecordUse(ctx, id, 0, true, fixedNow); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("1 → 0 は更新しない: %v", err)
	}
	if err := repo.RecordUse(ctx, id, 4294967295, true, fixedNow); err != nil {
		t.Fatalf("uint32の最大値も保存できる: %v", err)
	}
	if got, _ := repo.FindByCredentialID(ctx, id); got.SignCount != 4294967295 {
		t.Fatalf("SignCount = %d", got.SignCount)
	}
}

func TestPasskeyRecordUseSucceedsForOnlyOneOfConcurrentLogins(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")
	if _, err := repo.Create(ctx, credential(alice, 1)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var ok, conflict atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := repo.RecordUse(ctx, []byte{1, 1}, 10, false, fixedNow); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, passkey.ErrNotFound):
				conflict.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 7 {
		t.Fatalf("同じ認証回数での同時更新は1つだけ成功する: ok=%d conflict=%d", ok.Load(), conflict.Load())
	}
}

func TestPasskeyRecordUseOnDeletedPasskeyIsNotFound(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewPasskey(pool)
	alice := newPasskeyUser(t, pool, "alice")
	created, err := repo.Create(ctx, credential(alice, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, alice, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordUse(ctx, []byte{1, 1}, 99, false, fixedNow); !errors.Is(err, passkey.ErrNotFound) {
		t.Fatalf("削除済みは ErrNotFound: %v", err)
	}
}
