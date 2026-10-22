package migrations_test

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/zenn"
)

const fixZennTopicSlugsFile = "00009_fix_zenn_topic_slugs.sql"

// zennTopicFixes は、00009 が直すタグ（表示名）と、Zennに実在するトピック名。
// 直す前の値は、00008 と同じ導き方の zenn.TopicFor(表示名)。
var zennTopicFixes = map[string]string{
	"Next.js":      "nextjs",
	"CI/CD":        "cicd",
	"Node.js":      "nodejs",
	"scikit-learn": "scikitlearn",
	"自然言語処理（NLP）":  "nlp",
	"TDD（テスト駆動開発）": "tdd",
	"C++":          "cpp",
	"C#":           "csharp",
	"Vue.js":       "vue",
	"Nuxt.js":      "nuxtjs",
	"Sass/SCSS":    "scss",
}

// expectedZennTopic は、全移行の後の、表示名 name の zenn_topic。
func expectedZennTopic(name string) string {
	if topic, ok := zennTopicFixes[name]; ok {
		return topic
	}
	return zenn.TopicFor(name)
}

var topicPairRule = regexp.MustCompile(`(?m)^\s*\('((?:[^']|'')*)',\s*'((?:[^']|'')*)'\),?\s*$`)

func readFixZennTopicSlugs(t *testing.T) string {
	t.Helper()
	b, err := migrations.FS.ReadFile(fixZennTopicSlugsFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// zennTopicsByTagName は、取得対象タグの表示名ごとの zenn_topic を返す。
func zennTopicsByTagName(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT t.tag_name, f.zenn_topic FROM fetch_tags f JOIN tag t USING ("tag_ID")`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, topic string
		if err := rows.Scan(&name, &topic); err != nil {
			t.Fatal(err)
		}
		out[name] = topic
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Up の WHERE が本当に00008の値に当たるよう、旧値を1文字ずつ（全角の括弧も）照らし合わせる。DBは要らない。
func TestFixZennTopicSlugsUpMatchesTheSeededValues(t *testing.T) {
	want := map[string]string{}
	for name, topic := range zennTopicFixes {
		want[zenn.TopicFor(name)] = topic
	}
	got := map[string]string{}
	for _, m := range topicPairRule.FindAllStringSubmatch(upSection(readFixZennTopicSlugs(t)), -1) {
		got[m[1]] = m[2]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s の (旧, 新) が想定と違う\ngot:  %q\nwant: %q", fixZennTopicSlugsFile, got, want)
	}
	seeded := map[string]bool{}
	for _, name := range signupTagsFromSQL(t) {
		seeded[name] = true
	}
	for name := range zennTopicFixes {
		if !seeded[name] {
			t.Errorf("%q は 00006/00008 の初期タグにない", name)
		}
	}
}

func TestFixZennTopicSlugsUpAndDown(t *testing.T) {
	db := migratedDatabase(t)
	// Down が、同じトピック名を正しく使う別のタグを巻き込まないことも確かめる
	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES ('NodeJS Other')`)
	mustExec(t, db, `INSERT INTO fetch_tags ("tag_ID", zenn_topic) SELECT "tag_ID", 'nodejs' FROM tag WHERE tag_name = 'NodeJS Other'`)

	// Up の後: 11件は新しいトピック名、他はタグ名から導いた値のまま。表示名は変わらない
	after := zennTopicsByTagName(t, db)
	if len(after) != signupTagCount+1 {
		t.Fatalf("fetch_tags = %d, want %d", len(after), signupTagCount+1)
	}
	for name, topic := range after {
		want := expectedZennTopic(name)
		if name == "NodeJS Other" {
			want = "nodejs"
		}
		if topic != want {
			t.Errorf("Up後: %q の zenn_topic = %q, want %q", name, topic, want)
		}
	}
	for name := range zennTopicFixes {
		if _, ok := after[name]; !ok {
			t.Errorf("表示名 %q が見つからない（表示名が変わった可能性）", name)
		}
	}

	// 2回流しても変わらない
	mustExec(t, db, upSection(readFixZennTopicSlugs(t)))
	if again := zennTopicsByTagName(t, db); !reflect.DeepEqual(again, after) {
		t.Fatalf("Up を2回流すと値が変わった\nafter: %q\nagain: %q", after, again)
	}

	// Down の後: 11件は元の値（zenn.TopicFor(表示名)）に戻り、別のタグの 'nodejs' は残る
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownToContext(context.Background(), db, ".", 8); err != nil {
		t.Fatalf("down to 00008: %v", err)
	}
	for name, topic := range zennTopicsByTagName(t, db) {
		want := zenn.TopicFor(name)
		if name == "NodeJS Other" {
			want = "nodejs"
		}
		if topic != want {
			t.Errorf("Down後: %q の zenn_topic = %q, want %q", name, topic, want)
		}
	}
}
