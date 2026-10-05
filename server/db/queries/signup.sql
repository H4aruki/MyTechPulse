-- 表記違い(大文字小文字・前後空白)のタグをGo書き込み同士で重複作成しないよう、
-- 正規化したタグ名ごとにトランザクション内の排他ロックを取ってから再検索する。

-- name: LockNormalizedTag :exec
SELECT pg_advisory_xact_lock(hashtext('tag:' || lower(btrim($1::text))));

-- name: FindTagByNormalizedName :one
SELECT "tag_ID", tag_name FROM tag
WHERE lower(btrim(tag_name)) = lower(btrim($1::text)) ORDER BY "tag_ID" LIMIT 1;

-- name: CreateTag :one
INSERT INTO tag (tag_name) VALUES ($1) RETURNING "tag_ID", tag_name;

-- name: CreateInitialRecommendation :exec
INSERT INTO recommend ("user_ID", "tag_ID", match_int) VALUES ($1, $2, 1);
