-- name: LockUserForSavedArticles :one
-- 保存の上限（500件）を、同時に保存しても超えないようにするための鍵。
-- 数えてから入れるまでの間に別の保存が割り込むと上限を超えるので、利用者の行を先にロックして、同じ利用者の保存を1つずつにする。
SELECT "user_ID" FROM "user" WHERE "user_ID" = $1 FOR UPDATE;

-- name: SavedArticleExists :one
SELECT EXISTS (SELECT 1 FROM saved_articles WHERE "user_ID" = $1 AND article_id = $2);

-- name: ArticleExists :one
SELECT EXISTS (SELECT 1 FROM articles WHERE id = $1);

-- name: CountSavedArticles :one
SELECT count(*) FROM saved_articles WHERE "user_ID" = $1;

-- name: InsertSavedArticle :execrows
-- 保存済みなら何もしない（created_at も変えない）。
INSERT INTO saved_articles ("user_ID", article_id)
VALUES ($1, $2)
ON CONFLICT ("user_ID", article_id) DO NOTHING;

-- name: ListArticleTagsByArticles :many
-- 記事のタグ。保存の学習（反映）に使う。
SELECT at.article_id, at."tag_ID", t.tag_name
FROM article_tags at JOIN tag t ON t."tag_ID" = at."tag_ID"
WHERE at.article_id = ANY(sqlc.arg(article_ids)::bigint[]) ORDER BY at.article_id, at."tag_ID";

-- name: HasDueSavedLearning :one
-- 期限（delay_seconds 秒）が来た未反映の保存があるか。ロックは取らない（先に軽く見るための問い合わせ）。
SELECT EXISTS (
    SELECT 1 FROM saved_articles
    WHERE "user_ID" = sqlc.arg(user_id) AND learned_at IS NULL
      AND created_at <= now() - make_interval(secs => sqlc.arg(delay_seconds)::float8)
);

-- name: MarkDueSavedLearned :many
-- 期限が来た未反映の保存に反映済みの印を付け、その記事を返す。利用者の行をロックした中で呼ぶ。
UPDATE saved_articles SET learned_at = now()
WHERE "user_ID" = sqlc.arg(user_id) AND learned_at IS NULL
  AND created_at <= now() - make_interval(secs => sqlc.arg(delay_seconds)::float8)
RETURNING article_id;

-- name: RecordSaveLearnings :many
-- 保存による学習を、1人1記事に1回だけにするための記録（#236 の決定）。新しく記録できた記事だけを返す。
-- すでに記録のある記事（過去に学習済み）は返らない。利用者の行をロックした中で呼ぶ。
INSERT INTO saved_article_learnings ("user_ID", article_id)
SELECT sqlc.arg(user_id), unnest(sqlc.arg(article_ids)::bigint[])
ON CONFLICT DO NOTHING
RETURNING article_id;

-- name: DeleteSavedArticle :exec
-- 保存されていなくてもエラーにしない。
DELETE FROM saved_articles WHERE "user_ID" = $1 AND article_id = $2;

-- name: ListSavedArticles :many
-- 自分の保存だけを、保存の新しい順に返す。
SELECT a.id, a.source, a.url, a.title, a.author_name, a.likes, a.published_at, s.created_at AS saved_at
FROM saved_articles s
JOIN articles a ON a.id = s.article_id
WHERE s."user_ID" = sqlc.arg(user_id)
ORDER BY s.created_at DESC, s.article_id DESC
LIMIT sqlc.arg(max_rows) OFFSET sqlc.arg(skip_rows);
