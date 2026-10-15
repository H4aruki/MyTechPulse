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
INSERT INTO recommend ("user_ID", "tag_ID", match_int) VALUES ($1, $2, $3);

-- 登録時に選ばなかったタグ(tagテーブルにある全タグのうち、選んだタグと正規化後の名前が同じものを除く)に、
-- 同じ重みを一括で入れる。選んだタグの保存が終わったあとで呼ぶ。
-- name: CreateUnselectedRecommendations :exec
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT DISTINCT ON (lower(btrim(t.tag_name))) sqlc.arg(user_id)::int, t."tag_ID", sqlc.arg(match_int)::int
FROM tag t
WHERE NOT EXISTS (
  SELECT 1 FROM recommend r JOIN tag st ON st."tag_ID" = r."tag_ID"
  WHERE r."user_ID" = sqlc.arg(user_id)::int
    AND lower(btrim(st.tag_name)) = lower(btrim(t.tag_name))
)
ORDER BY lower(btrim(t.tag_name)), t."tag_ID";
