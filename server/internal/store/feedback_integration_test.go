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

func presentForTest(t *testing.T, repo *store.Feedback, uid int64, promptID string) userfeedback.Form {
	t.Helper()
	p, err := repo.Present(context.Background(), uid, promptID, fixedNow)
	if err != nil || p.Form == nil {
		t.Fatalf("present = %+v, %v", p, err)
	}
	return *p.Form
}

func TestFeedbackHighScoreCompletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-high", 61)
	form := presentForTest(t, repo, uid, promptA)
	got, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 4, fixedNow.Add(time.Minute))
	if err != nil || got.Status != userfeedback.SubmissionCompleted || got.FollowupRequired {
		t.Fatalf("submit = %+v, %v", got, err)
	}
	again, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 4, fixedNow.Add(2*time.Minute))
	if err != nil || again.SubmissionID != got.SubmissionID {
		t.Fatalf("再送は既存の結果を返す: %+v, %v", again, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts WHERE status='submitted' AND finished_at IS NOT NULL`); n != 1 {
		t.Fatalf("submitted prompts = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_submissions WHERE status='completed' AND completed_at IS NOT NULL`); n != 1 {
		t.Fatalf("completed submissions = %d", n)
	}
	if _, err := repo.Dismiss(ctx, uid, promptA, fixedNow.Add(3*time.Minute)); !errors.Is(err, userfeedback.ErrConflict) {
		t.Fatalf("回答済みは閉じられない: %v", err)
	}
}

func TestFeedbackLowScoreFollowupAndSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-low", 62)
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('A'),('B'),('C'),('D'),('E'),('F')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recommend("user_ID","tag_ID",match_int) SELECT $1, "tag_ID", CASE tag_name WHEN 'A' THEN 9000 WHEN 'B' THEN 7000 WHEN 'C' THEN 7000 WHEN 'D' THEN 5000 WHEN 'E' THEN 3000 ELSE 100 END FROM tag WHERE tag_name IN ('A','B','C','D','E','F')`, uid); err != nil {
		t.Fatal(err)
	}
	form := presentForTest(t, repo, uid, promptA)
	got, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 2, fixedNow.Add(time.Minute))
	if err != nil || got.Status != userfeedback.SubmissionPartial || !got.FollowupRequired {
		t.Fatalf("submit = %+v, %v", got, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_prompts WHERE stage='followup' AND status='shown'`); n != 1 {
		t.Fatalf("followup prompts = %d", n)
	}
	rows, err := pool.Query(ctx, `SELECT tag_name, match_int FROM feedback_interest_snapshots ORDER BY rank`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		var value int
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if len(names) != 5 || names[0] != "A" || names[1] != "B" || names[2] != "C" || names[4] != "E" {
		t.Fatalf("snapshot = %v", names)
	}
	answers := []userfeedback.Answer{}
	for _, q := range form.FollowupsFor(2) {
		answers = append(answers, userfeedback.Answer{QuestionID: q.ID, Score: 3})
	}
	done, err := repo.CompleteFollowup(ctx, uid, got.SubmissionID, answers, fixedNow.Add(2*time.Minute))
	if err != nil || done.Status != userfeedback.SubmissionCompleted {
		t.Fatalf("complete = %+v, %v", done, err)
	}
	if again, err := repo.CompleteFollowup(ctx, uid, got.SubmissionID, answers, fixedNow.Add(3*time.Minute)); err != nil || again.Status != userfeedback.SubmissionCompleted {
		t.Fatalf("再送は既存の結果を返す: %+v, %v", again, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_answers`); n != 4 {
		t.Fatalf("answers = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_interest_snapshots`); n != 5 {
		t.Fatalf("追加回答ではスナップショットを作り直さない: %d", n)
	}
}

func TestFeedbackFollowupRejectsInvalidAndOtherUsers(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-invalid", 63)
	other := feedbackUser(t, pool, "fb-other", 64)
	form := presentForTest(t, repo, uid, promptA)
	if _, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 6, fixedNow); !errors.Is(err, userfeedback.ErrInvalidInput) {
		t.Fatalf("score 6: %v", err)
	}
	if _, err := repo.SubmitOverall(ctx, other, promptA, form.Root().ID, 3, fixedNow); !errors.Is(err, userfeedback.ErrNotFound) {
		t.Fatalf("other user: %v", err)
	}
	got, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 1, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CompleteFollowup(ctx, uid, got.SubmissionID, []userfeedback.Answer{{QuestionID: form.FollowupsFor(1)[0].ID, Score: 3}}, fixedNow); !errors.Is(err, userfeedback.ErrInvalidInput) {
		t.Fatalf("missing answers: %v", err)
	}
	if _, err := repo.CompleteFollowup(ctx, other, got.SubmissionID, nil, fixedNow); !errors.Is(err, userfeedback.ErrNotFound) {
		t.Fatalf("other user submission: %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_answers`); n != 1 {
		t.Fatalf("失敗した追加回答は保存しない: %d", n)
	}
}

func TestFeedbackDismissDuringFollowupKeepsOverall(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-dismiss", 65)
	form := presentForTest(t, repo, uid, promptA)
	got, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 1, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	s, err := repo.Dismiss(ctx, uid, promptA, fixedNow.Add(time.Minute))
	if err != nil || s.Status != userfeedback.PromptDismissed || s.Stage != userfeedback.StageFollowup {
		t.Fatalf("dismiss = %+v, %v", s, err)
	}
	if again, err := repo.Dismiss(ctx, uid, promptA, fixedNow.Add(2*time.Minute)); err != nil || again.Status != userfeedback.PromptDismissed {
		t.Fatalf("再送は既存の結果を返す: %+v, %v", again, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_submissions WHERE status='partial'`); n != 1 {
		t.Fatalf("部分回答は残る: %d", n)
	}
	if _, err := repo.CompleteFollowup(ctx, uid, got.SubmissionID, nil, fixedNow); !errors.Is(err, userfeedback.ErrConflict) {
		t.Fatalf("閉じた後の追加回答は競合: %v", err)
	}
}

func TestFeedbackDeleteExpiredCascadesAndTagDeleteKeepsSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewFeedback(pool)
	uid := feedbackUser(t, pool, "fb-retention", 66)
	form := presentForTest(t, repo, uid, promptA)
	if _, err := repo.SubmitOverall(ctx, uid, promptA, form.Root().ID, 1, fixedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tag WHERE tag_name='Go'`); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM feedback_interest_snapshots WHERE "tag_ID" IS NULL AND tag_name='Go'`); n != 1 {
		t.Fatalf("タグを消してもスナップショットは残る: %d", n)
	}
	if n, err := repo.DeleteExpired(ctx, fixedNow, 500); err != nil || n != 0 {
		t.Fatalf("early delete = %d, %v", n, err)
	}
	if n, err := repo.DeleteExpired(ctx, fixedNow.AddDate(2, 0, 0).Add(time.Second), 500); err != nil || n != 1 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	for _, table := range []string{"feedback_prompts", "feedback_submissions", "feedback_answers", "feedback_interest_snapshots"} {
		if n := queryInt(t, pool, `SELECT count(*) FROM `+table); n != 0 {
			t.Fatalf("%s rows = %d", table, n)
		}
	}
}
