-- +goose Up
-- 利用者フィードバック（設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md 第9章）。
-- 既存の表（user、tag、recommend、auth_session）は変更しない。

CREATE TABLE feedback_forms (
    id bigserial PRIMARY KEY,
    form_key text NOT NULL CHECK (form_key ~ '^[a-z][a-z0-9_]{0,49}$'),
    version integer NOT NULL CHECK (version >= 1),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    status text NOT NULL CHECK (status IN ('draft', 'active', 'retired')),
    cooldown_days integer NOT NULL CHECK (cooldown_days >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (form_key, version)
);
-- 有効なフォームは常に1つ以下
CREATE UNIQUE INDEX feedback_forms_one_active_idx ON feedback_forms (status) WHERE status = 'active';

CREATE TABLE feedback_questions (
    id bigserial PRIMARY KEY,
    form_id bigint NOT NULL REFERENCES feedback_forms(id),
    question_key text NOT NULL CHECK (question_key ~ '^[a-z][a-z0-9_]{0,49}$'),
    question_text text NOT NULL CHECK (char_length(question_text) BETWEEN 1 AND 200),
    sort_order integer NOT NULL CHECK (sort_order >= 1),
    is_required boolean NOT NULL DEFAULT true,
    display_if_question_id bigint,
    display_if_score_max integer,
    UNIQUE (form_id, question_key),
    UNIQUE (form_id, sort_order),
    UNIQUE (form_id, id),
    -- 表示条件は同じフォームの質問だけを参照する
    FOREIGN KEY (form_id, display_if_question_id) REFERENCES feedback_questions(form_id, id),
    CHECK (
        (display_if_question_id IS NULL AND display_if_score_max IS NULL)
        OR (display_if_question_id IS NOT NULL AND display_if_score_max = 2)
    ),
    CHECK (display_if_question_id IS NULL OR display_if_question_id <> id)
);

CREATE TABLE feedback_prompts (
    id uuid PRIMARY KEY,
    form_id bigint NOT NULL REFERENCES feedback_forms(id),
    "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'shown' CHECK (status IN ('shown', 'dismissed', 'submitted')),
    stage text NOT NULL DEFAULT 'overall' CHECK (stage IN ('overall', 'followup')),
    shown_at timestamptz NOT NULL,
    finished_at timestamptz,
    CHECK ((status = 'shown') = (finished_at IS NULL)),
    UNIQUE (id, "user_ID"),
    UNIQUE (id, form_id)
);
CREATE INDEX feedback_prompts_user_shown_idx ON feedback_prompts ("user_ID", shown_at DESC);
CREATE INDEX feedback_prompts_retention_idx ON feedback_prompts ((COALESCE(finished_at, shown_at)));

CREATE TABLE feedback_submissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    form_id bigint NOT NULL REFERENCES feedback_forms(id),
    "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
    prompt_id uuid NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('partial', 'completed')),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    -- 回答は表示した本人・表示した版に限る。表示記録を消すと回答も消える
    FOREIGN KEY (prompt_id, "user_ID") REFERENCES feedback_prompts(id, "user_ID") ON DELETE CASCADE,
    FOREIGN KEY (prompt_id, form_id) REFERENCES feedback_prompts(id, form_id) ON DELETE CASCADE,
    UNIQUE (id, form_id)
);

CREATE TABLE feedback_answers (
    submission_id uuid NOT NULL,
    form_id bigint NOT NULL,
    question_id bigint NOT NULL,
    score integer NOT NULL CHECK (score BETWEEN 1 AND 5),
    PRIMARY KEY (submission_id, question_id),
    FOREIGN KEY (submission_id, form_id) REFERENCES feedback_submissions(id, form_id) ON DELETE CASCADE,
    -- 質問は回答と同じ版のものに限る
    FOREIGN KEY (form_id, question_id) REFERENCES feedback_questions(form_id, id)
);

CREATE TABLE feedback_interest_snapshots (
    submission_id uuid NOT NULL REFERENCES feedback_submissions(id) ON DELETE CASCADE,
    rank integer NOT NULL CHECK (rank BETWEEN 1 AND 5),
    "tag_ID" integer REFERENCES tag("tag_ID") ON DELETE SET NULL,
    tag_name varchar(50) NOT NULL,
    match_int integer NOT NULL,
    PRIMARY KEY (submission_id, rank)
);

-- +goose Down
-- 開発環境で作り直すときだけ使う。本番の切り戻しには使わない。
DROP TABLE feedback_interest_snapshots;
DROP TABLE feedback_answers;
DROP TABLE feedback_submissions;
DROP TABLE feedback_prompts;
DROP TABLE feedback_questions;
DROP TABLE feedback_forms;
