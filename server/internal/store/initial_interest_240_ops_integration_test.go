package store_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

// #240 の一回だけの作業(ops/fix_initial_interest_240.sh)が流すSQLを、本物のPostgreSQLで確かめる。

// ii240Conn は ops/sql/initial_interest_240_functions.sql を流した接続を返す(一時的な関数は接続ごと)。
func ii240Conn(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	sqlText, err := os.ReadFile(filepath.Join("..", "..", "..", "ops", "sql", "initial_interest_240_functions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	if _, err := conn.Exec(context.Background(), string(sqlText)); err != nil {
		t.Fatalf("functions: %v", err)
	}
	return conn
}

// oldSignup は #240 より前の登録(選んだタグ10000、tag表のほかの全タグ8000)を再現する。
func oldSignup(t *testing.T, pool *pgxpool.Pool, name string, token byte, selected []string) int64 {
	t.Helper()
	ctx := context.Background()
	ensureTags(t, pool, selected...)
	user, err := store.NewAuth(pool).CreateWithInterestsAndSession(ctx, name, "hash", auth.RoleMember, selected, hashOf(token), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	// 削除した CreateUnselectedRecommendations と同じ問い合わせ
	if _, err := pool.Exec(ctx, `INSERT INTO recommend ("user_ID", "tag_ID", match_int)
		SELECT DISTINCT ON (lower(btrim(t.tag_name))) $1::int, t."tag_ID", 8000 FROM tag t
		WHERE NOT EXISTS (SELECT 1 FROM recommend r JOIN tag st ON st."tag_ID" = r."tag_ID"
		  WHERE r."user_ID" = $1::int AND lower(btrim(st.tag_name)) = lower(btrim(t.tag_name)))
		ORDER BY lower(btrim(t.tag_name)), t."tag_ID"`, user.ID); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

// legacySignup は #203 より前の登録(選んだタグだけ、値1)を再現する。
func legacySignup(t *testing.T, pool *pgxpool.Pool, name string, token byte, selected []string) int64 {
	t.Helper()
	ensureTags(t, pool, selected...)
	user, err := store.NewAuth(pool).CreateWithInterestsAndSession(context.Background(), name, "hash", auth.RoleMember, selected, hashOf(token), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE recommend SET match_int = 1 WHERE "user_ID" = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

var pgxTxReadOnly = pgx.TxOptions{AccessMode: pgx.ReadOnly}

func seedTags(t *testing.T, pool *pgxpool.Pool, n int) []string {
	t.Helper()
	names := seedTagsNames(n)
	if _, err := pool.Exec(context.Background(), `INSERT INTO tag(tag_name) SELECT unnest($1::text[])`, names); err != nil {
		t.Fatal(err)
	}
	return names
}

// click は、#239 より前のクリックを再現する。当時はクリックしたタグが tag 表に無ければ作っていた。
// いまのクリックはタグを作らないので、先に tag 表へ入れてから押す。
func click(t *testing.T, pool *pgxpool.Pool, userID int64, tags ...string) {
	t.Helper()
	for _, tag := range tags {
		if _, err := pool.Exec(context.Background(), `INSERT INTO tag(tag_name) SELECT $1::text
			WHERE NOT EXISTS (SELECT 1 FROM tag WHERE lower(btrim(tag_name)) = lower(btrim($1::text)))`, tag); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.NewInterest(pool).UpdateForClick(context.Background(), userID, tags); err != nil {
		t.Fatal(err)
	}
}

func weightsByTag(t *testing.T, pool *pgxpool.Pool, userID int64) map[string]int64 {
	t.Helper()
	list, err := store.NewInterest(pool).List(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, w := range list {
		out[interest.NormalizeTag(w.Tag)] = w.Value
	}
	return out
}

type ii240Summary struct{ deleteUsers, deleteRows, raiseUsers, raiseRows int }

func summary(t *testing.T, conn *pgxpool.Conn) ii240Summary {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, pgxTxReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT kind, users, rows FROM pg_temp.ii240_summary()`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var s ii240Summary
	for rows.Next() {
		var kind string
		var users, n int
		if err := rows.Scan(&kind, &users, &n); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "delete_unselected":
			s.deleteUsers, s.deleteRows = users, n
		case "raise_legacy":
			s.raiseUsers, s.raiseRows = users, n
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInitialInterest240FixDeletesNeverClickedUnselectedAndRaisesLegacy(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	seedTags(t, pool, 30)

	// A: Goだけを選び、一度も押していない
	a := oldSignup(t, pool, "a", 101, []string{"Go"})
	// B: Go・Rustを選び、未選択のTag005と、まだ無いタグ(NewTag)と、Goを押した
	b := oldSignup(t, pool, "b", 102, []string{"Go", "Rust"})
	click(t, pool, b, "Tag005")
	click(t, pool, b, "NewTag")
	click(t, pool, b, "Tag005", "go")
	// B の登録より後に、ほかの人のクリックでできたタグ(Later)。B は後から押したので、0.2から始まった行を持つ
	c := oldSignup(t, pool, "c", 103, []string{"Tag001"})
	click(t, pool, c, "Later")
	click(t, pool, b, "later")
	// D: 40回押して、未選択の行が0まで下がった人(値0の行は見分けられないので残す)
	d := oldSignup(t, pool, "d", 104, []string{"Go"})
	for range 40 {
		click(t, pool, d, "Go")
	}
	// E: #203 より前の登録で、25個選んで一度も押していない(値はすべて1)
	legacyTags := seedTagsNames(30)[:25]
	e := legacySignup(t, pool, "e", 105, legacyTags)
	// F: #203 より前の登録で、一度押した(1は0になり、押したタグは2000)
	f := legacySignup(t, pool, "f", 106, []string{"Go", "Rust"})
	click(t, pool, f, "Go")

	before := map[int64]map[string]int64{}
	for _, id := range []int64{a, b, c, d, e, f} {
		before[id] = weightsByTag(t, pool, id)
	}
	conn := ii240Conn(t, pool)

	got := summary(t, conn)
	// A: 未選択の30行(Tag001〜030)。B: 30行から、押したTag005を除く29行。
	// C: 登録時の33タグ(Tag001〜030、Go、Rust、NewTag)から、選んだTag001を除く32行。D は値0なので対象外
	want := ii240Summary{deleteUsers: 3, deleteRows: 30 + 29 + 32, raiseUsers: 1, raiseRows: 25}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
	if _, err := conn.Exec(ctx, `CALL pg_temp.ii240_run()`); err != nil {
		t.Fatal(err)
	}
	if got := summary(t, conn); got != (ii240Summary{}) {
		t.Fatalf("実行後の残り = %+v", got)
	}

	// A: Goだけが残り、フィードの上位はGoだけ(残りは人気の記事で埋まる)
	if w := weightsByTag(t, pool, a); len(w) != 1 || w["go"] != 10000 {
		t.Fatalf("A = %v", w)
	}
	// B: 押したタグと選んだタグは値ごと残り、押していない未選択の行だけが消える
	wb := weightsByTag(t, pool, b)
	for _, tag := range []string{"go", "rust", "tag005", "newtag", "later"} {
		if wb[tag] != before[b][tag] || wb[tag] == 0 {
			t.Fatalf("B の %s が変わった: %d → %d", tag, before[b][tag], wb[tag])
		}
	}
	if len(wb) != 5 {
		t.Fatalf("B = %v", wb)
	}
	// D: 値0の行は残す(行数も値も変わらない)
	if wd := weightsByTag(t, pool, d); fmt.Sprint(wd) != fmt.Sprint(before[d]) {
		t.Fatalf("D が変わった: %v", wd)
	}
	// E: 25行すべて10000、消していない
	we := weightsByTag(t, pool, e)
	if len(we) != 25 {
		t.Fatalf("E rows = %d", len(we))
	}
	for tag, v := range we {
		if v != interest.InitialSelected {
			t.Fatalf("E の %s = %d", tag, v)
		}
	}
	// F: そのまま
	if wf := weightsByTag(t, pool, f); fmt.Sprint(wf) != fmt.Sprint(before[f]) || wf["go"] != 2000 || wf["rust"] != 0 {
		t.Fatalf("F が変わった: %v", wf)
	}
	// 何度流しても同じ
	if _, err := conn.Exec(ctx, `CALL pg_temp.ii240_run()`); err != nil {
		t.Fatal(err)
	}
	if wb2 := weightsByTag(t, pool, b); fmt.Sprint(wb2) != fmt.Sprint(wb) {
		t.Fatalf("2回目で変わった: %v", wb2)
	}
}

func seedTagsNames(n int) []string {
	var names []string
	for i := 1; i <= n; i++ {
		names = append(names, fmt.Sprintf("Tag%03d", i))
	}
	return names
}

// 実行中のクリックとぶつからないよう、クリックと同じ利用者の行ロックを待つ
func TestInitialInterest240ApplyTakesClickRowLock(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	seedTags(t, pool, 25)
	a := oldSignup(t, pool, "lock", 111, []string{"Go"})
	conn := ii240Conn(t, pool)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT "user_ID" FROM "user" WHERE "user_ID" = $1 FOR UPDATE`, a); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `SELECT * FROM pg_temp.ii240_apply($1)`, a)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("ロックを待たずに進んだ: %v", err)
	}
	_ = tx.Rollback(ctx)
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID" = $1`, a); n != 26 {
		t.Fatalf("ロック中に行が変わった: %d", n)
	}
	// ロックが外れれば直せる
	_ = holder.Rollback(ctx)
	var deleted, raised int
	if err := conn.QueryRow(ctx, `SELECT deleted, raised FROM pg_temp.ii240_apply($1)`, a).Scan(&deleted, &raised); err != nil {
		t.Fatal(err)
	}
	if deleted != 25 || raised != 0 {
		t.Fatalf("deleted=%d raised=%d", deleted, raised)
	}
	// 存在しない利用者は何もしない
	if err := conn.QueryRow(ctx, `SELECT deleted, raised FROM pg_temp.ii240_apply(999999)`).Scan(&deleted, &raised); err != nil || deleted != 0 || raised != 0 {
		t.Fatalf("missing user: %d %d %v", deleted, raised, err)
	}
}

// 本物のクリック学習で、ランダムに選んで押した利用者たち。消す行が「選ばず一度も押していない行」と一致する
// (値が0の行は見分けられないので数えない。#240 の調査と同じ条件)。
func TestInitialInterest240IdentificationMatchesGroundTruth(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	// 本番と同じく約100タグ(移行00006の会員登録用タグと同じ数)。見分けるには、未選択で押していない行が20行以上要る
	tags := seedTags(t, pool, 100)
	rng := rand.New(rand.NewSource(240))
	type truth struct {
		id      int64
		touched map[string]bool // 選んだか、一度でも押したタグ
	}
	var users []truth
	for i := 0; i < 16; i++ {
		touched := map[string]bool{}
		var selected []string
		for range 1 + rng.Intn(4) {
			tag := tags[rng.Intn(len(tags))]
			selected = append(selected, tag)
			touched[interest.NormalizeTag(tag)] = true
		}
		id := oldSignup(t, pool, fmt.Sprintf("r%02d", i), byte(120+i), selected)
		for range rng.Intn(14) {
			var clicked []string
			for range 1 + rng.Intn(3) {
				tag := tags[rng.Intn(len(tags))]
				if rng.Float64() < 0.25 {
					tag = fmt.Sprintf("born%d", rng.Intn(5))
				}
				clicked = append(clicked, tag)
				touched[interest.NormalizeTag(tag)] = true
			}
			click(t, pool, id, clicked...)
		}
		users = append(users, truth{id: id, touched: touched})
	}
	conn := ii240Conn(t, pool)
	for _, u := range users {
		rows, err := conn.Query(ctx, `SELECT t.tag_name FROM pg_temp.ii240_plan($1) p JOIN tag t ON t."tag_ID" = p.tag_id WHERE p.kind = 'delete_unselected'`, u.id)
		if err != nil {
			t.Fatal(err)
		}
		var planned []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			planned = append(planned, interest.NormalizeTag(name))
		}
		rows.Close()
		var want []string
		for tag, v := range weightsByTag(t, pool, u.id) {
			if !u.touched[tag] && v > 0 {
				want = append(want, tag)
			}
		}
		sort.Strings(planned)
		sort.Strings(want)
		if fmt.Sprint(planned) != fmt.Sprint(want) {
			t.Fatalf("user %d: 消す行 %v、正解 %v", u.id, planned, want)
		}
	}
}
