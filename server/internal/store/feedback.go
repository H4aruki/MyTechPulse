package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

type Feedback struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

var errFeedback = errors.New("store: 利用者フィードバックを処理できません")

func NewFeedback(pool *pgxpool.Pool) *Feedback { return &Feedback{pool: pool, q: dbgen.New(pool)} }

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil || !u.Valid {
		return pgtype.UUID{}, fmt.Errorf("%w: IDの形式が正しくありません", userfeedback.ErrInvalidInput)
	}
	return u, nil
}

func uuidText(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func feedbackUserID(userID int64) (int32, error) {
	id, err := userID32(userID)
	if err != nil {
		return 0, userfeedback.ErrNotFound
	}
	return id, nil
}

func (r *Feedback) loadForm(ctx context.Context, q *dbgen.Queries, id int64, key string, version int32, title string, cooldown int32) (userfeedback.Form, error) {
	rows, err := q.ListFeedbackQuestions(ctx, id)
	if err != nil {
		return userfeedback.Form{}, errFeedback
	}
	f := userfeedback.Form{ID: id, Key: key, Version: version, Title: title, CooldownDays: cooldown}
	for _, row := range rows {
		question := userfeedback.Question{ID: row.ID, Key: row.QuestionKey, Text: row.QuestionText, SortOrder: row.SortOrder, Required: row.IsRequired}
		if row.DisplayIfQuestionID.Valid {
			v := row.DisplayIfQuestionID.Int64
			question.DisplayIfQuestionID = &v
		}
		if row.DisplayIfScoreMax.Valid {
			v := row.DisplayIfScoreMax.Int32
			question.DisplayIfScoreMax = &v
		}
		f.Questions = append(f.Questions, question)
	}
	if err := userfeedback.ValidateDefinition(f); err != nil {
		return userfeedback.Form{}, err
	}
	return f, nil
}

func (r *Feedback) formByID(ctx context.Context, q *dbgen.Queries, id int64) (userfeedback.Form, error) {
	row, err := q.GetFeedbackFormByID(ctx, id)
	if err != nil {
		return userfeedback.Form{}, errFeedback
	}
	return r.loadForm(ctx, q, row.ID, row.FormKey, row.Version, row.Title, row.CooldownDays)
}

func (r *Feedback) activeForm(ctx context.Context, q *dbgen.Queries) (userfeedback.Form, error) {
	row, err := q.GetActiveFeedbackForm(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return userfeedback.Form{}, userfeedback.ErrNotFound
	}
	if err != nil {
		return userfeedback.Form{}, errFeedback
	}
	return r.loadForm(ctx, q, row.ID, row.FormKey, row.Version, row.Title, row.CooldownDays)
}

func (r *Feedback) ActiveForm(ctx context.Context) (userfeedback.Form, error) {
	return r.activeForm(ctx, r.q)
}

func (r *Feedback) LastPrompt(ctx context.Context, userID int64, formKey string) (*userfeedback.PromptSummary, error) {
	uid, err := feedbackUserID(userID)
	if err != nil {
		return nil, err
	}
	row, err := r.q.LastFeedbackPrompt(ctx, dbgen.LastFeedbackPromptParams{UserID: uid, FormKey: formKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errFeedback
	}
	s := userfeedback.PromptSummary{ID: uuidText(row.ID), Status: userfeedback.PromptStatus(row.Status), Stage: userfeedback.Stage(row.Stage), ShownAt: row.ShownAt.Time}
	if row.SubmissionID.Valid {
		id := uuidText(row.SubmissionID)
		st := userfeedback.SubmissionStatus(row.SubmissionStatus.String)
		s.SubmissionID, s.SubmissionStatus = &id, &st
	}
	return &s, nil
}

func (r *Feedback) existingPresentation(ctx context.Context, q *dbgen.Queries, uid int32, pid pgtype.UUID) (userfeedback.Presentation, bool, error) {
	p, err := q.LockFeedbackPrompt(ctx, pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return userfeedback.Presentation{}, false, nil
	}
	if err != nil {
		return userfeedback.Presentation{}, false, errFeedback
	}
	if p.UserID != uid {
		return userfeedback.Presentation{}, false, userfeedback.ErrNotFound
	}
	s := userfeedback.PromptSummary{ID: uuidText(p.ID), Status: userfeedback.PromptStatus(p.Status), Stage: userfeedback.Stage(p.Stage), ShownAt: p.ShownAt.Time}
	sub, err := q.GetFeedbackSubmissionByPrompt(ctx, pid)
	if err == nil {
		id := uuidText(sub.ID)
		st := userfeedback.SubmissionStatus(sub.Status)
		s.SubmissionID, s.SubmissionStatus = &id, &st
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return userfeedback.Presentation{}, false, errFeedback
	}
	out := userfeedback.Presentation{Eligible: true, Prompt: &s}
	if s.Status == userfeedback.PromptShown {
		form, err := r.formByID(ctx, q, p.FormID)
		if err != nil {
			return userfeedback.Presentation{}, false, err
		}
		out.Form = &form
	}
	return out, true, nil
}

func (r *Feedback) Present(ctx context.Context, userID int64, promptID string, now time.Time) (userfeedback.Presentation, error) {
	uid, err := feedbackUserID(userID)
	if err != nil {
		return userfeedback.Presentation{}, err
	}
	pid, err := parseUUID(promptID)
	if err != nil {
		return userfeedback.Presentation{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return userfeedback.Presentation{}, errFeedback
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if out, ok, err := r.existingPresentation(ctx, q, uid, pid); err != nil || ok {
		return out, err
	}
	form, err := r.activeForm(ctx, q)
	if errors.Is(err, userfeedback.ErrNotFound) {
		return userfeedback.Presentation{Eligible: false}, nil
	}
	if err != nil {
		return userfeedback.Presentation{}, err
	}
	if err := q.LockUserFeedbackForm(ctx, fmt.Sprintf("user-feedback:%s:%d", form.Key, uid)); err != nil {
		return userfeedback.Presentation{}, errFeedback
	}
	if out, ok, err := r.existingPresentation(ctx, q, uid, pid); err != nil || ok {
		return out, err
	}
	last, err := q.LastFeedbackPrompt(ctx, dbgen.LastFeedbackPromptParams{UserID: uid, FormKey: form.Key})
	switch {
	case err == nil:
		next := userfeedback.NextEligibleAt(last.ShownAt.Time, form.CooldownDays)
		if now.Before(next) {
			return userfeedback.Presentation{Eligible: false, NextEligibleAt: &next}, nil
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return userfeedback.Presentation{}, errFeedback
	}
	n, err := q.InsertFeedbackPrompt(ctx, dbgen.InsertFeedbackPromptParams{ID: pid, FormID: form.ID, UserID: uid, ShownAt: ts(now)})
	if err != nil {
		return userfeedback.Presentation{}, errFeedback
	}
	if n == 0 {
		return userfeedback.Presentation{}, userfeedback.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return userfeedback.Presentation{}, errFeedback
	}
	return userfeedback.Presentation{Eligible: true, Prompt: &userfeedback.PromptSummary{ID: uuidText(pid), Status: userfeedback.PromptShown, Stage: userfeedback.StageOverall, ShownAt: now}, Form: &form}, nil
}
