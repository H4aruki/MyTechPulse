package userfeedback

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"
)

const (
	RetentionYears         = 2
	CleanupInterval        = time.Hour
	CleanupBatchSize int32 = 500
	cleanupTimeout         = 3 * time.Second
)

type Repository interface {
	ActiveForm(context.Context) (Form, error)
	LastPrompt(context.Context, int64, string) (*PromptSummary, error)
	Present(context.Context, int64, string, time.Time) (Presentation, error)
	SubmitOverall(context.Context, int64, string, int64, int32, time.Time) (SubmitResult, error)
	CompleteFollowup(context.Context, int64, string, []Answer, time.Time) (SubmitResult, error)
	Dismiss(context.Context, int64, string, time.Time) (PromptSummary, error)
	DeleteExpired(context.Context, time.Time, int32) (int64, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	Repo        Repository
	Clock       Clock
	Logger      *slog.Logger
	mu          sync.Mutex
	lastCleanup time.Time
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}
func (s *Service) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s.Logger
}
func (s *Service) unusableForm(err error) bool {
	if errors.Is(err, ErrInvalidDefinition) {
		s.logger().Warn("user feedback form definition is invalid")
		return true
	}
	return errors.Is(err, ErrNotFound)
}

func (s *Service) Status(ctx context.Context, userID int64) (Status, error) {
	form, err := s.Repo.ActiveForm(ctx)
	if s.unusableForm(err) {
		return Status{Eligible: false}, nil
	}
	if err != nil {
		return Status{}, err
	}
	last, err := s.Repo.LastPrompt(ctx, userID, form.Key)
	if err != nil {
		return Status{}, err
	}
	st := Status{Eligible: true, LastPrompt: last}
	if last != nil {
		next := NextEligibleAt(last.ShownAt, form.CooldownDays)
		if s.now().Before(next) {
			st.Eligible, st.NextEligibleAt = false, &next
		}
	}
	return st, nil
}

func (s *Service) Present(ctx context.Context, userID int64, promptID string) (Presentation, error) {
	now := s.now()
	out, err := s.Repo.Present(ctx, userID, promptID, now)
	if s.unusableForm(err) {
		out, err = Presentation{Eligible: false}, nil
	}
	if err != nil {
		return Presentation{}, err
	}
	s.cleanupIfDue(ctx, now)
	return out, nil
}

func (s *Service) cleanupIfDue(ctx context.Context, now time.Time) {
	s.mu.Lock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < CleanupInterval {
		s.mu.Unlock()
		return
	}
	s.lastCleanup = now
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if _, err := s.Repo.DeleteExpired(ctx, now.AddDate(-RetentionYears, 0, 0), CleanupBatchSize); err != nil {
		s.logger().Warn("user feedback cleanup failed")
	}
}

func (s *Service) SubmitOverall(ctx context.Context, userID int64, promptID string, questionID int64, score int32) (SubmitResult, error) {
	return s.Repo.SubmitOverall(ctx, userID, promptID, questionID, score, s.now())
}
func (s *Service) CompleteFollowup(ctx context.Context, userID int64, submissionID string, answers []Answer) (SubmitResult, error) {
	return s.Repo.CompleteFollowup(ctx, userID, submissionID, answers, s.now())
}
func (s *Service) Dismiss(ctx context.Context, userID int64, promptID string) (PromptSummary, error) {
	return s.Repo.Dismiss(ctx, userID, promptID, s.now())
}
