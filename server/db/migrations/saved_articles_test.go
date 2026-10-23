package migrations_test

import "testing"

func TestSavedArticlesTableAndConstraints(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, `INSERT INTO "user" (user_name, password) VALUES ('saver', 'h')`)
	mustExec(t, db, insertArticle, "qiita", "s1", "https://qiita.com/u/items/s1")
	const save = `INSERT INTO saved_articles ("user_ID", article_id)
		SELECT u."user_ID", a.id FROM "user" u, articles a WHERE u.user_name = 'saver' AND a.external_id = 's1'`

	mustExec(t, db, save)
	if n := count(t, db, `SELECT count(*) FROM saved_articles WHERE created_at <= now() AND created_at > now() - interval '1 minute'`); n != 1 {
		t.Fatalf("created_at の既定値が入らない: %d", n)
	}
	mustFail(t, db, "同じ利用者の同じ記事", save)
	mustFail(t, db, "存在しない記事", `INSERT INTO saved_articles ("user_ID", article_id) SELECT "user_ID", 999999 FROM "user"`)
	mustFail(t, db, "存在しない利用者", `INSERT INTO saved_articles ("user_ID", article_id) SELECT 999999, id FROM articles`)
}

func TestSavedArticlesCascadeFromUserAndArticle(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, `INSERT INTO "user" (user_name, password) VALUES ('u1', 'h'), ('u2', 'h')`)
	mustExec(t, db, insertArticle, "qiita", "c1", "https://qiita.com/u/items/c1")
	mustExec(t, db, `INSERT INTO saved_articles ("user_ID", article_id) SELECT u."user_ID", a.id FROM "user" u, articles a`)
	if n := count(t, db, `SELECT count(*) FROM saved_articles`); n != 2 {
		t.Fatalf("saved_articles = %d", n)
	}

	// 利用者を消す（退会）と、その人の保存だけが消える。記事は残る
	mustExec(t, db, `DELETE FROM "user" WHERE user_name = 'u1'`)
	if n := count(t, db, `SELECT count(*) FROM saved_articles`); n != 1 {
		t.Fatalf("saved_articles = %d, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM articles`); n != 1 {
		t.Fatalf("articles = %d", n)
	}

	// 記事を消すと、その記事の保存も消える
	mustExec(t, db, `DELETE FROM articles WHERE external_id = 'c1'`)
	if n := count(t, db, `SELECT count(*) FROM saved_articles`); n != 0 {
		t.Fatalf("saved_articles = %d, want 0", n)
	}
}
