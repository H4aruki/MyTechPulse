package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

// ArticleFetchLockKey は、記事の定期取得が同時に1つだけ動くようにするアドバイザリロックのキー。
const ArticleFetchLockKey int64 = 6_600_001

// pgForeignKeyViolation は外部キー違反のSQLSTATE。一意制約違反は pgUniqueViolation(auth.go)。
const pgForeignKeyViolation = "23503"

// ArticleFetch は記事の定期一括取得のDB操作。
type ArticleFetch struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

var (
	_ articlefetch.JobStore = (*ArticleFetch)(nil)
	_ articlefetch.RunStore = (*ArticleFetch)(nil)
	_ articlefetch.Locker   = (*ArticleFetch)(nil)
)

func NewArticleFetch(pool *pgxpool.Pool) *ArticleFetch {
	return &ArticleFetch{pool: pool, q: dbgen.New(pool)}
}

func tstz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func (s *ArticleFetch) ListTags(ctx context.Context) ([]articlefetch.TagRef, error) {
	rows, err := s.q.ListTagsForMatching(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]articlefetch.TagRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, articlefetch.TagRef{ID: r.TagID, Name: r.TagName})
	}
	return out, nil
}

func (s *ArticleFetch) ListFetchTags(ctx context.Context) ([]articlefetch.FetchTag, error) {
	rows, err := s.q.ListEnabledFetchTags(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]articlefetch.FetchTag, 0, len(rows))
	for _, r := range rows {
		out = append(out, articlefetch.FetchTag{ID: r.TagID, Name: r.TagName, ZennTopic: r.ZennTopic})
	}
	return out, nil
}

// SaveArticles は記事をまとめて1つのトランザクションで保存する。
// 同じURLが別の記事IDで既にあるなど、一意制約に反した記事だけを飛ばして続ける（その記事の分だけ巻き戻す）。
// 結ぶタグは、呼び出し側が渡した tag_ID だけ（tag 表の行は作らない）。
func (s *ArticleFetch) SaveArticles(ctx context.Context, records []articlefetch.Record) (articlefetch.SaveResult, error) {
	var res articlefetch.SaveResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, rec := range records {
		sp, err := tx.Begin(ctx) // セーブポイント。この記事だけを巻き戻せるようにする
		if err != nil {
			return res, err
		}
		err = s.saveOne(ctx, s.q.WithTx(sp), rec)
		if err != nil {
			_ = sp.Rollback(ctx)
			if isSkippable(err) {
				res.Skipped++
				continue
			}
			return res, err
		}
		if err := sp.Commit(ctx); err != nil {
			return res, err
		}
		res.Saved++
	}
	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	return res, nil
}

func (s *ArticleFetch) saveOne(ctx context.Context, q *dbgen.Queries, rec articlefetch.Record) error {
	a := rec.Article
	id, err := q.UpsertArticle(ctx, dbgen.UpsertArticleParams{
		Source:      strings.ToLower(a.Source),
		ExternalID:  a.ExternalID,
		Url:         a.URL,
		Title:       a.Title,
		AuthorName:  a.Author,
		Likes:       int32(a.Likes),
		PublishedAt: tstz(a.PublishedAt),
	})
	if err != nil {
		return err
	}
	for _, tagID := range rec.TagIDs {
		if err := q.AddArticleTag(ctx, dbgen.AddArticleTagParams{ArticleID: id, TagID: tagID, PublishedAt: tstz(a.PublishedAt)}); err != nil {
			return err
		}
	}
	return nil
}

// isSkippable は、その記事だけを飛ばしてよい失敗か。
// 同じURLが別の記事IDで既にある（一意制約違反）、取得の途中でタグが消された（外部キー違反）。
func isSkippable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == pgUniqueViolation || pgErr.Code == pgForeignKeyViolation)
}

func (s *ArticleFetch) DeleteExpired(ctx context.Context, source string, before time.Time) (int64, error) {
	return s.q.DeleteExpiredArticles(ctx, dbgen.DeleteExpiredArticlesParams{Source: source, PublishedAt: tstz(before)})
}

func (s *ArticleFetch) StartRun(ctx context.Context, slot time.Time, trigger string, attempt int, startedAt time.Time) (int64, error) {
	return s.q.InsertFetchRun(ctx, dbgen.InsertFetchRunParams{SlotAt: tstz(slot), Trigger: trigger, Attempt: int16(attempt), StartedAt: tstz(startedAt)})
}

func (s *ArticleFetch) FinishRun(ctx context.Context, id int64, finishedAt time.Time, res articlefetch.Result) error {
	return s.q.FinishFetchRun(ctx, dbgen.FinishFetchRunParams{
		ID:            id,
		FinishedAt:    tstz(finishedAt),
		Status:        res.Status,
		ErrorMessage:  res.Message,
		QiitaRequests: int32(res.QiitaRequests),
		ZennRequests:  int32(res.ZennRequests),
		Upserted:      int32(res.Upserted),
		Deleted:       int32(res.Deleted),
	})
}

func (s *ArticleFetch) RunsForSlot(ctx context.Context, slot time.Time) ([]articlefetch.RunRow, error) {
	rows, err := s.q.ListFetchRunsForSlot(ctx, tstz(slot))
	if err != nil {
		return nil, err
	}
	out := make([]articlefetch.RunRow, 0, len(rows))
	for _, r := range rows {
		row := articlefetch.RunRow{ID: r.ID, Attempt: int(r.Attempt), Status: r.Status}
		if r.FinishedAt.Valid {
			row.FinishedAt = r.FinishedAt.Time
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *ArticleFetch) AbandonStaleRuns(ctx context.Context, finishedAt time.Time) error {
	return s.q.AbandonStaleFetchRuns(ctx, tstz(finishedAt))
}

// TryLock は、プールとは別の専用の接続でアドバイザリロックを試す。
// 取得は数分かかるため、小さい接続プールの接続を占有しない。接続が切れればDBがロックを自動で外す。
func (s *ArticleFetch) TryLock(ctx context.Context) (func(), bool, error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
	if err != nil {
		return nil, false, errors.New("store: ロック用の接続を作れません")
	}
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, ArticleFetchLockKey).Scan(&got); err != nil || !got {
		_ = conn.Close(context.WithoutCancel(ctx))
		if err != nil {
			return nil, false, errors.New("store: ロックを試せません")
		}
		return nil, false, nil
	}
	unlock := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, ArticleFetchLockKey)
		_ = conn.Close(cleanup)
	}
	return unlock, true, nil
}
