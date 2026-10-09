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
	form         userfeedback.Form
	formErr      error
	last         *userfeedback.PromptSummary
	presentOut   userfeedback.Presentation
	presentErr   error
	deleteCalls  []time.Time
	deleteErr    error
	submitResult userfeedback.SubmitResult
}

func (r *fakeRepo) ActiveForm(context.Context) (userfeedback.Form, error) { return r.form, r.formErr }
func (r *fakeRepo) LastPrompt(context.Context, int64, string) (*userfeedback.PromptSummary, error) {
	return r.last, nil
}
func (r *fakeRepo) Present(context.Context, int64, string, time.Time) (userfeedback.Presentation, error) {
	return r.presentOut, r.presentErr
}
func (r *fakeRepo) SubmitOverall(context.Context, int64, string, int64, int32, time.Time) (userfeedback.SubmitResult, error) {
	return r.submitResult, nil
}
func (r *fakeRepo) CompleteFollowup(context.Context, int64, string, []userfeedback.Answer, time.Time) (userfeedback.SubmitResult, error) {
	return r.submitResult, nil
}
func (r *fakeRepo) Dismiss(context.Context, int64, string, time.Time) (userfeedback.PromptSummary, error) {
	return userfeedback.PromptSummary{Status: userfeedback.PromptDismissed}, nil
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
