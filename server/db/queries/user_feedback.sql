-- name: GetActiveFeedbackForm :one
SELECT id, form_key, version, title, cooldown_days FROM feedback_forms WHERE status = 'active';

-- name: GetFeedbackFormByID :one
SELECT id, form_key, version, title, cooldown_days FROM feedback_forms WHERE id = $1;

-- name: ListFeedbackQuestions :many
SELECT id, question_key, question_text, sort_order, is_required, display_if_question_id, display_if_score_max
FROM feedback_questions WHERE form_id = $1 ORDER BY sort_order;

-- name: LockFeedbackPrompt :one
SELECT id, form_id, "user_ID", status, stage, shown_at FROM feedback_prompts WHERE id = $1 FOR UPDATE;

-- 同じ利用者・同じフォームの表示判定を1つずつ行う（まだ表示記録が無い初回でも効く）
-- name: LockUserFeedbackForm :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));

-- 版をまたいで、同じフォームの直近の表示を返す
-- name: LastFeedbackPrompt :one
SELECT p.id, p.status, p.stage, p.shown_at, s.id AS submission_id, s.status AS submission_status
FROM feedback_prompts p
JOIN feedback_forms f ON f.id = p.form_id
LEFT JOIN feedback_submissions s ON s.prompt_id = p.id
WHERE p."user_ID" = $1 AND f.form_key = $2
ORDER BY p.shown_at DESC, p.id DESC
LIMIT 1;

-- name: InsertFeedbackPrompt :execrows
INSERT INTO feedback_prompts (id, form_id, "user_ID", shown_at) VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING;

-- name: GetFeedbackSubmissionByPrompt :one
SELECT id, status FROM feedback_submissions WHERE prompt_id = $1;

-- name: ListFeedbackSnapshotSource :many
SELECT r."tag_ID", t.tag_name, r.match_int
FROM recommend r JOIN tag t ON t."tag_ID" = r."tag_ID"
WHERE r."user_ID" = $1
ORDER BY r.match_int DESC, r."tag_ID" ASC
LIMIT 5;

-- name: InsertFeedbackSubmission :one
INSERT INTO feedback_submissions (form_id, "user_ID", prompt_id, status, started_at, completed_at)
VALUES ($1, $2, $3, $4, $5, sqlc.narg(completed_at))
RETURNING id;

-- name: InsertFeedbackAnswer :exec
INSERT INTO feedback_answers (submission_id, form_id, question_id, score) VALUES ($1, $2, $3, $4);

-- name: InsertFeedbackSnapshot :exec
INSERT INTO feedback_interest_snapshots (submission_id, rank, "tag_ID", tag_name, match_int)
VALUES ($1, $2, $3, $4, $5);

-- name: MarkFeedbackPromptFollowup :exec
UPDATE feedback_prompts SET stage = 'followup' WHERE id = $1 AND status = 'shown';

-- name: MarkFeedbackPromptSubmitted :exec
UPDATE feedback_prompts SET status = 'submitted', finished_at = $2 WHERE id = $1 AND status = 'shown';

-- name: MarkFeedbackPromptDismissed :exec
UPDATE feedback_prompts SET status = 'dismissed', finished_at = $2 WHERE id = $1 AND status = 'shown';

-- name: GetFeedbackSubmissionOwner :one
SELECT prompt_id, "user_ID" FROM feedback_submissions WHERE id = $1;

-- name: LockFeedbackSubmission :one
SELECT id, form_id, "user_ID", prompt_id, status FROM feedback_submissions WHERE id = $1 FOR UPDATE;

-- name: GetFeedbackAnswerScore :one
SELECT score FROM feedback_answers WHERE submission_id = $1 AND question_id = $2;

-- name: CompleteFeedbackSubmission :exec
UPDATE feedback_submissions SET status = 'completed', completed_at = $2 WHERE id = $1 AND status = 'partial';

-- 保持期限（終了日時、未終了なら表示日時）の古い順に、上限件数まで消す。回答などは連鎖して消える
-- name: DeleteExpiredFeedbackPrompts :execrows
DELETE FROM feedback_prompts AS target WHERE target.id IN (
    SELECT candidate.id FROM feedback_prompts AS candidate
    WHERE COALESCE(candidate.finished_at, candidate.shown_at) < sqlc.arg(before)
    ORDER BY COALESCE(candidate.finished_at, candidate.shown_at)
    LIMIT sqlc.arg(batch_size)
);
