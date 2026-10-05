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
	user, err := authRepo.CreateWithInterestsAndSession(ctx, "click-user", "hash", auth.RoleMember, []string{"Go"}, hashOf(41), fixedNow.Add(time.Hour))
	if err != nil {
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
	user, err := authRepo.CreateWithInterestsAndSession(ctx, "collision-user", "hash", auth.RoleMember, []string{"Go"}, hashOf(42), fixedNow.Add(time.Hour))
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
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID"=$1 AND match_int IN (1,1725)`, user.ID); n != 2 {
		t.Fatalf("rows changed after collision: %d", n)
	}
}

var _ = time.Second
