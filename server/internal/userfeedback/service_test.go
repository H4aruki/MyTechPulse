package userfeedback_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeRepo struct {
	form            userfeedback.Form
	formErr         error
	last            *userfeedback.PromptSummary
	lastErr         error
	lastCalls       int
	presentOut      userfeedback.Presentation
	presentErr      error
	deleteCalls     []time.Time
	deleteErr       error
	submitResult    userfeedback.SubmitResult
	submitErr       error
	submitCalls     int
	submitUser      int64
	submitPrompt    string
	submitQuestion  int64
	submitScore     int32
	submitAt        time.Time
	completeResult  userfeedback.SubmitResult
	completeErr     error
	completeCalls   int
	completeUser    int64
	completeID      string
	completeAnswers []userfeedback.Answer
	completeAt      time.Time
	dismissOut      userfeedback.PromptSummary
	dismissErr      error
	dismissCalls    int
	dismissUser     int64
	dismissPrompt   string
	dismissAt       time.Time
}

func (r *fakeRepo) ActiveForm(context.Context) (userfeedback.Form, error) { return r.form, r.formErr }
func (r *fakeRepo) LastPrompt(context.Context, int64, string) (*userfeedback.PromptSummary, error) {
	r.lastCalls++
	return r.last, r.lastErr
}
func (r *fakeRepo) Present(context.Context, int64, string, time.Time) (userfeedback.Presentation, error) {
	return r.presentOut, r.presentErr
}
func (r *fakeRepo) SubmitOverall(_ context.Context, userID int64, promptID string, questionID int64, score int32, at time.Time) (userfeedback.SubmitResult, error) {
	r.submitCalls++
	r.submitUser, r.submitPrompt, r.submitQuestion, r.submitScore, r.submitAt = userID, promptID, questionID, score, at
	return r.submitResult, r.submitErr
}
func (r *fakeRepo) CompleteFollowup(_ context.Context, userID int64, submissionID string, answers []userfeedback.Answer, at time.Time) (userfeedback.SubmitResult, error) {
	r.completeCalls++
	r.completeUser, r.completeID, r.completeAnswers, r.completeAt = userID, submissionID, answers, at
	return r.completeResult, r.completeErr
}
func (r *fakeRepo) Dismiss(_ context.Context, userID int64, promptID string, at time.Time) (userfeedback.PromptSummary, error) {
	r.dismissCalls++
	r.dismissUser, r.dismissPrompt, r.dismissAt = userID, promptID, at
	if r.dismissErr != nil {
		return userfeedback.PromptSummary{}, r.dismissErr
	}
	out := r.dismissOut
	if out.Status == "" {
		out.Status = userfeedback.PromptDismissed
		out.Stage = userfeedback.StageOverall
	}
	return out, nil
}
func (r *fakeRepo) DeleteExpired(_ context.Context, before time.Time, _ int32) (int64, error) {
	r.deleteCalls = append(r.deleteCalls, before)
	return 0, r.deleteErr
}

var base = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestStatusUsesCooldownOfActiveForm(t *testing.T) {
	repo := &fakeRepo{form: validForm(), last: &userfeedback.PromptSummary{ID: "p", Status: userfeedback.PromptDismissed, ShownAt: base}}
	clock := &fakeClock{now: base.AddDate(0, 0, 59)}
	svc := &userfeedback.Service{Repo: repo, Clock: clock}
	st, err := svc.Status(context.Background(), 1)
	if err != nil || st.Eligible || st.NextEligibleAt == nil || !st.NextEligibleAt.Equal(base.AddDate(0, 0, 60)) || st.LastPrompt == nil {
		t.Fatalf("status = %+v, %v", st, err)
	}
	clock.now = base.AddDate(0, 0, 60)
	if st, _ := svc.Status(context.Background(), 1); !st.Eligible || st.NextEligibleAt != nil {
		t.Fatalf("60日後は表示できる: %+v", st)
	}
}

func TestStatusAndPresentWithoutUsableFormAreNotEligible(t *testing.T) {
	var logs bytes.Buffer
	for _, formErr := range []error{userfeedback.ErrNotFound, userfeedback.ErrInvalidDefinition} {
		logs.Reset()
		repo := &fakeRepo{formErr: formErr, presentErr: formErr}
		svc := &userfeedback.Service{Repo: repo, Clock: &fakeClock{now: base}, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
		st, err := svc.Status(context.Background(), 1)
		if err != nil || st.Eligible || st.NextEligibleAt != nil {
			t.Fatalf("%v: status = %+v, %v", formErr, st, err)
		}
		p, err := svc.Present(context.Background(), 1, "11111111-1111-4111-8111-111111111111")
		if err != nil || p.Eligible {
			t.Fatalf("%v: present = %+v, %v", formErr, p, err)
		}
		if errors.Is(formErr, userfeedback.ErrInvalidDefinition) && !strings.Contains(logs.String(), "level=WARN") {
			t.Fatalf("壊れた定義は警告を残す: %s", logs.String())
		}
	}
}

func TestPresentCleansUpAtMostHourlyAndIgnoresCleanupFailure(t *testing.T) {
	repo := &fakeRepo{presentOut: userfeedback.Presentation{Eligible: true}, deleteErr: errors.New("timeout")}
	clock := &fakeClock{now: base}
	var logs bytes.Buffer
	svc := &userfeedback.Service{Repo: repo, Clock: clock, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	for _, step := range []time.Duration{0, 30 * time.Minute, 31 * time.Minute} {
		clock.now = clock.now.Add(step)
		p, err := svc.Present(context.Background(), 1, "11111111-1111-4111-8111-111111111111")
		if err != nil || !p.Eligible {
			t.Fatalf("削除の失敗は表示判定に影響しない: %+v, %v", p, err)
		}
	}
	if len(repo.deleteCalls) != 2 {
		t.Fatalf("1時間に1回まで: %d", len(repo.deleteCalls))
	}
	if !repo.deleteCalls[0].Equal(base.AddDate(-2, 0, 0)) {
		t.Fatalf("2年前より古いものを消す: %v", repo.deleteCalls[0])
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("削除の失敗は警告を残す: %s", logs.String())
	}
}

func TestStatusWithoutHistoryIsEligible(t *testing.T) {
	svc := &userfeedback.Service{Repo: &fakeRepo{form: validForm()}, Clock: &fakeClock{now: base}}
	st, err := svc.Status(context.Background(), 4)
	if err != nil || !st.Eligible || st.LastPrompt != nil || st.NextEligibleAt != nil {
		t.Fatalf("履歴が無いときは表示できる: %+v, %v", st, err)
	}
}

func TestStatusReturnsStoreErrors(t *testing.T) {
	dbErr := errors.New("db down")
	repo := &fakeRepo{formErr: dbErr}
	svc := &userfeedback.Service{Repo: repo, Clock: &fakeClock{now: base}}
	if _, err := svc.Status(context.Background(), 1); !errors.Is(err, dbErr) || repo.lastCalls != 0 {
		t.Fatalf("form err = %v lastCalls = %d", err, repo.lastCalls)
	}
	repo = &fakeRepo{form: validForm(), lastErr: dbErr}
	svc = &userfeedback.Service{Repo: repo, Clock: &fakeClock{now: base}}
	if _, err := svc.Status(context.Background(), 7); !errors.Is(err, dbErr) || repo.lastCalls != 1 {
		t.Fatalf("last err = %v lastCalls = %d", err, repo.lastCalls)
	}
}

func TestPresentReturnsStoreErrorWithoutCleanup(t *testing.T) {
	repo := &fakeRepo{presentErr: errors.New("db down"), presentOut: userfeedback.Presentation{Eligible: true}}
	svc := &userfeedback.Service{Repo: repo, Clock: &fakeClock{now: base}}
	p, err := svc.Present(context.Background(), 1, "11111111-1111-4111-8111-111111111111")
	if err == nil || p.Eligible || p.Form != nil || p.Prompt != nil || len(repo.deleteCalls) != 0 {
		t.Fatalf("present = %+v err=%v cleanups=%d", p, err, len(repo.deleteCalls))
	}
}

func TestServiceDefaultsClockAndLogger(t *testing.T) {
	repo := &fakeRepo{formErr: userfeedback.ErrInvalidDefinition}
	svc := &userfeedback.Service{Repo: repo}
	st, err := svc.Status(context.Background(), 1)
	if err != nil || st.Eligible {
		t.Fatalf("ロガーが無くても壊れた定義は表示しない: %+v, %v", st, err)
	}
	repo = &fakeRepo{form: validForm(), last: &userfeedback.PromptSummary{ShownAt: time.Unix(0, 0).UTC(), Status: userfeedback.PromptDismissed}}
	svc = &userfeedback.Service{Repo: repo}
	st, err = svc.Status(context.Background(), 1)
	if err != nil || !st.Eligible || st.NextEligibleAt != nil {
		t.Fatalf("時計が無いときは現在時刻で再表示を判定する: %+v, %v", st, err)
	}
}

func TestCompleteFollowupPassesClockAndAnswers(t *testing.T) {
	want := userfeedback.SubmitResult{SubmissionID: "sub", Status: userfeedback.SubmissionCompleted}
	answers := []userfeedback.Answer{{QuestionID: 11, Score: 1}, {QuestionID: 12, Score: 5}}
	repo := &fakeRepo{completeResult: want}
	svc := &userfeedback.Service{Repo: repo, Clock: &fakeClock{now: base}}
	got, err := svc.CompleteFollowup(context.Background(), 9, "sub", answers)
	if err != nil || got != want || repo.completeUser != 9 || repo.completeID != "sub" || !repo.completeAt.Equal(base) {
		t.Fatalf("got = %+v err=%v repo=%+v", got, err, repo)
	}
	if len(repo.completeAnswers) != 2 || repo.completeAnswers[0].Score != 1 || repo.completeAnswers[1].Score != 5 {
		t.Fatalf("answers = %+v", repo.completeAnswers)
	}
	repo.completeErr = errors.New("db down")
	if _, err := svc.CompleteFollowup(context.Background(), 9, "sub", answers); !errors.Is(err, repo.completeErr) {
		t.Fatalf("err = %v", err)
	}
}
