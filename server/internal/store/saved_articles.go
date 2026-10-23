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

// Save は記事を保存する（保存するだけで、興味度は変えない。学習は SaveLearnDelay 後に applyDueSaveLearning が行う）。
// 新しく保存したときだけ created を true にする。
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

// SaveLearnDelay は、保存してから興味度へ反映するまでの時間。この間に解除すれば、学習に影響しない（#236 決めごと5）。
const SaveLearnDelay = 10 * time.Second

// applyDueSaveLearning は、保存から SaveLearnDelay を過ぎた未反映の保存を、まとめて興味度へ反映する。
// 各記事のタグへ interest.SaveBoost を足す（他のタグは減衰しない）。tag 表に新しい行は作らず、記事に付いているタグだけを使う。
// 呼び出し側は、利用者の行をロック（LockUserForRecommendation）したトランザクションの中で呼ぶこと。
// 同じ記事が二重に入らないのは、反映済みの印（learned_at）を、興味度の更新と同じトランザクションで付けるため。
// 解除された保存は行が無いので反映されない。読み込みと書き込みは、何件あっても1回ずつ。
func applyDueSaveLearning(ctx context.Context, q *dbgen.Queries, uid int32) error {
	fail := errors.New("store: 興味度を更新できません")
	articleIDs, err := q.MarkDueSavedLearned(ctx, dbgen.MarkDueSavedLearnedParams{UserID: uid, DelaySeconds: SaveLearnDelay.Seconds()})
	if err != nil {
		return fail
	}
	if len(articleIDs) == 0 {
		return nil
	}
	tagRows, err := q.ListArticleTagsByArticles(ctx, articleIDs)
	if err != nil {
		return fail
	}
	rows, err := q.ListRecommendations(ctx, uid)
	if err != nil {
		return fail
	}
	original := make(map[int64]int64, len(rows))
	current := make([]interest.Weight, 0, len(rows))
	for _, row := range rows {
		current = append(current, interest.Weight{TagID: int64(row.TagID), Tag: row.TagName, Value: int64(row.MatchInt)})
		original[int64(row.TagID)] = int64(row.MatchInt)
	}
	tagIDs := make(map[string]int64, len(tagRows))
	namesByArticle := make(map[int64][]string, len(articleIDs))
	for _, tag := range tagRows {
		namesByArticle[tag.ArticleID] = append(namesByArticle[tag.ArticleID], tag.TagName)
		tagIDs[interest.NormalizeTag(tag.TagName)] = int64(tag.TagID)
	}
	for _, id := range articleIDs {
		names := namesByArticle[id]
		if len(names) == 0 {
			continue
		}
		before := len(current)
		updated, err := interest.UpdateOnSave(current, names)
		if err != nil {
			return err
		}
		// まだ行の無かったタグは、名前から記事のタグのIDを引く。次の記事の計算でも同じタグとして扱うため、ここで確定する。
		for i := before; i < len(updated); i++ {
			tagID, ok := tagIDs[updated[i].Tag]
			if !ok {
				return fail
			}
			updated[i].TagID = tagID
		}
		current = updated
	}
	for _, weight := range current {
		if was, ok := original[weight.TagID]; ok && was == weight.Value {
			continue
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
