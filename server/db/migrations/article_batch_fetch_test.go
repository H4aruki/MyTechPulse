package migrations_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/zenn"
)

const articleBatchFetchFile = "00008_article_batch_fetch.sql"

func readArticleBatchFetch(t *testing.T) string {
	t.Helper()
	b, err := migrations.FS.ReadFile(articleBatchFetchFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return db
}

func mustFail(t *testing.T, db *sql.DB, what, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err == nil {
		t.Fatalf("%s: 制約に反するのに成功した", what)
	}
}

const insertArticle = `INSERT INTO articles (source, external_id, url, title, published_at) VALUES ($1, $2, $3, 't', now())`

func TestArticleBatchFetchTablesAndConstraints(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, insertArticle, "qiita", "id1", "https://qiita.com/u/items/id1")
	mustExec(t, db, insertArticle, "zenn", "id1", "https://zenn.dev/u/articles/id1") // sourceが違えば同じIDでよい

	mustFail(t, db, "同じ(source, external_id)", insertArticle, "qiita", "id1", "https://qiita.com/u/items/other")
	mustFail(t, db, "同じURL", insertArticle, "qiita", "id2", "https://qiita.com/u/items/id1")
	mustFail(t, db, "不明なsource", insertArticle, "devto", "id3", "https://dev.to/x")
	mustFail(t, db, "負のいいね数", `INSERT INTO articles (source, external_id, url, title, likes, published_at) VALUES ('qiita','neg','https://qiita.com/neg','t',-1, now())`)
	mustFail(t, db, "公開日時なし", `INSERT INTO articles (source, external_id, url, title) VALUES ('qiita','np','https://qiita.com/np','t')`)

	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES ('ArticleBatchTag')`)
	mustFail(t, db, "存在しない記事へのタグ", `INSERT INTO article_tags (article_id, "tag_ID", published_at) VALUES (999999, (SELECT "tag_ID" FROM tag WHERE tag_name='ArticleBatchTag'), now())`)
	mustFail(t, db, "存在しないタグ", `INSERT INTO article_tags (article_id, "tag_ID", published_at) SELECT id, 999999, now() FROM articles LIMIT 1`)
	mustFail(t, db, "空のzenn_topic", `INSERT INTO fetch_tags ("tag_ID", zenn_topic) SELECT "tag_ID", '' FROM tag WHERE tag_name='ArticleBatchTag'`)

	mustExec(t, db, `INSERT INTO fetch_runs (slot_at, trigger) VALUES (now(), 'scheduled')`)
	mustFail(t, db, "不明なtrigger", `INSERT INTO fetch_runs (slot_at, trigger) VALUES (now(), 'manual')`)
	mustFail(t, db, "回数が4", `INSERT INTO fetch_runs (slot_at, trigger, attempt) VALUES (now(), 'retry', 4)`)
	mustFail(t, db, "不明なstatus", `INSERT INTO fetch_runs (slot_at, trigger, status) VALUES (now(), 'retry', 'done')`)
	var status, msg string
	if err := db.QueryRow(`SELECT status, error_message FROM fetch_runs`).Scan(&status, &msg); err != nil || status != "running" || msg != "" {
		t.Fatalf("既定値: status = %q, msg = %q, err = %v", status, msg, err)
	}
}

func TestArticleBatchFetchCascadesFromTagAndArticle(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES ('CascadeTag')`)
	mustExec(t, db, insertArticle, "qiita", "c1", "https://qiita.com/u/items/c1")
	mustExec(t, db, `INSERT INTO article_tags (article_id, "tag_ID", published_at)
		SELECT a.id, t."tag_ID", now() FROM articles a, tag t WHERE a.external_id = 'c1' AND t.tag_name = 'CascadeTag'`)
	mustExec(t, db, `INSERT INTO fetch_tags ("tag_ID", zenn_topic) SELECT "tag_ID", 'cascadetag' FROM tag WHERE tag_name = 'CascadeTag'`)

	// タグを消すと、そのタグの記事タグと取得対象も消える（記事は残る）
	mustExec(t, db, `DELETE FROM tag WHERE tag_name = 'CascadeTag'`)
	if n := count(t, db, `SELECT count(*) FROM article_tags`); n != 0 {
		t.Fatalf("article_tags = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM fetch_tags WHERE zenn_topic = 'cascadetag'`); n != 0 {
		t.Fatalf("fetch_tags = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM articles WHERE external_id = 'c1'`); n != 1 {
		t.Fatalf("articles = %d", n)
	}

	// 記事を消すと、その記事のタグ付けも消える
	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES ('CascadeTag2')`)
	mustExec(t, db, `INSERT INTO article_tags (article_id, "tag_ID", published_at)
		SELECT a.id, t."tag_ID", now() FROM articles a, tag t WHERE a.external_id = 'c1' AND t.tag_name = 'CascadeTag2'`)
	mustExec(t, db, `DELETE FROM articles WHERE external_id = 'c1'`)
	if n := count(t, db, `SELECT count(*) FROM article_tags`); n != 0 {
		t.Fatalf("article_tags = %d", n)
	}
}

func TestArticleBatchFetchSeedsTheSignupTagsWithZennTopics(t *testing.T) {
	db := migratedDatabase(t)
	if n := count(t, db, `SELECT count(*) FROM fetch_tags`); n != signupTagCount {
		t.Fatalf("fetch_tags = %d, want %d", n, signupTagCount)
	}
	if n := count(t, db, `SELECT count(*) FROM fetch_tags WHERE NOT enabled`); n != 0 {
		t.Fatalf("無効な行がある: %d", n)
	}
	rows, err := db.Query(`SELECT t.tag_name, f.zenn_topic FROM fetch_tags f JOIN tag t USING ("tag_ID")`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name, topic string
		if err := rows.Scan(&name, &topic); err != nil {
			t.Fatal(err)
		}
		// 00008 はタグ名から導いた値を入れ、Zennのトピック名と違う11件だけを 00009 が直す
		if want := expectedZennTopic(name); topic != want {
			t.Errorf("%q の zenn_topic = %q, want %q (zenn.TopicFor = %q)", name, topic, want, zenn.TopicFor(name))
		}
		seen++
	}
	if err := rows.Err(); err != nil || seen != signupTagCount {
		t.Fatalf("seen = %d, err = %v", seen, err)
	}
}

func TestArticleBatchFetchSeedListMatchesSignupTags(t *testing.T) {
	var fromBatch []string
	for _, m := range sqlValueRule.FindAllStringSubmatch(upSection(readArticleBatchFetch(t)), -1) {
		fromBatch = append(fromBatch, strings.ReplaceAll(m[1], "''", "'"))
	}
	if !reflect.DeepEqual(sorted(fromBatch), sorted(signupTagsFromSQL(t))) {
		t.Fatalf("%s の初期タグが %s と一致しない\n%s: %q\n%s: %q", articleBatchFetchFile, seedMigrationFile, articleBatchFetchFile, sorted(fromBatch), seedMigrationFile, sorted(signupTagsFromSQL(t)))
	}
	if len(fromBatch) != signupTagCount {
		t.Fatalf("初期タグ数 = %d", len(fromBatch))
	}
}

func TestArticleBatchFetchSeedIsRepeatableAndKeepsExistingRows(t *testing.T) {
	db := migratedDatabase(t)
	up := upSection(readArticleBatchFetch(t))
	start := strings.Index(up, "INSERT INTO fetch_tags")
	if start < 0 {
		t.Fatal("初期値のINSERTが見つからない")
	}
	insert := up[start:]

	// 取得対象でないタグ(クリックで増えたタグなど)は入らない。既にある行は書き換えない。
	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES ('ClickedOnlyTag')`)
	mustExec(t, db, `UPDATE fetch_tags SET zenn_topic = 'custom-topic', enabled = false WHERE zenn_topic = 'python'`)
	mustExec(t, db, insert)
	mustExec(t, db, insert)
	if n := count(t, db, `SELECT count(*) FROM fetch_tags`); n != signupTagCount {
		t.Fatalf("fetch_tags = %d, want %d", n, signupTagCount)
	}
	if n := count(t, db, `SELECT count(*) FROM fetch_tags WHERE zenn_topic = 'custom-topic' AND NOT enabled`); n != 1 {
		t.Fatalf("既にある行が書き換わった")
	}
}
