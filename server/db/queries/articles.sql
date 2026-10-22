-- name: UpsertArticle :one
-- 同じ (source, external_id) は上書きする。fetched_at（最初に取得した日時）は変えない。
-- 同じURLが別のIDで来たときは一意制約違反になる。呼び出し側がその記事だけを飛ばす。
INSERT INTO articles (source, external_id, url, title, author_name, likes, published_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (source, external_id) DO UPDATE
SET title = EXCLUDED.title,
    author_name = EXCLUDED.author_name,
    likes = EXCLUDED.likes,
    published_at = EXCLUDED.published_at,
    last_seen_at = now()
RETURNING id;

-- name: AddArticleTag :exec
INSERT INTO article_tags (article_id, "tag_ID", published_at)
VALUES ($1, $2, $3)
ON CONFLICT (article_id, "tag_ID") DO UPDATE SET published_at = EXCLUDED.published_at;

-- name: ListTagsForMatching :many
SELECT "tag_ID", tag_name FROM tag;

-- name: ListEnabledFetchTags :many
SELECT f."tag_ID", t.tag_name, f.zenn_topic
FROM fetch_tags f
JOIN tag t USING ("tag_ID")
WHERE f.enabled
ORDER BY f."tag_ID"
LIMIT 500;

-- name: DeleteExpiredArticles :execrows
-- 保持期間を過ぎた記事を消す。article_tags は連動して消える。
DELETE FROM articles WHERE source = $1 AND published_at < $2;

-- name: InsertFetchRun :one
INSERT INTO fetch_runs (slot_at, trigger, attempt, started_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: FinishFetchRun :exec
UPDATE fetch_runs
SET finished_at = $2,
    status = $3,
    error_message = $4,
    qiita_requests = $5,
    zenn_requests = $6,
    upserted = $7,
    deleted = $8
WHERE id = $1;

-- name: ListFetchRunsForSlot :many
SELECT id, attempt, status, started_at, finished_at
FROM fetch_runs
WHERE slot_at = $1
ORDER BY started_at DESC, id DESC;

-- name: AbandonStaleFetchRuns :exec
-- 'running' のまま残った行（実行中に止まったもの）を失敗にする。アドバイザリロックを持っているときだけ呼ぶ。
UPDATE fetch_runs
SET status = 'failed', finished_at = $1, error_message = '実行が中断されました'
WHERE status = 'running';
