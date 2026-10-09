package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const (
	promptA = "11111111-1111-4111-8111-111111111111"
	promptB = "22222222-2222-4222-8222-222222222222"
	promptC = "33333333-3333-4333-8333-333333333333"
)

func feedbackUser(t *testing.T, pool *pgxpool.Pool, name string, token byte) int64 {
	t.Helper()
	user, err := store.NewAuth(pool).CreateWithInterestsAndSession(context.Background(), name, "hash", auth.RoleMember, []string{"Go"}, hashOf(token), fixedNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func TestFeedbackActiveFormIsInitialVersion(t *testing.T) {
	repo := store.NewFeedback(newTestPool(t))
	form, err := repo.ActiveForm(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if form.Key != "service_satisfaction" || form.Version != 1 || form.CooldownDays != 60 || len(form.Questions) != 4 || len(form.FollowupsFor(2)) != 3 {
		t.Fatalf("form = %+v", form)
	}
	if err := userfeedback.ValidateDefinition(form); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackPresentCooldownAndReplay(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-cooldown", 51)
	first, err := repo.Present(ctx, uid, promptA, fixedNow)
	if err != nil || !first.Eligible || first.Prompt == nil || first.Prompt.Status != userfeedback.PromptShown || first.Form == nil {
		t.Fatalf("first = %+v, %v", first, err)
	}
	replay, err := repo.Present(ctx, uid, promptA, fixedNow.Add(time.Hour))
	if err != nil || !replay.Eligible || replay.Prompt.ID != promptA || replay.Form == nil || replay.Form.ID != first.Form.ID {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	blocked, err := repo.Present(ctx, uid, promptB, fixedNow.AddDate(0, 0, 59))
	if err != nil || blocked.Eligible || blocked.NextEligibleAt == nil || !blocked.NextEligibleAt.Equal(fixedNow.AddDate(0, 0, 60)) {
		t.Fatalf("blocked = %+v, %v", blocked, err)
	}
	last, err := repo.LastPrompt(ctx, uid, "service_satisfaction")
	if err != nil || last == nil || last.ID != promptA {
		t.Fatalf("last = %+v, %v", last, err)
	}
	again, err := repo.Present(ctx, uid, promptC, fixedNow.AddDate(0, 0, 60))
	if err != nil || !again.Eligible || again.Prompt.ID != promptC {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts WHERE "user_ID"=$1`, uid); n != 2 {
		t.Fatalf("prompts = %d", n)
	}
}

func TestFeedbackPresentRejectsOtherUsersPrompt(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	alice := feedbackUser(t, pool, "fb-alice", 52)
	bob := feedbackUser(t, pool, "fb-bob", 53)
	if _, err := repo.Present(ctx, alice, promptA, fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Present(ctx, bob, promptA, fixedNow); !errors.Is(err, userfeedback.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := repo.Present(ctx, alice, "not-a-uuid", fixedNow); !errors.Is(err, userfeedback.ErrInvalidInput) {
		t.Fatalf("invalid uuid err = %v", err)
	}
}

func TestFeedbackConcurrentPresentationsShowOnce(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-tabs", 54)
	ids := []string{promptA, promptB}
	results := make([]userfeedback.Presentation, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = repo.Present(ctx, uid, ids[i], fixedNow) }(i)
	}
	wg.Wait()
	shown := 0
	for i := range 2 {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if results[i].Eligible && results[i].Prompt != nil && results[i].Prompt.Status == userfeedback.PromptShown {
			shown++
		}
	}
	if shown != 1 || queryInt(t, pool, `SELECT count(*) FROM feedback_prompts`) != 1 {
		t.Fatalf("shown = %d", shown)
	}
}

func TestFeedbackSamePromptConcurrentRequestsCreateOne(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-same-id", 56)
	results := make([]userfeedback.Presentation, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = repo.Present(ctx, uid, promptA, fixedNow) }(i)
	}
	wg.Wait()
	for i := range 2 {
		if errs[i] != nil || !results[i].Eligible || results[i].Prompt == nil || results[i].Prompt.ID != promptA || results[i].Form == nil {
			t.Fatalf("result[%d] = %+v, %v", i, results[i], errs[i])
		}
	}
	if results[0].Form.ID != results[1].Form.ID || queryInt(t, pool, `SELECT count(*) FROM feedback_prompts`) != 1 {
		t.Fatalf("forms = %d/%d", results[0].Form.ID, results[1].Form.ID)
	}
}

func TestFeedbackCooldownSpansFormVersions(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-versions", 55)
	if _, err := repo.Present(ctx, uid, promptA, fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE feedback_forms SET status='retired' WHERE form_key='service_satisfaction' AND version=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH f AS (INSERT INTO feedback_forms(form_key, version, title, status, cooldown_days)
		VALUES ('service_satisfaction', 2, '版2', 'active', 60) RETURNING id)
		INSERT INTO feedback_questions(form_id, question_key, question_text, sort_order, is_required) SELECT id, 'overall', '版2の質問', 1, true FROM f`); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Present(ctx, uid, promptB, fixedNow.AddDate(0, 0, 1))
	if err != nil || got.Eligible {
		t.Fatalf("版をまたいでも60日以内は出さない: %+v, %v", got, err)
	}
	replay, err := repo.Present(ctx, uid, promptA, fixedNow.AddDate(0, 0, 1))
	if err != nil || replay.Form == nil || replay.Form.Version != 1 {
		t.Fatalf("再送では表示時の版1が返る: %+v, %v", replay, err)
	}
}
