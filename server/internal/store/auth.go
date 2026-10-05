// Package store はsqlc生成物をラップし、各機能のインターフェースをPostgreSQLで満たす。
// 生のSQLエラーや入力値は上位へ返さず、機能側のエラーへ変換する。
package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

// pgUniqueViolation はPostgreSQLの一意制約違反コード。
const pgUniqueViolation = "23505"

// Auth は auth.UserRepository と auth.SessionRepository のPostgreSQL実装。
type Auth struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

var (
	_ auth.UserRepository    = (*Auth)(nil)
	_ auth.SessionRepository = (*Auth)(nil)
)

// NewAuth は接続プールから Auth を作る。
func NewAuth(pool *pgxpool.Pool) *Auth {
	return &Auth{pool: pool, q: dbgen.New(pool)}
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func toUser(id int32, username, role string) auth.User {
	return auth.User{ID: int64(id), Username: username, Role: auth.Role(role)}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func userID32(id int64) (int32, error) {
	if id < 1 || id > math.MaxInt32 {
		return 0, auth.ErrNotFound
	}
	return int32(id), nil
}

// FindByUsername は利用者名で利用者を探す。無ければ auth.ErrNotFound。
func (a *Auth) FindByUsername(ctx context.Context, username string) (auth.UserWithPassword, error) {
	row, err := a.q.FindUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserWithPassword{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.UserWithPassword{}, errors.New("store: 利用者を取得できません")
	}
	return auth.UserWithPassword{
		User:         toUser(row.UserID, row.UserName, row.Role),
		PasswordHash: row.Password,
	}, nil
}

// CreateWithInterestsAndSession は利用者・初期興味度・セッションを1つのトランザクションで作る。
// どこかで失敗した場合は何も残さない。同名の利用者は auth.ErrUsernameTaken。
func (a *Auth) CreateWithInterestsAndSession(ctx context.Context, username, passwordHash string, role auth.Role,
	favoriteTags []string, tokenHash [32]byte, expiresAt time.Time) (auth.User, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return auth.User{}, errors.New("store: トランザクションを開始できません")
	}
	// コミット済みなら何もしない。それ以外は全て巻き戻す。
	defer func() { _ = tx.Rollback(ctx) }()
	q := a.q.WithTx(tx)

	created, err := q.CreateUser(ctx, dbgen.CreateUserParams{UserName: username, Password: passwordHash, Role: string(role)})
	if isUniqueViolation(err) {
		return auth.User{}, auth.ErrUsernameTaken
	}
	if err != nil {
		return auth.User{}, errors.New("store: 利用者を作成できません")
	}

	linked := map[int32]bool{}
	for _, name := range favoriteTags {
		name = strings.TrimSpace(name)
		if err := q.LockNormalizedTag(ctx, name); err != nil {
			return auth.User{}, errors.New("store: タグを確定できません")
		}
		tag, err := q.FindTagByNormalizedName(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			tag, err = q.CreateTag(ctx, name)
		}
		if err != nil {
			return auth.User{}, errors.New("store: タグを確定できません")
		}
		if linked[tag.TagID] {
			continue
		}
		linked[tag.TagID] = true
		if err := q.CreateInitialRecommendation(ctx, dbgen.CreateInitialRecommendationParams{UserID: created.UserID, TagID: tag.TagID}); err != nil {
			return auth.User{}, errors.New("store: 初期の興味度を作成できません")
		}
	}

	if err := q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: tokenHash[:], UserID: created.UserID, ExpiresAt: timestamptz(expiresAt),
	}); err != nil {
		return auth.User{}, errors.New("store: セッションを作成できません")
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.User{}, errors.New("store: 登録を確定できません")
	}
	return toUser(created.UserID, created.UserName, created.Role), nil
}

// Create はセッションを保存する。
func (a *Auth) Create(ctx context.Context, tokenHash [32]byte, userID int64, expiresAt time.Time) error {
	id, err := userID32(userID)
	if err != nil {
		return err
	}
	if err := a.q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: tokenHash[:], UserID: id, ExpiresAt: timestamptz(expiresAt),
	}); err != nil {
		return errors.New("store: セッションを作成できません")
	}
	return nil
}

// FindUser は now より前に期限が切れていない場合だけセッションの利用者を返す。無ければ auth.ErrNotFound。
func (a *Auth) FindUser(ctx context.Context, tokenHash [32]byte, now time.Time) (auth.Session, error) {
	row, err := a.q.FindSessionUser(ctx, dbgen.FindSessionUserParams{TokenHash: tokenHash[:], ExpiresAt: timestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.Session{}, errors.New("store: セッションを取得できません")
	}
	return auth.Session{User: toUser(row.UserID, row.UserName, row.Role), ExpiresAt: row.ExpiresAt.Time}, nil
}

// Delete はセッションを削除する。対象が無くてもエラーにしない。
func (a *Auth) Delete(ctx context.Context, tokenHash [32]byte) error {
	if err := a.q.DeleteSession(ctx, tokenHash[:]); err != nil {
		return errors.New("store: セッションを削除できません")
	}
	return nil
}

// DeleteExpired は now までに期限が切れたセッションを削除する。
func (a *Auth) DeleteExpired(ctx context.Context, now time.Time) error {
	if err := a.q.DeleteExpiredSessions(ctx, timestamptz(now)); err != nil {
		return errors.New("store: 期限切れセッションを削除できません")
	}
	return nil
}
