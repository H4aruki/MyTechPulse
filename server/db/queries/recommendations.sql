-- name: LockUserForRecommendation :one
SELECT "user_ID" FROM "user" WHERE "user_ID" = $1 FOR UPDATE;

-- name: ListRecommendations :many
SELECT r."tag_ID", t.tag_name, r.match_int
FROM recommend r JOIN tag t ON t."tag_ID" = r."tag_ID"
WHERE r."user_ID" = $1 ORDER BY r."tag_ID";

-- name: UpsertRecommendation :exec
INSERT INTO recommend ("user_ID", "tag_ID", match_int) VALUES ($1, $2, $3)
ON CONFLICT ("user_ID", "tag_ID") DO UPDATE SET match_int = EXCLUDED.match_int;
