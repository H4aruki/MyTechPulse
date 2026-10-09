-- +goose Up
-- 初期版の質問（設計書 第5章）。質問を変えるときは、このファイルを書き換えず、
-- 新しい移行ファイルで「旧版をretiredへ → 新しい版をactiveでINSERT」の順に行う（第12章）。
INSERT INTO feedback_forms (form_key, version, title, status, cooldown_days)
VALUES ('service_satisfaction', 1, 'おすすめ記事についてのアンケート', 'active', 60);

INSERT INTO feedback_questions (form_id, question_key, question_text, sort_order, is_required)
SELECT id, 'overall', '今日のおすすめ記事は役に立ちましたか？', 1, true
FROM feedback_forms WHERE form_key = 'service_satisfaction' AND version = 1;

INSERT INTO feedback_questions (form_id, question_key, question_text, sort_order, is_required, display_if_question_id, display_if_score_max)
SELECT q.form_id, v.question_key, v.question_text, v.sort_order, true, q.id, 2
FROM feedback_questions q
JOIN feedback_forms f ON f.id = q.form_id
CROSS JOIN (VALUES
    ('interest_match', 'おすすめ記事は、あなたの興味に合っていましたか？', 2),
    ('freshness', 'おすすめ記事の新しさに満足しましたか？', 3),
    ('usability', '記事一覧画面は使いやすかったですか？', 4)
) AS v(question_key, question_text, sort_order)
WHERE f.form_key = 'service_satisfaction' AND f.version = 1 AND q.question_key = 'overall';

-- +goose Down
DELETE FROM feedback_questions WHERE form_id IN (SELECT id FROM feedback_forms WHERE form_key = 'service_satisfaction' AND version = 1);
DELETE FROM feedback_forms WHERE form_key = 'service_satisfaction' AND version = 1;
