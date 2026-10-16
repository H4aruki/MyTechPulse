package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

// Passkey は passkey.Repository のPostgreSQL実装。
type Passkey struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

var _ passkey.Repository = (*Passkey)(nil)

// NewPasskey は接続プールから Passkey を作る。
func NewPasskey(pool *pgxpool.Pool) *Passkey {
	return &Passkey{pool: pool, q: dbgen.New(pool)}
}

func passkeyUserID32(id int64) (int32, error) {
	if id < 1 || id > math.MaxInt32 {
		return 0, passkey.ErrNotFound
	}
	return int32(id), nil
}

func toCredential(r dbgen.AuthPasskey) passkey.Credential {
	c := passkey.Credential{
		ID:             r.ID,
		UserID:         int64(r.UserID),
		CredentialID:   r.CredentialID,
		PublicKey:      r.PublicKey,
		SignCount:      uint32(min(max(r.SignCount, 0), math.MaxUint32)),
		Transports:     r.Transports,
		AAGUID:         r.Aaguid,
		BackupEligible: r.BackupEligible,
		BackupState:    r.BackupState,
		Name:           r.Name,
		CreatedAt:      r.CreatedAt.Time,
	}
	if r.LastUsedAt.Valid {
		t := r.LastUsedAt.Time
		c.LastUsedAt = &t
	}
	return c
}

// UserHandle は利用者のハンドルを返す。無ければ newHandle を保存して返す。
func (p *Passkey) UserHandle(ctx context.Context, userID int64, newHandle []byte) ([]byte, error) {
	id, err := passkeyUserID32(userID)
	if err != nil {
		return nil, err
	}
	handle, err := p.q.UpsertWebAuthnUser(ctx, dbgen.UpsertWebAuthnUserParams{UserID: id, UserHandle: newHandle})
	if err != nil {
		return nil, errors.New("store: 利用者ハンドルを取得できません")
	}
	return handle, nil
}

// UserIDByHandle はハンドルから利用者IDを返す。無ければ passkey.ErrNotFound。
func (p *Passkey) UserIDByHandle(ctx context.Context, handle []byte) (int64, error) {
	id, err := p.q.FindUserIDByWebAuthnHandle(ctx, handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, passkey.ErrNotFound
	}
	if err != nil {
		return 0, errors.New("store: 利用者を取得できません")
	}
	return int64(id), nil
}

// Create は上限を確かめて登録する。利用者の行を鍵にして数え直すため、同時に登録しても上限を超えない。
func (p *Passkey) Create(ctx context.Context, c passkey.Credential) (passkey.Credential, error) {
	id, err := passkeyUserID32(c.UserID)
	if err != nil {
		return passkey.Credential{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return passkey.Credential{}, errors.New("store: トランザクションを開始できません")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.q.WithTx(tx)

	if _, err := q.LockUserForPasskey(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return passkey.Credential{}, passkey.ErrNotFound
	} else if err != nil {
		return passkey.Credential{}, errors.New("store: パスキーを登録できません")
	}
	n, err := q.CountPasskeysByUser(ctx, id)
	if err != nil {
		return passkey.Credential{}, errors.New("store: パスキーを登録できません")
	}
	if n >= passkey.MaxPasskeysPerUser {
		return passkey.Credential{}, passkey.ErrLimitReached
	}
	transports := c.Transports
	if transports == nil {
		transports = []string{}
	}
	row, err := q.CreatePasskey(ctx, dbgen.CreatePasskeyParams{
		UserID: id, CredentialID: c.CredentialID, PublicKey: c.PublicKey, SignCount: int64(c.SignCount),
		Transports: transports, Aaguid: c.AAGUID, BackupEligible: c.BackupEligible, BackupState: c.BackupState, Name: c.Name,
	})
	if isUniqueViolation(err) {
		return passkey.Credential{}, passkey.ErrCredentialExists
	}
	if err != nil {
		return passkey.Credential{}, errors.New("store: パスキーを登録できません")
	}
	if err := tx.Commit(ctx); err != nil {
		return passkey.Credential{}, errors.New("store: パスキーを登録できません")
	}
	return toCredential(row), nil
}

// ListByUser は利用者のパスキーを登録順に返す。
func (p *Passkey) ListByUser(ctx context.Context, userID int64) ([]passkey.Credential, error) {
	id, err := passkeyUserID32(userID)
	if err != nil {
		return nil, err
	}
	rows, err := p.q.ListPasskeysByUser(ctx, id)
	if err != nil {
		return nil, errors.New("store: パスキーを取得できません")
	}
	out := make([]passkey.Credential, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCredential(r))
	}
	return out, nil
}

// FindByCredentialID は認証情報IDでパスキーを探す。無ければ passkey.ErrNotFound。
func (p *Passkey) FindByCredentialID(ctx context.Context, credentialID []byte) (passkey.Credential, error) {
	row, err := p.q.FindPasskeyByCredentialID(ctx, credentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return passkey.Credential{}, passkey.ErrNotFound
	}
	if err != nil {
		return passkey.Credential{}, errors.New("store: パスキーを取得できません")
	}
	return toCredential(row), nil
}

// RecordUse はログイン成功後の認証回数・同期の状態・最後に使った日時を更新する。
// 認証回数が増えない更新(巻き戻り、同時ログインの競合)と、削除済みのパスキーは 0 行になり、passkey.ErrNotFound を返す。
func (p *Passkey) RecordUse(ctx context.Context, credentialID []byte, signCount uint32, backupState bool, now time.Time) error {
	n, err := p.q.UpdatePasskeyUsage(ctx, dbgen.UpdatePasskeyUsageParams{
		CredentialID: credentialID, SignCount: int64(signCount), BackupState: backupState, LastUsedAt: timestamptz(now),
	})
	if err != nil {
		return errors.New("store: パスキーを更新できません")
	}
	if n == 0 {
		return passkey.ErrNotFound
	}
	return nil
}

// Rename は本人のパスキーの名前を変える。
func (p *Passkey) Rename(ctx context.Context, userID, id int64, name string) error {
	uid, err := passkeyUserID32(userID)
	if err != nil {
		return err
	}
	n, err := p.q.RenamePasskey(ctx, dbgen.RenamePasskeyParams{ID: id, UserID: uid, Name: name})
	if err != nil {
		return errors.New("store: パスキーを更新できません")
	}
	if n == 0 {
		return passkey.ErrNotFound
	}
	return nil
}

// Delete は本人のパスキーを物理削除する。
func (p *Passkey) Delete(ctx context.Context, userID, id int64) error {
	uid, err := passkeyUserID32(userID)
	if err != nil {
		return err
	}
	n, err := p.q.DeletePasskey(ctx, dbgen.DeletePasskeyParams{ID: id, UserID: uid})
	if err != nil {
		return errors.New("store: パスキーを削除できません")
	}
	if n == 0 {
		return passkey.ErrNotFound
	}
	return nil
}

// PasskeyChallenge は passkey.ChallengeRepository のPostgreSQL実装。
type PasskeyChallenge struct {
	q *dbgen.Queries
}

var _ passkey.ChallengeRepository = (*PasskeyChallenge)(nil)

// NewPasskeyChallenge は接続プールから PasskeyChallenge を作る。
func NewPasskeyChallenge(pool *pgxpool.Pool) *PasskeyChallenge {
	return &PasskeyChallenge{q: dbgen.New(pool)}
}

// Create はチャレンジを保存する。登録は利用者に結び付け、ログインは結び付けない。
func (c *PasskeyChallenge) Create(ctx context.Context, id [32]byte, purpose passkey.Purpose, userID *int64, sessionData []byte, expiresAt time.Time) error {
	uid := pgtype.Int4{}
	if userID != nil {
		v, err := passkeyUserID32(*userID)
		if err != nil {
			return err
		}
		uid = pgtype.Int4{Int32: v, Valid: true}
	}
	if err := c.q.CreateWebAuthnChallenge(ctx, dbgen.CreateWebAuthnChallengeParams{
		ID: id[:], Purpose: string(purpose), UserID: uid, SessionData: sessionData, ExpiresAt: timestamptz(expiresAt),
	}); err != nil {
		return errors.New("store: チャレンジを保存できません")
	}
	return nil
}

// Consume は未使用かつ期限内で用途が合うチャレンジを、1回だけ使用済みにして返す。
func (c *PasskeyChallenge) Consume(ctx context.Context, id [32]byte, purpose passkey.Purpose, now time.Time) (passkey.Challenge, error) {
	row, err := c.q.ConsumeWebAuthnChallenge(ctx, dbgen.ConsumeWebAuthnChallengeParams{
		ID: id[:], Purpose: string(purpose), UsedAt: timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return passkey.Challenge{}, passkey.ErrChallengeInvalid
	}
	if err != nil {
		return passkey.Challenge{}, errors.New("store: チャレンジを取得できません")
	}
	out := passkey.Challenge{SessionData: row.SessionData}
	if row.UserID.Valid {
		v := int64(row.UserID.Int32)
		out.UserID = &v
	}
	return out, nil
}

// DeleteExpired は期限切れと使用済みのチャレンジを消す。
func (c *PasskeyChallenge) DeleteExpired(ctx context.Context, now time.Time) error {
	if err := c.q.DeleteExpiredWebAuthnChallenges(ctx, timestamptz(now)); err != nil {
		return errors.New("store: 期限切れのチャレンジを削除できません")
	}
	return nil
}
