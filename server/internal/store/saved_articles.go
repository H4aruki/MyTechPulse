package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/interest"
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
	// 新しい保存だけ学習する。再PUT（保存済み）は学習しない（連打で点を上げられないように）。
	// 学習は保存と同じトランザクションで行い、失敗したら保存ごと取り消す。
	if rows == 1 {
		if err := learnFromSave(ctx, q, uid, articleID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, errors.New("store: 記事を保存できません")
	}
	return rows == 1, nil
}

// learnFromSave は、保存した記事の各タグの興味度に interest.SaveBoost を足す。
// 他のタグは減衰しない。tag 表に新しい行は作らず、記事に付いているタグだけを使う。
func learnFromSave(ctx context.Context, q *dbgen.Queries, uid int32, articleID int64) error {
	fail := errors.New("store: 興味度を更新できません")
	tags, err := q.ListArticleTags(ctx, articleID)
	if err != nil {
		return fail
	}
	if len(tags) == 0 {
		return nil
	}
	rows, err := q.ListRecommendations(ctx, uid)
	if err != nil {
		return fail
	}
	current := make([]interest.Weight, 0, len(rows))
	for _, row := range rows {
		current = append(current, interest.Weight{TagID: int64(row.TagID), Tag: row.TagName, Value: int64(row.MatchInt)})
	}
	names := make([]string, 0, len(tags))
	tagIDs := make(map[string]int32, len(tags))
	for _, tag := range tags {
		names = append(names, tag.TagName)
		tagIDs[interest.NormalizeTag(tag.TagName)] = tag.TagID
	}
	updated, err := interest.UpdateOnSave(current, names)
	if err != nil {
		return err
	}
	for i, weight := range updated {
		if i < len(current) {
			if weight.Value == current[i].Value {
				continue
			}
		} else {
			// まだ行の無かったタグ。名前から、記事のタグのIDを引く。
			id, ok := tagIDs[weight.Tag]
			if !ok {
				return fail
			}
			weight.TagID = int64(id)
		}
		if weight.Value > math.MaxInt32 || weight.Value < math.MinInt32 || weight.TagID > math.MaxInt32 || weight.TagID < 1 {
			return fail
		}
		if err := q.UpsertRecommendation(ctx, dbgen.UpsertRecommendationParams{UserID: uid, TagID: int32(weight.TagID), MatchInt: int32(weight.Value)}); err != nil {
			return fail
		}
	}
	return nil
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
