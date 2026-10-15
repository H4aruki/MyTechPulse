package migrations_test

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

const (
	seedMigrationFile = "00006_seed_signup_tags.sql"
	signupTagsFile    = "../../../frontend/src/constants/tags.ts"
	signupTagCount    = 100
)

var (
	tagsArrayRule = regexp.MustCompile(`(?s)tags:\s*\[(.*?)\]`)
	tsStringRule  = regexp.MustCompile(`'([^']*)'`)
	sqlValueRule  = regexp.MustCompile(`(?m)^\s*\('((?:[^']|'')*)'\),?\s*$`)
)

func readSeedMigration(t *testing.T) string {
	t.Helper()
	b, err := migrations.FS.ReadFile(seedMigrationFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func signupTagsFromTS(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(signupTagsFile)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, block := range tagsArrayRule.FindAllStringSubmatch(string(b), -1) {
		for _, m := range tsStringRule.FindAllStringSubmatch(block[1], -1) {
			tags = append(tags, m[1])
		}
	}
	return tags
}

func signupTagsFromSQL(t *testing.T) []string {
	t.Helper()
	var tags []string
	for _, m := range sqlValueRule.FindAllStringSubmatch(upSection(readSeedMigration(t)), -1) {
		tags = append(tags, strings.ReplaceAll(m[1], "''", "'"))
	}
	return tags
}

func sorted(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func TestSeedSignupTagsMatchFrontendList(t *testing.T) {
	ts := signupTagsFromTS(t)
	sqlTags := signupTagsFromSQL(t)
	if len(ts) != signupTagCount {
		t.Fatalf("tags.ts のタグ数 = %d, want %d", len(ts), signupTagCount)
	}
	if !reflect.DeepEqual(sorted(ts), sorted(sqlTags)) {
		t.Fatalf("tags.ts と %s のタグが一致しない\ntags.ts: %q\nmigration: %q", seedMigrationFile, sorted(ts), sorted(sqlTags))
	}
}

func TestSeedSignupTagsHaveNoCaseOnlyDuplicates(t *testing.T) {
	seen := map[string]string{}
	for _, tag := range signupTagsFromSQL(t) {
		key := strings.ToLower(strings.TrimSpace(tag))
		if prev, ok := seen[key]; ok {
			t.Errorf("%q と %q は大文字小文字・空白だけが違う", prev, tag)
		}
		seen[key] = tag
	}
}

func TestSeedSignupTagsAreInsertedOnce(t *testing.T) {
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM tag`); n != signupTagCount {
		t.Fatalf("登録後のタグ数 = %d", n)
	}
	// 同じSQLをもう一度流しても増えない
	mustExec(t, db, upSection(readSeedMigration(t)))
	if n := count(t, db, `SELECT count(*) FROM tag`); n != signupTagCount {
		t.Fatalf("2回目の後のタグ数 = %d", n)
	}
}

func TestSeedSignupTagsSkipExistingNameIgnoringCase(t *testing.T) {
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	// 小文字の 'go' が先にある状態を作る（Go を消して go を入れる）
	mustExec(t, db, `DELETE FROM tag WHERE tag_name = 'Go'`)
	mustExec(t, db, `INSERT INTO tag (tag_name) VALUES (' go')`)
	mustExec(t, db, upSection(readSeedMigration(t)))
	if n := count(t, db, `SELECT count(*) FROM tag WHERE lower(btrim(tag_name)) = 'go'`); n != 1 {
		t.Fatalf("go 系のタグ数 = %d, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM tag`); n != signupTagCount {
		t.Fatalf("タグ数 = %d, want %d", n, signupTagCount)
	}
}
