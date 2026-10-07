package migrations_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
)

// 本番の自動デプロイは、DBの移行を入れ替えより先に行い、入れ替えに失敗すると直前のAPIへ戻す。
// DBは巻き戻さないため、戻った直前のAPIが、移行後のDBでも動く必要がある。
// そのため、移行（Up）は「追加だけ」に限る。直前の版が使う列・表を、消さない・名前を変えない・型を変えない・
// 直前の版が書き込めなくなる制約を、足さない。この試験は、その決まりを機械的に守らせる。
// どうしても破壊的な変更が要るときは、自動デプロイに乗せず、手順を決めて、オーナーの承認のもとで行う。
// （その場合は、この検査自体を、承認を得て変更する。注釈で抜け道を作らない。）

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	spaces       = regexp.MustCompile(`\s+`)

	// 消す・名前を変える・中身を捨てる操作。DROP INDEX（索引を消すだけで、直前の版の動作は変わらない）は許す
	dropRule     = regexp.MustCompile(`\bDROP\s+(?:[A-Z_]+\s+)?`)
	dropIndex    = regexp.MustCompile(`\bDROP\s+INDEX\b`)
	renameRule   = regexp.MustCompile(`\bRENAME\b`)
	truncateRule = regexp.MustCompile(`\bTRUNCATE\b`)
	deleteRule   = regexp.MustCompile(`\bDELETE\s+FROM\b`)

	// 直前の版が使う列の型を変える・NULLを禁止する
	alterTypeRule    = regexp.MustCompile(`\bALTER\s+(?:COLUMN\s+)?\S+\s+(?:SET\s+DATA\s+)?TYPE\b`)
	setNotNullRule   = regexp.MustCompile(`\bALTER\s+(?:COLUMN\s+)?\S+\s+SET\s+NOT\s+NULL\b`)
	addColumnClause  = regexp.MustCompile(`\bADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?("[^"]+"|\w+)\s+([^,;]*)`)
	hasNotNull       = regexp.MustCompile(`\bNOT\s+NULL\b`)
	hasFillOrDefault = regexp.MustCompile(`\b(?:DEFAULT|GENERATED|SERIAL|BIGSERIAL|SMALLSERIAL)\b`)
)

// upSection は、goose SQLの Up の部分だけを返す（Down は開発環境で順方向を作り直すためのもので、本番では使わない）。
func upSection(sql string) string {
	upIndex := strings.Index(sql, "-- +goose Up")
	if upIndex >= 0 {
		sql = sql[upIndex+len("-- +goose Up"):]
	}
	if downIndex := strings.Index(sql, "-- +goose Down"); downIndex >= 0 {
		sql = sql[:downIndex]
	}
	return sql
}

// normalize は、コメントを除き、空白を1つにして大文字にそろえる（規則の照合用）。
func normalize(sql string) string {
	sql = blockComment.ReplaceAllString(sql, " ")
	sql = lineComment.ReplaceAllString(sql, " ")
	sql = spaces.ReplaceAllString(sql, " ")
	return strings.ToUpper(strings.TrimSpace(sql))
}

// destructiveFindings は、移行（Up）の中の、追加だけという決まりに反する操作を、日本語の説明で返す。
func destructiveFindings(sql string) []string {
	text := normalize(upSection(sql))
	var findings []string

	if dropRule.MatchString(text) {
		// DROP INDEX だけは許す。他のDROPが1つでもあれば違反
		withoutIndexDrops := dropIndex.ReplaceAllString(text, " ")
		if dropRule.MatchString(withoutIndexDrops) {
			findings = append(findings, "DROP（表・列・制約などを消す）")
		}
	}
	if renameRule.MatchString(text) {
		findings = append(findings, "RENAME（名前を変える）")
	}
	if truncateRule.MatchString(text) {
		findings = append(findings, "TRUNCATE（中身を捨てる）")
	}
	if deleteRule.MatchString(text) {
		findings = append(findings, "DELETE FROM（データを消す）")
	}
	if alterTypeRule.MatchString(text) {
		findings = append(findings, "列の型の変更")
	}
	if setNotNullRule.MatchString(text) {
		findings = append(findings, "既存の列を NOT NULL にする")
	}
	for _, match := range addColumnClause.FindAllStringSubmatch(text, -1) {
		definition := match[2]
		if hasNotNull.MatchString(definition) && !hasFillOrDefault.MatchString(definition) {
			findings = append(findings, "既定値の無い NOT NULL の列の追加（直前の版の書き込みが失敗する）: "+match[1])
		}
	}
	return findings
}

func TestAllMigrationsAreAdditiveOnly(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if findings := destructiveFindings(string(content)); len(findings) > 0 {
			t.Errorf("%s は、追加だけという決まりに反しています（自動デプロイでは、入れ替えに失敗して直前のAPIへ戻ったとき、DBは巻き戻らないため）: %s",
				entry.Name(), strings.Join(findings, " / "))
		}
	}
	if checked == 0 {
		t.Fatal("検査するマイグレーションがありません（埋め込みの失敗の可能性）")
	}
}

func TestDestructiveFindingsRejectsBreakingChanges(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"表を消す", "-- +goose Up\nDROP TABLE tag;"},
		{"表を消す（存在すれば）", "-- +goose Up\nDROP TABLE IF EXISTS tag;"},
		{"列を消す", "-- +goose Up\nALTER TABLE \"user\" DROP COLUMN password;"},
		{"列を消す（COLUMNの省略）", "-- +goose Up\nALTER TABLE \"user\" DROP password;"},
		{"制約を消す", "-- +goose Up\nALTER TABLE tag DROP CONSTRAINT tag_tag_name_key;"},
		{"名前を変える", "-- +goose Up\nALTER TABLE tag RENAME COLUMN tag_name TO name;"},
		{"表の名前を変える", "-- +goose Up\nALTER TABLE tag RENAME TO tags;"},
		{"中身を捨てる", "-- +goose Up\nTRUNCATE recommend;"},
		{"データを消す", "-- +goose Up\nDELETE FROM recommend WHERE match_int = 0;"},
		{"列の型を変える", "-- +goose Up\nALTER TABLE tag ALTER COLUMN tag_name TYPE text;"},
		{"列の型を変える（SET DATA TYPE）", "-- +goose Up\nALTER TABLE tag ALTER COLUMN tag_name SET DATA TYPE text;"},
		{"既存の列をNOT NULLにする", "-- +goose Up\nALTER TABLE tag ALTER COLUMN tag_name SET NOT NULL;"},
		{"既定値の無いNOT NULLの列を足す", "-- +goose Up\nALTER TABLE tag ADD COLUMN note text NOT NULL;"},
		{"複数の列のうち1つが既定値の無いNOT NULL", "-- +goose Up\nALTER TABLE tag ADD COLUMN a int NOT NULL DEFAULT 0, ADD COLUMN b int NOT NULL;"},
		{"小文字でも検出する", "-- +goose Up\nalter table tag drop column tag_name;"},
		{"改行や余分な空白があっても検出する", "-- +goose Up\nALTER   TABLE tag\n   DROP\n   COLUMN tag_name;"},
		{"コメントの後ろの本文も検査する", "-- +goose Up\n-- 説明\nSELECT 1;\nDROP TABLE tag; -- 末尾のコメント"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := destructiveFindings(tc.sql); len(findings) == 0 {
				t.Errorf("破壊的な変更を検出できませんでした: %s", tc.sql)
			}
		})
	}
}

func TestDestructiveFindingsAcceptsAdditiveChanges(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"表の追加", "-- +goose Up\nCREATE TABLE t (id serial PRIMARY KEY, name text NOT NULL);"},
		{"索引の追加", "-- +goose Up\nCREATE INDEX t_name_idx ON t (name);"},
		{"索引を消すだけなら許す", "-- +goose Up\nDROP INDEX IF EXISTS t_name_idx;"},
		{"既定値付きのNOT NULLの列の追加", "-- +goose Up\nALTER TABLE \"user\" ADD COLUMN role varchar(20) NOT NULL DEFAULT 'member';"},
		{"NULLを許す列の追加", "-- +goose Up\nALTER TABLE tag ADD COLUMN note text;"},
		{"制約の追加（新しい列への）", "-- +goose Up\nALTER TABLE \"user\" ADD CONSTRAINT user_role_check CHECK (role IN ('member', 'admin'));"},
		{"データの追加", "-- +goose Up\nINSERT INTO tag (tag_name) VALUES ('go');"},
		{"Down の中の破壊的な操作は、本番では使わないので見ない",
			"-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Down\nDROP TABLE t;"},
		{"コメントの中の語は見ない", "-- +goose Up\n-- DROP TABLE は使わない\nCREATE TABLE t (id int);"},
		{"ブロックコメントの中の語は見ない", "-- +goose Up\n/* DROP TABLE x; */\nCREATE TABLE t (id int);"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := destructiveFindings(tc.sql); len(findings) > 0 {
				t.Errorf("追加だけの変更を、破壊的と誤検出しました: %v\n%s", findings, tc.sql)
			}
		})
	}
}
