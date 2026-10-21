-- 会員登録とクリックは tag 表に行を作らない(#239、#262)。タグは正規化した名前(前後空白・大文字小文字を無視)で探すだけ。

-- name: FindTagByNormalizedName :one
SELECT "tag_ID", tag_name FROM tag
WHERE lower(btrim(tag_name)) = lower(btrim($1::text)) ORDER BY "tag_ID" LIMIT 1;

-- name: CreateInitialRecommendation :exec
INSERT INTO recommend ("user_ID", "tag_ID", match_int) VALUES ($1, $2, $3);
