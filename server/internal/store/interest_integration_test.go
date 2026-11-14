package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

func TestInterestRepositoryConcurrentClicksAreSerialized(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	authRepo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	user, err := authRepo.CreateWithInterestsAndSession(ctx, "click-user", "hash", auth.RoleMember, []string{"Go"}, hashOf(41), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	// 初期値のままだと上限1.0に張り付いて直列化を検出できないので、低い値から始める
	if _, err := pool.Exec(ctx, `UPDATE recommend SET match_int=1 WHERE "user_ID"=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	repo := store.NewInterest(pool)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = repo.UpdateForClick(ctx, user.ID, []string{"GO"}) }(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TagID == 0 || got[0].Tag != "Go" || got[0].Value != 3600 {
		t.Fatalf("interests = %#v", got)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID"=$1`, user.ID); n != 1 {
		t.Fatalf("recommend rows = %d", n)
	}
}

func TestInterestRepositoryCollisionDoesNotChangeRows(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	authRepo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	user, err := authRepo.CreateWithInterestsAndSession(ctx, "collision-user", "hash", auth.RoleMember, []string{"Go"}, hashOf(42), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES (' go '), ('Rust')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recommend("user_ID","tag_ID",match_int) SELECT $1,"tag_ID",1725 FROM tag WHERE tag_name=' go '`, user.ID); err != nil {
		t.Fatal(err)
	}
	repo := store.NewInterest(pool)
	if _, err := repo.List(ctx, user.ID); !errors.Is(err, interest.ErrNormalizedTagCollision) {
		t.Fatalf("List error = %v", err)
	}
	if _, err := repo.UpdateForClick(ctx, user.ID, []string{"go"}); !errors.Is(err, interest.ErrNormalizedTagCollision) {
		t.Fatalf("Update error = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID"=$1 AND match_int IN (10000,1725)`, user.ID); n != 2 {
		t.Fatalf("rows changed after collision: %d", n)
	}
}

// #239: クリックAPIは、送られたタグ名から tag 表の行を作らない。tag 表にあるタグだけを学習に使う。
func TestInterestRepositoryClickIgnoresUnknownTags(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	ensureTags(t, pool, "Go")
	user, err := store.NewAuth(pool).CreateWithInterestsAndSession(ctx, "unknown-tag-user", "hash", auth.RoleMember, []string{"Go"}, hashOf(43), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	// Rust は tag 表にあるが、この利用者はまだ行を持っていない(選ばなかったタグ)
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('Rust')`); err != nil {
		t.Fatal(err)
	}
	tagsBefore := queryInt(t, pool, `SELECT count(*) FROM tag`)
	repo := store.NewInterest(pool)
	got, err := repo.UpdateForClick(ctx, user.ID, []string{"GO", " rust ", "no-such-tag-239", "evil\u0007tag"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("updated = %#v", got)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != tagsBefore {
		t.Fatalf("tag rows = %d, want %d", n, tagsBefore)
	}
	list, err := repo.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Go": 10000, "Rust": 2000}
	if len(list) != len(want) {
		t.Fatalf("interests = %#v", list)
	}
	for _, w := range list {
		if w.TagID == 0 || want[w.Tag] != w.Value {
			t.Fatalf("interests = %#v", list)
		}
	}

	// 知らないタグだけのクリックでも、0.8倍の減衰はかかる。行もタグも増えない
	if _, err := repo.UpdateForClick(ctx, user.ID, []string{"no-such-tag-239"}); err != nil {
		t.Fatal(err)
	}
	list, err = repo.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]int64{"Go": 8000, "Rust": 1600}
	if len(list) != len(want) {
		t.Fatalf("interests = %#v", list)
	}
	for _, w := range list {
		if want[w.Tag] != w.Value {
			t.Fatalf("interests = %#v", list)
		}
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != tagsBefore {
		t.Fatalf("tag rows = %d, want %d", n, tagsBefore)
	}
}

var _ = time.Second
