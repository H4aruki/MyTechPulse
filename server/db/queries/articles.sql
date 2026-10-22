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

-- name: ListTaggedArticles :many
-- フィードの候補（Issue #66 段取り2）。指定したタグ（前後の空白を除いた小文字で比べる）のどれかが付いた、
-- 提供元の記事を新しい順に返す。tags には記事に付いた全タグの表示名をまとめる。
-- タグを先に tag 表で引き、article_tags は ("tag_ID", published_at) の索引で絞る。
WITH matched AS (
    SELECT DISTINCT at.article_id
    FROM tag t
    JOIN article_tags at ON at."tag_ID" = t."tag_ID"
    WHERE lower(btrim(t.tag_name)) = ANY(sqlc.arg(tags)::text[])
      AND at.published_at >= sqlc.arg(since)
)
SELECT a.id, a.source, a.title, a.url, a.author_name, a.likes, a.published_at,
       COALESCE((SELECT array_agg(t2.tag_name ORDER BY t2."tag_ID")
                 FROM article_tags at2 JOIN tag t2 ON t2."tag_ID" = at2."tag_ID"
                 WHERE at2.article_id = a.id), '{}')::text[] AS tags
FROM articles a
JOIN matched m ON m.article_id = a.id
WHERE a.source = sqlc.arg(source) AND a.published_at >= sqlc.arg(since)
ORDER BY a.published_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows);

-- name: ListPopularArticles :many
-- フィードの空き枠の候補。タグを問わず、提供元の記事をいいね数の多い順に返す。
SELECT a.id, a.source, a.title, a.url, a.author_name, a.likes, a.published_at,
       COALESCE((SELECT array_agg(t2.tag_name ORDER BY t2."tag_ID")
                 FROM article_tags at2 JOIN tag t2 ON t2."tag_ID" = at2."tag_ID"
                 WHERE at2.article_id = a.id), '{}')::text[] AS tags
FROM articles a
WHERE a.source = sqlc.arg(source) AND a.published_at >= sqlc.arg(since)
ORDER BY a.likes DESC, a.published_at DESC, a.id DESC
LIMIT sqlc.arg(max_rows);
