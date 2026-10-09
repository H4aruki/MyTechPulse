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

var _ userfeedback.Repository = (*Feedback)(nil)

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

func lockOwnPrompt(ctx context.Context, q *dbgen.Queries, uid int32, pid pgtype.UUID) (dbgen.LockFeedbackPromptRow, error) {
	p, err := q.LockFeedbackPrompt(ctx, pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, userfeedback.ErrNotFound
	}
	if err != nil {
		return p, errFeedback
	}
	if p.UserID != uid {
		return p, userfeedback.ErrNotFound
	}
	return p, nil
}

func (r *Feedback) SubmitOverall(ctx context.Context, userID int64, promptID string, questionID int64, score int32, now time.Time) (userfeedback.SubmitResult, error) {
	uid, err := feedbackUserID(userID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	pid, err := parseUUID(promptID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	p, err := lockOwnPrompt(ctx, q, uid, pid)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	existing, err := q.GetFeedbackSubmissionByPrompt(ctx, pid)
	if err == nil {
		st := userfeedback.SubmissionStatus(existing.Status)
		return userfeedback.SubmitResult{SubmissionID: uuidText(existing.ID), Status: st, FollowupRequired: st == userfeedback.SubmissionPartial && p.Status == string(userfeedback.PromptShown)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if p.Status != string(userfeedback.PromptShown) {
		return userfeedback.SubmitResult{}, userfeedback.ErrConflict
	}
	form, err := r.formByID(ctx, q, p.FormID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	followup, err := userfeedback.ValidateOverall(form, questionID, score)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	status, completedAt := userfeedback.SubmissionCompleted, ts(now)
	if followup {
		status, completedAt = userfeedback.SubmissionPartial, pgtype.Timestamptz{}
	}
	sid, err := q.InsertFeedbackSubmission(ctx, dbgen.InsertFeedbackSubmissionParams{FormID: p.FormID, UserID: uid, PromptID: pid, Status: string(status), StartedAt: ts(now), CompletedAt: completedAt})
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if err := q.InsertFeedbackAnswer(ctx, dbgen.InsertFeedbackAnswerParams{SubmissionID: sid, FormID: p.FormID, QuestionID: questionID, Score: score}); err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	weights, err := q.ListFeedbackSnapshotSource(ctx, uid)
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	for i, w := range weights {
		if err := q.InsertFeedbackSnapshot(ctx, dbgen.InsertFeedbackSnapshotParams{SubmissionID: sid, Rank: int32(i + 1), TagID: pgtype.Int4{Int32: w.TagID, Valid: true}, TagName: w.TagName, MatchInt: w.MatchInt}); err != nil {
			return userfeedback.SubmitResult{}, errFeedback
		}
	}
	if followup {
		err = q.MarkFeedbackPromptFollowup(ctx, pid)
	} else {
		err = q.MarkFeedbackPromptSubmitted(ctx, dbgen.MarkFeedbackPromptSubmittedParams{ID: pid, FinishedAt: ts(now)})
	}
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	return userfeedback.SubmitResult{SubmissionID: uuidText(sid), Status: status, FollowupRequired: followup}, nil
}

func (r *Feedback) CompleteFollowup(ctx context.Context, userID int64, submissionID string, answers []userfeedback.Answer, now time.Time) (userfeedback.SubmitResult, error) {
	uid, err := feedbackUserID(userID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	sid, err := parseUUID(submissionID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	owner, err := q.GetFeedbackSubmissionOwner(ctx, sid)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner.UserID != uid) {
		return userfeedback.SubmitResult{}, userfeedback.ErrNotFound
	}
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	p, err := lockOwnPrompt(ctx, q, uid, owner.PromptID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	sub, err := q.LockFeedbackSubmission(ctx, sid)
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if sub.Status == string(userfeedback.SubmissionCompleted) {
		return userfeedback.SubmitResult{SubmissionID: uuidText(sid), Status: userfeedback.SubmissionCompleted}, nil
	}
	if p.Status != string(userfeedback.PromptShown) {
		return userfeedback.SubmitResult{}, userfeedback.ErrConflict
	}
	form, err := r.formByID(ctx, q, sub.FormID)
	if err != nil {
		return userfeedback.SubmitResult{}, err
	}
	overall, err := q.GetFeedbackAnswerScore(ctx, dbgen.GetFeedbackAnswerScoreParams{SubmissionID: sid, QuestionID: form.Root().ID})
	if err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if err := userfeedback.ValidateFollowup(form, overall, answers); err != nil {
		return userfeedback.SubmitResult{}, err
	}
	for _, a := range answers {
		if err := q.InsertFeedbackAnswer(ctx, dbgen.InsertFeedbackAnswerParams{SubmissionID: sid, FormID: sub.FormID, QuestionID: a.QuestionID, Score: a.Score}); err != nil {
			return userfeedback.SubmitResult{}, errFeedback
		}
	}
	if err := q.CompleteFeedbackSubmission(ctx, dbgen.CompleteFeedbackSubmissionParams{ID: sid, CompletedAt: ts(now)}); err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if err := q.MarkFeedbackPromptSubmitted(ctx, dbgen.MarkFeedbackPromptSubmittedParams{ID: owner.PromptID, FinishedAt: ts(now)}); err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return userfeedback.SubmitResult{}, errFeedback
	}
	return userfeedback.SubmitResult{SubmissionID: uuidText(sid), Status: userfeedback.SubmissionCompleted}, nil
}

func (r *Feedback) Dismiss(ctx context.Context, userID int64, promptID string, now time.Time) (userfeedback.PromptSummary, error) {
	uid, err := feedbackUserID(userID)
	if err != nil {
		return userfeedback.PromptSummary{}, err
	}
	pid, err := parseUUID(promptID)
	if err != nil {
		return userfeedback.PromptSummary{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return userfeedback.PromptSummary{}, errFeedback
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	p, err := lockOwnPrompt(ctx, q, uid, pid)
	if err != nil {
		return userfeedback.PromptSummary{}, err
	}
	out := userfeedback.PromptSummary{ID: uuidText(pid), Status: userfeedback.PromptDismissed, Stage: userfeedback.Stage(p.Stage), ShownAt: p.ShownAt.Time}
	switch userfeedback.PromptStatus(p.Status) {
	case userfeedback.PromptDismissed:
		return out, nil
	case userfeedback.PromptSubmitted:
		return userfeedback.PromptSummary{}, userfeedback.ErrConflict
	}
	if err := q.MarkFeedbackPromptDismissed(ctx, dbgen.MarkFeedbackPromptDismissedParams{ID: pid, FinishedAt: ts(now)}); err != nil {
		return userfeedback.PromptSummary{}, errFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return userfeedback.PromptSummary{}, errFeedback
	}
	return out, nil
}

func (r *Feedback) DeleteExpired(ctx context.Context, before time.Time, limit int32) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, errFeedback
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '2s'`); err != nil {
		return 0, errFeedback
	}
	n, err := r.q.WithTx(tx).DeleteExpiredFeedbackPrompts(ctx, dbgen.DeleteExpiredFeedbackPromptsParams{Before: ts(before), BatchSize: limit})
	if err != nil {
		return 0, errFeedback
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, errFeedback
	}
	return n, nil
}
