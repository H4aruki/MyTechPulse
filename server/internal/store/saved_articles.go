package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

// SavedArticlesLimit は、1人の利用者が保存できる記事の上限。
const SavedArticlesLimit = 500

var (
	// ErrArticleNotFound は、保存しようとした記事が存在しないとき。
	ErrArticleNotFound = errors.New("store: 記事が見つかりません")
	// ErrSavedLimitReached は、未保存の記事を保存しようとして、上限（SavedArticlesLimit）に達しているとき。
	ErrSavedLimitReached = errors.New("store: 保存できる記事の上限です")
	// ErrSavedUserNotFound は、保存しようとした利用者が存在しないとき。
	ErrSavedUserNotFound = errors.New("store: 利用者が見つかりません")
)

// SavedArticle は、利用者が保存した記事1件。
type SavedArticle struct {
	ID          int64
	Source      string
	URL         string
	Title       string
	AuthorName  string
	Likes       int
	PublishedAt time.Time
	SavedAt     time.Time
}

// SavedArticles は保存した記事のDB操作。
type SavedArticles struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

func NewSavedArticles(pool *pgxpool.Pool) *SavedArticles {
	return &SavedArticles{pool: pool, q: dbgen.New(pool)}
}

func savedUserID32(id int64) (int32, bool) {
	if id < 1 || id > math.MaxInt32 {
		return 0, false
	}
	return int32(id), true
}

// Save は記事を保存する。新しく保存したときだけ created を true にする。
// 保存済みなら何もせず（保存日時も変えず）、上限でも成功する。
// 未保存の記事は、すでに SavedArticlesLimit 件なら ErrSavedLimitReached、記事が無ければ ErrArticleNotFound。
// 利用者の行を鍵にして数え直すため、同時に保存しても上限を超えない。
func (s *SavedArticles) Save(ctx context.Context, userID, articleID int64) (created bool, err error) {
	uid, ok := savedUserID32(userID)
	if !ok {
		return false, ErrSavedUserNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, errors.New("store: トランザクションを開始できません")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockUserForSavedArticles(ctx, uid); errors.Is(err, pgx.ErrNoRows) {
		return false, ErrSavedUserNotFound
	} else if err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	saved, err := q.SavedArticleExists(ctx, dbgen.SavedArticleExistsParams{UserID: uid, ArticleID: articleID})
	if err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	if saved {
		return false, nil
	}
	exists, err := q.ArticleExists(ctx, articleID)
	if err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	if !exists {
		return false, ErrArticleNotFound
	}
	n, err := q.CountSavedArticles(ctx, uid)
	if err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	if n >= SavedArticlesLimit {
		return false, ErrSavedLimitReached
	}
	rows, err := q.InsertSavedArticle(ctx, dbgen.InsertSavedArticleParams{UserID: uid, ArticleID: articleID})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		return false, ErrArticleNotFound // 確かめた後に、保持期間の削除で記事が消えた
	}
	if err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	if err := tx.Commit(ctx); err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	return rows == 1, nil
}

// Unsave は保存を解除する（行を消す）。保存されていなくてもエラーにしない。
func (s *SavedArticles) Unsave(ctx context.Context, userID, articleID int64) error {
	uid, ok := savedUserID32(userID)
	if !ok {
		return nil
	}
	if err := s.q.DeleteSavedArticle(ctx, dbgen.DeleteSavedArticleParams{UserID: uid, ArticleID: articleID}); err != nil {
		return errors.New("store: 保存を解除できません")
	}
	return nil
}

// List は、利用者が保存した記事を保存の新しい順に返す。total は、ページングする前の保存の総件数。
func (s *SavedArticles) List(ctx context.Context, userID int64, limit, offset int) (items []SavedArticle, total int, err error) {
	uid, ok := savedUserID32(userID)
	if !ok {
		return []SavedArticle{}, 0, nil
	}
	limit = min(max(limit, 0), math.MaxInt32)
	offset = min(max(offset, 0), math.MaxInt32)
	n, err := s.q.CountSavedArticles(ctx, uid)
	if err != nil {
		return nil, 0, errors.New("store: 保存した記事を取得できません")
	}
	rows, err := s.q.ListSavedArticles(ctx, dbgen.ListSavedArticlesParams{UserID: uid, MaxRows: int32(limit), SkipRows: int32(offset)})
	if err != nil {
		return nil, 0, errors.New("store: 保存した記事を取得できません")
	}
	items = make([]SavedArticle, 0, len(rows))
	for _, r := range rows {
		items = append(items, SavedArticle{
			ID: r.ID, Source: r.Source, URL: r.Url, Title: r.Title, AuthorName: r.AuthorName,
			Likes: int(r.Likes), PublishedAt: r.PublishedAt.Time, SavedAt: r.SavedAt.Time,
		})
	}
	return items, int(n), nil
}
