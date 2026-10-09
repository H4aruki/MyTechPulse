# 利用者フィードバック（アンケート）機能 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 記事を3件開いた利用者へ、1問の総合評価と、低評価のときだけの追加3問を出すポップアップを作り、回答・表示履歴・回答時の興味傾向を保存する。

**Architecture:** バックエンドは機能パッケージ `server/internal/userfeedback/`（判断ロジック・サービス・API窓口）と、`server/internal/store/feedback.go`（sqlcで生成した問い合わせを1つのトランザクションにまとめる層）に分ける。質問定義はgooseの移行ファイルに直接書く。フロントエンドは、記事の開封数と次回表示可能日時を持つ小さなモジュール、表示判定を行うフック、ポップアップ部品の3つに分け、記事一覧画面へ組み込む。

**Tech Stack:** Go 1.26 / huma v2 / pgx v5 / sqlc / goose / PostgreSQL 17、React 19 / TypeScript / TanStack Query / Vitest / Testing Library / openapi-typescript

**Spec:** `docs/superpowers/specs/2026-09-10-user-feedback-design.md`（2026-10-09改訂版）。実装者は設計書と本計画の両方を読む。

**関連Issue:** #84

## Global Constraints

- DBの変更は追加だけ（`server/db/migrations/additive_test.go` が検査）。既存の移行ファイルは書き換えない。
- 新しいライブラリを追加しない（Go・npmとも）。UUIDは `pgtype.UUID`、画面側は `crypto.randomUUID()` を使う。
- API は `/api/v1/user-feedback/` 配下。保護APIとして認証セッションのCookieを要求し、POST/PUT は既存のCSRF対策（Origin と `X-MTP-CSRF`）を通す。エラーは Problem Details。
- 利用者IDは認証セッションから取り、リクエスト本文で受け取らない。他人の `prompt_id`・`submission_id` は 404。
- HTTPステータス: 未ログイン401 / 他人・存在しない404 / 入力検証422 / 状態の競合409 / 想定外500。
- 評価値は1〜5。追加質問のしきい値は `display_if_score_max = 2` 固定。表示条件は異なる記事3件（フロント固定）。再表示間隔はフォームの `cooldown_days`（初期版60日）。
- 興味傾向スナップショットは `recommend.match_int` を変換せず、高い順・同値は `tag_ID` 昇順で最大5件。
- 保持期間2年。削除は表示判定の後に別トランザクションで、最大500件・`statement_timeout = '2s'`・プロセスごとに1時間に1回まで。
- クリック学習の確定を待つのは最大10秒。
- API を変えたら `cd server && go run ./cmd/openapi` と `cd frontend && npm run api:generate` を実行し、生成物をコミットする（CIが差分を検査）。
- 画面の文言は平易な日本語。ポップアップ内に「回答はあなたのアカウントに紐づけて保存し、サービス改善の分析に使います。」を表示する。
- コミットは Conventional Commits、本文に「なぜ」を書く。末尾に `Refs #84`。

## Review Focus

- **同じ利用者が2つのタブで同時に3件目を開く** → ポップアップは片方だけ。Task 4 の同時実行テストで固定する。
- **ブラウザの保存領域が使えない（プライベートモード等で `sessionStorage` が例外を出す）** → 記事閲覧は壊れず、開封数はメモリで数え続ける。Task 8 のテストで固定する。
- **同じブラウザで別の利用者がログインし直す** → 前の利用者の開封履歴・表示停止日時を引き継がない。Task 8・Task 10 のテストで固定する。
- **クリック学習の通信が返ってこない** → 10秒で待つのをやめて表示判定へ進む。Task 8 のテストで固定する。
- **有効なフォームが無い・質問定義が壊れている** → ポップアップを出さず、記事一覧はそのまま使える。警告ログを残す。Task 5 のテストで固定する。

## ファイル構成

| ファイル | 役割 |
| --- | --- |
| `server/db/migrations/00004_user_feedback.sql` | 6つの新しい表と制約・索引 |
| `server/db/migrations/00005_user_feedback_initial_form.sql` | 初期版の質問（総合評価1問＋追加3問） |
| `server/db/migrations/user_feedback_test.go` | DB制約の試験 |
| `server/internal/userfeedback/model.go` | 型・エラー・定数 |
| `server/internal/userfeedback/definition.go` | 質問定義の検証、回答の検証、次回表示日時の計算（DBなしの純粋な処理） |
| `server/internal/userfeedback/service.go` | 表示可否の判定、期限切れ削除の間引き、リポジトリの呼び出し |
| `server/internal/userfeedback/handler.go` | huma の窓口5つ |
| `server/db/queries/user_feedback.sql` | sqlc の問い合わせ |
| `server/internal/store/feedback.go` | トランザクションの組み立て |
| `server/internal/store/feedback_integration_test.go` | DBを使う結合試験 |
| `server/internal/app/app.go`、`server/cmd/api/main.go`、`server/cmd/openapi/main.go` | 配線 |
| `frontend/src/api/client.ts`・`endpoints.ts`・`types.ts`・`generated-contract.ts` | PUT対応と呼び出し関数 |
| `frontend/src/lib/feedbackTracker.ts` | 開封履歴・表示停止日時・待ち合わせ |
| `frontend/src/lib/useUserFeedback.ts` | 表示判定の流れ |
| `frontend/src/components/FeedbackDialog.tsx` | ポップアップ |
| `frontend/src/pages/ArticlesPage.tsx`・`LoginPage.tsx`・`SignupPage.tsx` | 組み込みと状態の消去 |

## 実行の分担（並行作業する場合）

- Task 1〜6（サーバー側）は直列。Codex worker 1つに `server/` だけを任せられる。
- Task 7 は Task 6 の `openapi.json` が必要。
- Task 8 は API に依存しないので、Task 1 と並行して始められる。
- Task 9・10 は Task 7 の後。Task 11（資料）は最後。

---

### Task 1: DBの表と初期の質問

**Files:**
- Create: `server/db/migrations/00004_user_feedback.sql`
- Create: `server/db/migrations/00005_user_feedback_initial_form.sql`
- Test: `server/db/migrations/user_feedback_test.go`

**Interfaces:**
- Produces: 表 `feedback_forms`、`feedback_questions`、`feedback_prompts`、`feedback_submissions`、`feedback_answers`、`feedback_interest_snapshots`。初期フォーム `form_key = 'service_satisfaction'`、`version = 1`、`status = 'active'`、`cooldown_days = 60`、質問キー `overall`（sort 1）・`interest_match`（2）・`freshness`（3）・`usability`（4）。

- [ ] **Step 1: 失敗する試験を書く**

`server/db/migrations/user_feedback_test.go`:

```go
package migrations_test

import (
	"context"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

func TestUserFeedbackInitialFormIsSeeded(t *testing.T) {
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM feedback_forms WHERE status='active' AND form_key='service_satisfaction' AND version=1 AND cooldown_days=60`); n != 1 {
		t.Fatalf("active form = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM feedback_questions WHERE display_if_question_id IS NULL`); n != 1 {
		t.Fatalf("root questions = %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM feedback_questions c JOIN feedback_questions r ON r.id = c.display_if_question_id
		WHERE r.question_key='overall' AND c.display_if_score_max=2 AND c.question_key IN ('interest_match','freshness','usability')`); n != 3 {
		t.Fatalf("followups = %d", n)
	}
}

func TestUserFeedbackConstraintsRejectInvalidRows(t *testing.T) {
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO feedback_forms(form_key, version, title, status, cooldown_days) VALUES ('other', 1, 'その他', 'draft', 60)`)
	cases := map[string]string{
		"2つ目のactive": `INSERT INTO feedback_forms(form_key, version, title, status, cooldown_days) VALUES ('service_satisfaction', 2, 't', 'active', 60)`,
		"しきい値が2以外": `INSERT INTO feedback_questions(form_id, question_key, question_text, sort_order, is_required, display_if_question_id, display_if_score_max)
			SELECT form_id, 'bad', 'q', 9, true, id, 3 FROM feedback_questions WHERE question_key='overall'`,
		"しきい値だけある": `INSERT INTO feedback_questions(form_id, question_key, question_text, sort_order, is_required, display_if_score_max)
			SELECT form_id, 'bad', 'q', 9, true, 2 FROM feedback_questions WHERE question_key='overall'`,
		"別フォームの質問を参照": `INSERT INTO feedback_questions(form_id, question_key, question_text, sort_order, is_required, display_if_question_id, display_if_score_max)
			SELECT f.id, 'bad', 'q', 1, true, q.id, 2 FROM feedback_forms f, feedback_questions q WHERE f.form_key='other' AND q.question_key='overall'`,
		"並び順の重複": `INSERT INTO feedback_questions(form_id, question_key, question_text, sort_order, is_required)
			SELECT form_id, 'dup', 'q', 1, true FROM feedback_questions WHERE question_key='overall'`,
		"状態の値が不正": `INSERT INTO feedback_forms(form_key, version, title, status, cooldown_days) VALUES ('x', 1, 't', 'open', 60)`,
	}
	for name, query := range cases {
		if _, err := db.Exec(query); err == nil {
			t.Errorf("%s: 拒否されるべき", name)
		}
	}
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('u', 'h')`)
	mustExec(t, db, `INSERT INTO feedback_prompts(id, form_id, "user_ID", shown_at)
		SELECT '11111111-1111-4111-8111-111111111111', id, 1, now() FROM feedback_forms WHERE status='active'`)
	if _, err := db.Exec(`UPDATE feedback_prompts SET status='dismissed'`); err == nil {
		t.Error("finished_atなしの最終状態は拒否されるべき")
	}
	if _, err := db.Exec(`INSERT INTO feedback_submissions(form_id, "user_ID", prompt_id, status, started_at)
		SELECT form_id, 1, id, 'completed', now() FROM feedback_prompts`); err == nil {
		t.Error("completed_atなしの完了は拒否されるべき")
	}
}
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd server && go test ./db/migrations/ -run UserFeedback -v`
Expected: `TEST_DATABASE_URL` があれば `relation "feedback_forms" does not exist` で FAIL。無ければ SKIP（その場合は `docker compose up -d db` で起動し、`TEST_DATABASE_URL=postgres://...` を設定して実行する。値は利用者に確認する）。

- [ ] **Step 3: 表を作る移行ファイルを書く**

`server/db/migrations/00004_user_feedback.sql`:

```sql
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
```

- [ ] **Step 4: 初期の質問を登録する移行ファイルを書く**

`server/db/migrations/00005_user_feedback_initial_form.sql`:

```sql
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
```

- [ ] **Step 5: 試験が通ることを確認する**

Run: `cd server && go test ./db/migrations/ -v`
Expected: `TestUserFeedback*` を含めて PASS（追加だけの検査 `TestMigrationsAreAdditive` 系も PASS）。

- [ ] **Step 6: コミット**

```bash
git add server/db/migrations/00004_user_feedback.sql server/db/migrations/00005_user_feedback_initial_form.sql server/db/migrations/user_feedback_test.go
git commit -m "feat(db): アンケートの回答と表示履歴を保存する表を追加する" -m "利用者フィードバック（#84）の回答・表示履歴・回答時の興味傾向を保存するため。質問は移行ファイルに直接書く方針（設計書 第12章）なので、初期版の4問も同時に登録する。" -m "Refs #84"
```

---

### Task 2: 質問定義と回答の検証（DBなしの処理）

**Files:**
- Create: `server/internal/userfeedback/model.go`
- Create: `server/internal/userfeedback/definition.go`
- Test: `server/internal/userfeedback/definition_test.go`

**Interfaces:**
- Produces（以降のタスクはこの名前を使う）:
  - エラー: `ErrNotFound`、`ErrInvalidInput`、`ErrConflict`、`ErrInvalidDefinition`
  - 定数: `FollowupScoreMax = 2`、`MinScore = 1`、`MaxScore = 5`
  - 型: `PromptStatus`（`PromptShown`/`PromptDismissed`/`PromptSubmitted`）、`Stage`（`StageOverall`/`StageFollowup`）、`SubmissionStatus`（`SubmissionPartial`/`SubmissionCompleted`）、`Question`、`Form`、`Answer`、`PromptSummary`、`Status`、`Presentation`、`SubmitResult`
  - 関数: `ValidateDefinition(Form) error`、`(Form) Root() Question`、`(Form) FollowupsFor(score int32) []Question`、`ValidateOverall(Form, questionID int64, score int32) (bool, error)`、`ValidateFollowup(Form, overallScore int32, []Answer) error`、`NextEligibleAt(lastShown time.Time, cooldownDays int32) time.Time`

- [ ] **Step 1: 型を書く（試験のコンパイルに必要）**

`server/internal/userfeedback/model.go`:

```go
// Package userfeedback は利用者フィードバック（アンケート）の表示判定・回答の保存を扱う。
// 設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md
package userfeedback

import (
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("userfeedback: not found")
	ErrInvalidInput      = errors.New("userfeedback: invalid input")
	ErrConflict          = errors.New("userfeedback: conflict")
	ErrInvalidDefinition = errors.New("userfeedback: invalid form definition")
)

const (
	// FollowupScoreMax 以下の総合評価で追加質問を出す
	FollowupScoreMax int32 = 2
	MinScore         int32 = 1
	MaxScore         int32 = 5
)

type PromptStatus string

const (
	PromptShown     PromptStatus = "shown"
	PromptDismissed PromptStatus = "dismissed"
	PromptSubmitted PromptStatus = "submitted"
)

type Stage string

const (
	StageOverall  Stage = "overall"
	StageFollowup Stage = "followup"
)

type SubmissionStatus string

const (
	SubmissionPartial   SubmissionStatus = "partial"
	SubmissionCompleted SubmissionStatus = "completed"
)

type Question struct {
	ID                  int64
	Key                 string
	Text                string
	SortOrder           int32
	Required            bool
	DisplayIfQuestionID *int64
	DisplayIfScoreMax   *int32
}

// Form は1つの版の質問一式。Questions は sort_order 順。
type Form struct {
	ID           int64
	Key          string
	Version      int32
	Title        string
	CooldownDays int32
	Questions    []Question
}

type Answer struct {
	QuestionID int64
	Score      int32
}

type PromptSummary struct {
	ID               string
	Status           PromptStatus
	Stage            Stage
	ShownAt          time.Time
	SubmissionID     *string
	SubmissionStatus *SubmissionStatus
}

type Status struct {
	Eligible       bool
	NextEligibleAt *time.Time
	LastPrompt     *PromptSummary
}

// Presentation は表示要求の結果。Prompt.Status が shown のときだけ Form を持つ。
type Presentation struct {
	Eligible       bool
	NextEligibleAt *time.Time
	Prompt         *PromptSummary
	Form           *Form
}

type SubmitResult struct {
	SubmissionID     string
	Status           SubmissionStatus
	FollowupRequired bool
}
```

- [ ] **Step 2: 失敗する試験を書く**

`server/internal/userfeedback/definition_test.go`:

```go
package userfeedback_test

import (
	"errors"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

func ptr[T any](v T) *T { return &v }

func validForm() userfeedback.Form {
	return userfeedback.Form{ID: 1, Key: "service_satisfaction", Version: 1, Title: "t", CooldownDays: 60, Questions: []userfeedback.Question{
		{ID: 10, Key: "overall", Text: "q", SortOrder: 1, Required: true},
		{ID: 11, Key: "interest_match", Text: "q", SortOrder: 2, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
		{ID: 12, Key: "freshness", Text: "q", SortOrder: 3, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
		{ID: 13, Key: "usability", Text: "q", SortOrder: 4, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
	}}
}

func TestValidateDefinitionAcceptsInitialForm(t *testing.T) {
	if err := userfeedback.ValidateDefinition(validForm()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDefinitionRejectsBrokenForms(t *testing.T) {
	cases := map[string]func(*userfeedback.Form){
		"総合評価が0問":       func(f *userfeedback.Form) { f.Questions = f.Questions[1:] },
		"総合評価が2問":       func(f *userfeedback.Form) { f.Questions[1].DisplayIfQuestionID, f.Questions[1].DisplayIfScoreMax = nil, nil },
		"追加質問が追加質問を参照":  func(f *userfeedback.Form) { f.Questions[2].DisplayIfQuestionID = ptr[int64](11) },
		"しきい値が2以外":      func(f *userfeedback.Form) { f.Questions[1].DisplayIfScoreMax = ptr[int32](3) },
		"しきい値だけある":      func(f *userfeedback.Form) { f.Questions[0].DisplayIfScoreMax = ptr[int32](2) },
		"質問キーの重複":       func(f *userfeedback.Form) { f.Questions[2].Key = "interest_match" },
		"並び順の重複":        func(f *userfeedback.Form) { f.Questions[2].SortOrder = 2 },
		"再表示間隔が0日":      func(f *userfeedback.Form) { f.CooldownDays = 0 },
		"自分自身を参照":       func(f *userfeedback.Form) { f.Questions[3].DisplayIfQuestionID = ptr[int64](13) },
	}
	for name, mutate := range cases {
		f := validForm()
		mutate(&f)
		if err := userfeedback.ValidateDefinition(f); !errors.Is(err, userfeedback.ErrInvalidDefinition) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestValidateDefinitionAcceptsFormWithoutFollowups(t *testing.T) {
	f := validForm()
	f.Questions = f.Questions[:1]
	if err := userfeedback.ValidateDefinition(f); err != nil {
		t.Fatal(err)
	}
	needs, err := userfeedback.ValidateOverall(f, 10, 1)
	if err != nil || needs {
		t.Fatalf("追加質問が無い版では低評価でも完了する: %v %v", needs, err)
	}
}

func TestValidateOverall(t *testing.T) {
	f := validForm()
	for score, want := range map[int32]bool{1: true, 2: true, 3: false, 5: false} {
		got, err := userfeedback.ValidateOverall(f, 10, score)
		if err != nil || got != want {
			t.Errorf("score %d: %v %v", score, got, err)
		}
	}
	for _, bad := range []struct {
		id    int64
		score int32
	}{{10, 0}, {10, 6}, {11, 3}, {99, 3}} {
		if _, err := userfeedback.ValidateOverall(f, bad.id, bad.score); !errors.Is(err, userfeedback.ErrInvalidInput) {
			t.Errorf("%+v: err = %v", bad, err)
		}
	}
}

func TestValidateFollowup(t *testing.T) {
	f := validForm()
	ok := []userfeedback.Answer{{11, 1}, {12, 5}, {13, 3}}
	if err := userfeedback.ValidateFollowup(f, 2, ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string][]userfeedback.Answer{
		"必須の未回答":   {{11, 1}, {12, 5}},
		"範囲外の値":    {{11, 1}, {12, 5}, {13, 6}},
		"総合評価を混ぜる": {{10, 1}, {11, 1}, {12, 5}, {13, 3}},
		"同じ質問を2回":  {{11, 1}, {11, 2}, {12, 5}, {13, 3}},
		"存在しない質問":  {{11, 1}, {12, 5}, {13, 3}, {99, 1}},
	}
	for name, answers := range bad {
		if err := userfeedback.ValidateFollowup(f, 2, answers); !errors.Is(err, userfeedback.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := userfeedback.ValidateFollowup(f, 4, ok); !errors.Is(err, userfeedback.ErrInvalidInput) {
		t.Errorf("高評価には追加質問が無い: err = %v", err)
	}
}

func TestNextEligibleAt(t *testing.T) {
	shown := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if got := userfeedback.NextEligibleAt(shown, 60); !got.Equal(time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %v", got)
	}
}
```

- [ ] **Step 3: 失敗を確認する**

Run: `cd server && go test ./internal/userfeedback/ -v`
Expected: FAIL（`undefined: userfeedback.ValidateDefinition` など）

- [ ] **Step 4: 実装する**

`server/internal/userfeedback/definition.go`:

```go
package userfeedback

import (
	"fmt"
	"time"
)

// ValidateDefinition は、DB制約だけでは守れない、複数の質問にまたがる条件を確かめる（設計書 第12章）。
// 移行ファイル以外の書き込み経路（将来の管理画面など）を作るときも、保存前に必ずこれを通す。
func ValidateDefinition(f Form) error {
	if f.CooldownDays < 1 {
		return fmt.Errorf("%w: 再表示間隔は1日以上にしてください", ErrInvalidDefinition)
	}
	keys := map[string]bool{}
	orders := map[int32]bool{}
	var roots []Question
	for _, q := range f.Questions {
		if keys[q.Key] || orders[q.SortOrder] {
			return fmt.Errorf("%w: 質問キーまたは並び順が重複しています", ErrInvalidDefinition)
		}
		keys[q.Key], orders[q.SortOrder] = true, true
		if q.DisplayIfQuestionID == nil {
			if q.DisplayIfScoreMax != nil {
				return fmt.Errorf("%w: 表示条件の参照先が無いのにしきい値があります", ErrInvalidDefinition)
			}
			roots = append(roots, q)
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("%w: 総合評価はちょうど1問にしてください", ErrInvalidDefinition)
	}
	root := roots[0]
	for _, q := range f.Questions {
		if q.DisplayIfQuestionID == nil {
			continue
		}
		if *q.DisplayIfQuestionID != root.ID || q.DisplayIfScoreMax == nil || *q.DisplayIfScoreMax != FollowupScoreMax {
			return fmt.Errorf("%w: 追加質問は総合評価だけを参照し、しきい値を2にしてください", ErrInvalidDefinition)
		}
	}
	return nil
}

// Root は総合評価の質問を返す。ValidateDefinition を通った版にだけ使う。
func (f Form) Root() Question {
	for _, q := range f.Questions {
		if q.DisplayIfQuestionID == nil {
			return q
		}
	}
	return Question{}
}

// FollowupsFor は、総合評価が score のときに表示する追加質問を返す。
func (f Form) FollowupsFor(score int32) []Question {
	var out []Question
	for _, q := range f.Questions {
		if q.DisplayIfScoreMax != nil && score <= *q.DisplayIfScoreMax {
			out = append(out, q)
		}
	}
	return out
}

func validScore(score int32) bool { return score >= MinScore && score <= MaxScore }

// ValidateOverall は総合評価の入力を、表示時の版で確かめる。追加質問へ進むかを返す。
func ValidateOverall(f Form, questionID int64, score int32) (bool, error) {
	if !validScore(score) {
		return false, fmt.Errorf("%w: 評価は1〜5で選んでください", ErrInvalidInput)
	}
	if f.Root().ID != questionID {
		return false, fmt.Errorf("%w: 総合評価の質問ではありません", ErrInvalidInput)
	}
	return len(f.FollowupsFor(score)) > 0, nil
}

// ValidateFollowup は追加質問の回答が、表示された質問とちょうど一致するかを確かめる。
func ValidateFollowup(f Form, overallScore int32, answers []Answer) error {
	expected := f.FollowupsFor(overallScore)
	if len(expected) == 0 {
		return fmt.Errorf("%w: 追加質問はありません", ErrInvalidInput)
	}
	byID := make(map[int64]Question, len(expected))
	for _, q := range expected {
		byID[q.ID] = q
	}
	seen := map[int64]bool{}
	for _, a := range answers {
		if _, ok := byID[a.QuestionID]; !ok || seen[a.QuestionID] || !validScore(a.Score) {
			return fmt.Errorf("%w: 回答の内容が正しくありません", ErrInvalidInput)
		}
		seen[a.QuestionID] = true
	}
	for _, q := range expected {
		if q.Required && !seen[q.ID] {
			return fmt.Errorf("%w: すべての質問に回答してください", ErrInvalidInput)
		}
	}
	return nil
}

// NextEligibleAt は、直近の表示日時から次に表示できる日時を返す。
func NextEligibleAt(lastShown time.Time, cooldownDays int32) time.Time {
	return lastShown.AddDate(0, 0, int(cooldownDays))
}
```

- [ ] **Step 5: 通ることを確認する**

Run: `cd server && go test ./internal/userfeedback/ -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
git add server/internal/userfeedback/
git commit -m "feat(feedback): アンケートの質問定義と回答を確かめる処理を追加する" -m "DB制約だけでは守れない「総合評価はちょうど1問」などの条件と、表示時の版に合う回答かどうかを、DBに触れずに確かめられるようにするため（設計書 第10章・第12章）。" -m "Refs #84"
```

---

### Task 3: DB問い合わせと、状態照会・表示要求の保存処理

**Files:**
- Create: `server/db/queries/user_feedback.sql`
- Create（生成）: `server/internal/store/dbgen/user_feedback.sql.go`（`go tool sqlc generate`）
- Create: `server/internal/store/feedback.go`
- Test: `server/internal/store/feedback_integration_test.go`

**Interfaces:**
- Consumes: Task 2 の型と関数。既存の `userID32`（`server/internal/store/auth.go:53`）、テスト補助 `newTestPool`・`queryInt`・`hashOf`・`fixedNow`（`server/internal/store/auth_integration_test.go`）。
- Produces:
  - `store.NewFeedback(*pgxpool.Pool) *store.Feedback`
  - `(*Feedback) ActiveForm(ctx) (userfeedback.Form, error)` — 無ければ `ErrNotFound`、定義が壊れていれば `ErrInvalidDefinition`
  - `(*Feedback) LastPrompt(ctx, userID int64, formKey string) (*userfeedback.PromptSummary, error)` — 無ければ `nil, nil`
  - `(*Feedback) Present(ctx, userID int64, promptID string, now time.Time) (userfeedback.Presentation, error)`

- [ ] **Step 1: 問い合わせを書く**

`server/db/queries/user_feedback.sql`:

```sql
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
DELETE FROM feedback_prompts WHERE id IN (
    SELECT id FROM feedback_prompts
    WHERE COALESCE(finished_at, shown_at) < sqlc.arg(before)
    ORDER BY COALESCE(finished_at, shown_at)
    LIMIT sqlc.arg(batch_size)
);
```

- [ ] **Step 2: 生成する**

Run: `cd server && go tool sqlc generate && go build ./...`
Expected: `internal/store/dbgen/user_feedback.sql.go` ができ、ビルドが通る。以降のコードは sqlc の命名（`user_ID`→`UserID`、`tag_ID`→`TagID`、`display_if_question_id`→`DisplayIfQuestionID`、uuid→`pgtype.UUID`、timestamptz→`pgtype.Timestamptz`、NULL可の整数→`pgtype.Int8`/`pgtype.Int4`、LEFT JOIN 側→`pgtype.UUID`/`pgtype.Text`）を前提にしている。生成された名前が違う場合は、生成物に合わせて読み替える。

- [ ] **Step 3: 失敗する結合試験を書く**

`server/internal/store/feedback_integration_test.go`:

```go
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
	// 応答が失われて同じIDで再送しても、60日判定に止められず同じ質問が返る
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

// Review Focus: 2つのタブで同時に3件目を開いても、表示は1回だけ
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

// 同じIDの要求が同時に届いても、表示記録は1件で、両方に同じ版の質問が返る
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
	// 版2へ切り替える（旧版をretiredにしてから新しい版をactiveで入れる）
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
```

- [ ] **Step 4: 失敗を確認する**

Run: `cd server && go test ./internal/store/ -run Feedback -v`
Expected: FAIL（`undefined: store.NewFeedback`）

- [ ] **Step 5: 保存処理を実装する**

`server/internal/store/feedback.go`:

```go
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

// Feedback は利用者フィードバックの保存先。複数の表を更新する処理は1つのトランザクションにまとめる。
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

// existingPresentation は保存済みの表示記録をロックして返す。無ければ ok=false。
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

// Present は表示要求を処理する（設計書 第10章 POST presentations の手順1〜8）。
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
	// ロックを待つ間に同じIDが保存された場合に備えて、もう一度確かめる
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
	if n == 0 { // 別の利用者が同じIDを先に使った
		return userfeedback.Presentation{}, userfeedback.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return userfeedback.Presentation{}, errFeedback
	}
	return userfeedback.Presentation{
		Eligible: true,
		Prompt:   &userfeedback.PromptSummary{ID: uuidText(pid), Status: userfeedback.PromptShown, Stage: userfeedback.StageOverall, ShownAt: now},
		Form:     &form,
	}, nil
}
```

- [ ] **Step 6: 通ることを確認する**

Run: `cd server && go test ./internal/store/ -run Feedback -race -v`
Expected: PASS（`TEST_DATABASE_URL` が必要）

- [ ] **Step 7: コミット**

```bash
git add server/db/queries/user_feedback.sql server/internal/store/dbgen/ server/internal/store/feedback.go server/internal/store/feedback_integration_test.go
git commit -m "feat(feedback): アンケートを出してよいかの判定と表示記録の保存を追加する" -m "同じ利用者に60日以内に二度出さないこと、応答が失われて再送されても二重に記録しないこと、2つのタブから同時に要求されても1回だけ出すことを、DBのロックで守るため（設計書 第10章）。" -m "Refs #84"
```

---

### Task 4: 回答・追加回答・離脱・期限切れ削除の保存処理

**Files:**
- Modify: `server/internal/store/feedback.go`（末尾に追記）
- Test: `server/internal/store/feedback_integration_test.go`（追記）

**Interfaces:**
- Consumes: Task 3 の `Feedback`、`parseUUID`、`uuidText`、`ts`、`feedbackUserID`、`formByID`、生成済みの問い合わせ。
- Produces:
  - `(*Feedback) SubmitOverall(ctx, userID int64, promptID string, questionID int64, score int32, now time.Time) (userfeedback.SubmitResult, error)`
  - `(*Feedback) CompleteFollowup(ctx, userID int64, submissionID string, answers []userfeedback.Answer, now time.Time) (userfeedback.SubmitResult, error)`
  - `(*Feedback) Dismiss(ctx, userID int64, promptID string, now time.Time) (userfeedback.PromptSummary, error)`
  - `(*Feedback) DeleteExpired(ctx, before time.Time, limit int32) (int64, error)`
  - コンパイル時の確認 `var _ userfeedback.Repository = (*Feedback)(nil)` は Task 5 で `Repository` を定義した後に足す。

- [ ] **Step 1: 失敗する試験を追記する**

`server/internal/store/feedback_integration_test.go` の末尾に追記:

```go
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
	// 興味度: Go(1) に加えて6タグ。同じ値は tag_ID の小さい順
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('A'),('B'),('C'),('D'),('E'),('F')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recommend("user_ID","tag_ID",match_int)
		SELECT $1, "tag_ID", CASE tag_name WHEN 'A' THEN 9000 WHEN 'B' THEN 7000 WHEN 'C' THEN 7000 WHEN 'D' THEN 5000 WHEN 'E' THEN 3000 ELSE 100 END
		FROM tag WHERE tag_name IN ('A','B','C','D','E','F')`, uid); err != nil {
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
	if want := []string{"A", "B", "C", "D", "E"}; len(names) != 5 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] || names[4] != want[4] {
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
	// 未終了（部分回答のまま）なので shown_at から2年。境界の手前では消さない
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
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd server && go test ./internal/store/ -run Feedback -v`
Expected: FAIL（`repo.SubmitOverall undefined` など）

- [ ] **Step 3: 実装する**

`server/internal/store/feedback.go` の末尾に追記:

```go
// lockOwnPrompt は表示記録をロックし、本人のものか確かめる。
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

// SubmitOverall は総合評価を保存する。回答時点の興味傾向も同じトランザクションで保存する。
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
	if err == nil { // 再送: 既存の結果を返す
		st := userfeedback.SubmissionStatus(existing.Status)
		return userfeedback.SubmitResult{SubmissionID: uuidText(existing.ID), Status: st,
			FollowupRequired: st == userfeedback.SubmissionPartial && p.Status == string(userfeedback.PromptShown)}, nil
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
	sid, err := q.InsertFeedbackSubmission(ctx, dbgen.InsertFeedbackSubmissionParams{
		FormID: p.FormID, UserID: uid, PromptID: pid, Status: string(status), StartedAt: ts(now), CompletedAt: completedAt,
	})
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
		if err := q.InsertFeedbackSnapshot(ctx, dbgen.InsertFeedbackSnapshotParams{
			SubmissionID: sid, Rank: int32(i + 1), TagID: pgtype.Int4{Int32: w.TagID, Valid: true}, TagName: w.TagName, MatchInt: w.MatchInt,
		}); err != nil {
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

// CompleteFollowup は部分回答へ追加質問の回答を保存して完了にする。
// ロックの順番は SubmitOverall・Dismiss と同じく「表示記録 → 回答」にそろえる。
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
	if sub.Status == string(userfeedback.SubmissionCompleted) { // 再送
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

// Dismiss は回答せず閉じたことを保存する。閉じた段階はDBに保存済みの stage を使う。
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

// DeleteExpired は保持期限を過ぎた表示記録を、古い順に limit 件まで消す。回答などは連鎖して消える。
// 表示判定の応答を遅らせないよう、DB側の実行時間に2秒の上限を付ける（設計書 第14章）。
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
```

- [ ] **Step 4: 通ることを確認する**

Run: `cd server && go test ./internal/store/ -run Feedback -race -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add server/internal/store/feedback.go server/internal/store/feedback_integration_test.go
git commit -m "feat(feedback): アンケートの回答・途中で閉じた記録・古い記録の削除を保存できるようにする" -m "低評価のときだけ追加質問へ進み、途中で閉じても総合評価を残すため。回答時点の興味傾向も同時に残し、2年を過ぎた記録はまとめて消せるようにする（設計書 第9章・第14章）。" -m "Refs #84"
```

---

### Task 5: サービス（表示可否の判定と古い記録の削除の間引き）

**Files:**
- Create: `server/internal/userfeedback/service.go`
- Modify: `server/internal/store/feedback.go`（`var _ userfeedback.Repository = (*Feedback)(nil)` を追加）
- Test: `server/internal/userfeedback/service_test.go`

**Interfaces:**
- Consumes: Task 2 の型、Task 3・4 のメソッド形。
- Produces:
  - `type Repository interface { ActiveForm; LastPrompt; Present; SubmitOverall; CompleteFollowup; Dismiss; DeleteExpired }`（シグネチャは Task 3・4 と同じ）
  - `type Clock interface{ Now() time.Time }`
  - `type Service struct { Repo Repository; Clock Clock; Logger *slog.Logger; ... }`（ポインタで使う。ゼロ値の `&Service{}` でもOpenAPI生成に使える）
  - `(*Service) Status(ctx, userID int64) (Status, error)`、`Present(ctx, userID int64, promptID string) (Presentation, error)`、`SubmitOverall(ctx, userID int64, promptID string, questionID int64, score int32) (SubmitResult, error)`、`CompleteFollowup(ctx, userID int64, submissionID string, answers []Answer) (SubmitResult, error)`、`Dismiss(ctx, userID int64, promptID string) (PromptSummary, error)`
  - 定数 `RetentionYears = 2`、`CleanupInterval = time.Hour`、`CleanupBatchSize int32 = 500`

- [ ] **Step 1: 失敗する試験を書く**

`server/internal/userfeedback/service_test.go`:

```go
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

// Review Focus: 有効なフォームが無い・壊れているときはポップアップを出さない
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
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd server && go test ./internal/userfeedback/ -v`
Expected: FAIL（`undefined: userfeedback.Service`）

- [ ] **Step 3: 実装する**

`server/internal/userfeedback/service.go`:

```go
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
	ActiveForm(ctx context.Context) (Form, error)
	LastPrompt(ctx context.Context, userID int64, formKey string) (*PromptSummary, error)
	Present(ctx context.Context, userID int64, promptID string, now time.Time) (Presentation, error)
	SubmitOverall(ctx context.Context, userID int64, promptID string, questionID int64, score int32, now time.Time) (SubmitResult, error)
	CompleteFollowup(ctx context.Context, userID int64, submissionID string, answers []Answer, now time.Time) (SubmitResult, error)
	Dismiss(ctx context.Context, userID int64, promptID string, now time.Time) (PromptSummary, error)
	DeleteExpired(ctx context.Context, before time.Time, limit int32) (int64, error)
}

type Clock interface{ Now() time.Time }

// Service はポインタで使う（古い記録の削除を間引くため、最後に試みた時刻を持つ）。
type Service struct {
	Repo   Repository
	Clock  Clock
	Logger *slog.Logger

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

// unusableForm は、有効なフォームが無い・壊れているときに true を返す。壊れている場合は警告を残す。
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

// cleanupIfDue は保持期限を過ぎた記録の削除を、プロセスごとに1時間に1回まで試みる。
// 表示判定のトランザクションとは別に行い、失敗しても表示判定の結果には影響させない。
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
```

`server/internal/store/feedback.go` の `errFeedback` の宣言の直後に追加:

```go
var _ userfeedback.Repository = (*Feedback)(nil)
```

- [ ] **Step 4: 通ることを確認する**

Run: `cd server && go test ./internal/userfeedback/ ./internal/store/ -race -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add server/internal/userfeedback/service.go server/internal/userfeedback/service_test.go server/internal/store/feedback.go
git commit -m "feat(feedback): アンケートを出せるかの判定と古い記録の削除をまとめる" -m "有効な質問が無い・壊れているときはアンケートを出さずに記事閲覧を続けられるようにし、2年を過ぎた記録の削除はサーバー側の定期実行を足さずに、表示判定のついでに1時間に1回まで行うため（設計書 第14章）。" -m "Refs #84"
```

---

### Task 6: APIの窓口と配線、API仕様ファイルの生成

**Files:**
- Create: `server/internal/userfeedback/handler.go`
- Modify: `server/internal/app/app.go`、`server/cmd/api/main.go`、`server/cmd/openapi/main.go`
- Regenerate: `server/openapi/openapi.json`
- Test: `server/internal/userfeedback/handler_test.go`

**Interfaces:**
- Consumes: Task 5 の `*Service`。既存の `auth.Service`、`httpx.NewProblem`。
- Produces（OpenAPI のスキーマ名。Task 7 の型の別名で使う）:
  - `GET /api/v1/user-feedback/status` → `FeedbackStatusResponse`
  - `POST /api/v1/user-feedback/presentations`（`FeedbackPresentationRequest`）→ `FeedbackPresentationResponse`
  - `POST /api/v1/user-feedback/submissions`（`FeedbackSubmissionRequest`）→ `FeedbackSubmissionResponse`
  - `PUT /api/v1/user-feedback/submissions/{submission_id}`（`FeedbackFollowupRequest`）→ `FeedbackSubmissionResponse`
  - `POST /api/v1/user-feedback/dismissals`（`FeedbackDismissalRequest`）→ `FeedbackDismissalResponse`
  - 補助スキーマ: `FeedbackForm`、`FeedbackQuestion`、`FeedbackLastPrompt`、`FeedbackAnswer`
  - `app.Dependencies.UserFeedback *userfeedback.Service`

- [ ] **Step 1: 失敗する試験を書く**

`server/internal/userfeedback/handler_test.go`:

```go
package userfeedback_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const sessionToken = "fixed-session-token"
const origin = "http://localhost:5173"
const promptID = "11111111-1111-4111-8111-111111111111"

type sessionRepo struct{}

func (sessionRepo) FindUser(_ context.Context, hash [32]byte, _ time.Time) (auth.Session, error) {
	if hash != auth.HashToken(sessionToken) {
		return auth.Session{}, auth.ErrNotFound
	}
	return auth.Session{User: auth.User{ID: 9, Username: "fixture", Role: auth.RoleMember}}, nil
}
func (sessionRepo) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (sessionRepo) Delete(context.Context, [32]byte) error                   { return nil }
func (sessionRepo) DeleteExpired(context.Context, time.Time) error           { return nil }

type errRepo struct {
	fakeRepo
	err error
}

func (r *errRepo) SubmitOverall(context.Context, int64, string, int64, int32, time.Time) (userfeedback.SubmitResult, error) {
	return userfeedback.SubmitResult{}, r.err
}

func newHandler(repo userfeedback.Repository) http.Handler {
	clock := &fakeClock{now: base}
	authSvc := auth.Service{Sessions: sessionRepo{}, Clock: clock}
	cfg := config.Config{Environment: "test", SwaggerEnabled: true, CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Auth:         &authSvc,
		UserFeedback: &userfeedback.Service{Repo: repo, Clock: clock},
	})
	return h
}

func send(h http.Handler, method, path, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: sessionToken})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFeedbackRoutesRequireAuthenticationAndCSRF(t *testing.T) {
	h := newHandler(&fakeRepo{form: validForm()})
	if got := send(h, http.MethodGet, "/api/v1/user-feedback/status", "", false, false); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", got.Code)
	}
	body := `{"prompt_id":"` + promptID + `"}`
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", body, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("CSRFヘッダーなし = %d", got.Code)
	}
	if got := send(h, http.MethodPut, "/api/v1/user-feedback/submissions/"+promptID, `{"answers":[{"question_id":11,"score":3}]}`, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("PUTもCSRF対象 = %d", got.Code)
	}
}

func TestFeedbackPresentationReturnsQuestions(t *testing.T) {
	form := validForm()
	repo := &fakeRepo{form: form, presentOut: userfeedback.Presentation{
		Eligible: true,
		Prompt:   &userfeedback.PromptSummary{ID: promptID, Status: userfeedback.PromptShown, Stage: userfeedback.StageOverall, ShownAt: base},
		Form:     &form,
	}}
	h := newHandler(repo)
	got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d %s", got.Code, got.Body.String())
	}
	for _, want := range []string{`"eligible":true`, `"status":"shown"`, `"stage":"overall"`, `"prompt_id":"` + promptID + `"`, `"display_if_score_max":2`, `"key":"overall"`} {
		if !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %s", want, got.Body.String())
		}
	}
	if bad := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"x"}`, true, true); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("UUIDでないIDは422 = %d", bad.Code)
	}
}

func TestFeedbackErrorsMapToStatusCodes(t *testing.T) {
	cases := map[error]int{
		userfeedback.ErrNotFound:     http.StatusNotFound,
		userfeedback.ErrInvalidInput: http.StatusUnprocessableEntity,
		userfeedback.ErrConflict:     http.StatusConflict,
		context.DeadlineExceeded:     http.StatusInternalServerError,
	}
	for err, want := range cases {
		h := newHandler(&errRepo{err: err})
		got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":3}`, true, true)
		if got.Code != want || !strings.Contains(got.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("%v: %d %s", err, got.Code, got.Header().Get("Content-Type"))
		}
	}
	h := newHandler(&fakeRepo{})
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":6}`, true, true); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("score 6 = %d", got.Code)
	}
}

func TestFeedbackOpenAPIContainsRoutes(t *testing.T) {
	h := newHandler(&fakeRepo{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	for _, path := range []string{"/api/v1/user-feedback/status", "/api/v1/user-feedback/presentations", "/api/v1/user-feedback/submissions", "/api/v1/user-feedback/submissions/{submission_id}", "/api/v1/user-feedback/dismissals"} {
		if !strings.Contains(rec.Body.String(), path) {
			t.Fatalf("OpenAPI missing %s", path)
		}
	}
}
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd server && go test ./internal/userfeedback/ -run Feedback -v`
Expected: FAIL（`unknown field UserFeedback in struct literal of type app.Dependencies`）

- [ ] **Step 3: 窓口を実装する**

`server/internal/userfeedback/handler.go`:

```go
package userfeedback

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

type Handler struct {
	Service    *Service
	Auth       auth.Service
	CookieName string
}

type tokenKey struct{}

type FeedbackQuestion struct {
	ID                  int64  `json:"id"`
	Key                 string `json:"key"`
	Text                string `json:"text"`
	SortOrder           int32  `json:"sort_order"`
	Required            bool   `json:"required"`
	DisplayIfQuestionID *int64 `json:"display_if_question_id,omitempty" doc:"この質問を出す条件になる質問。総合評価では省略"`
	DisplayIfScoreMax   *int32 `json:"display_if_score_max,omitempty" doc:"条件の質問の評価がこの値以下のときに出す"`
}
type FeedbackForm struct {
	Title     string             `json:"title"`
	Version   int32              `json:"version"`
	Questions []FeedbackQuestion `json:"questions"`
}
type FeedbackLastPrompt struct {
	Status           string    `json:"status" enum:"shown,dismissed,submitted"`
	Stage            string    `json:"stage" enum:"overall,followup"`
	ShownAt          time.Time `json:"shown_at"`
	SubmissionStatus string    `json:"submission_status,omitempty" enum:"partial,completed"`
}
type FeedbackStatusResponse struct {
	Eligible       bool                `json:"eligible"`
	NextEligibleAt *time.Time          `json:"next_eligible_at,omitempty"`
	LastPrompt     *FeedbackLastPrompt `json:"last_prompt,omitempty"`
}
type FeedbackPresentationRequest struct {
	PromptID string `json:"prompt_id" format:"uuid" doc:"画面が表示要求ごとに作るID。同じ要求の再送では同じ値を使う"`
}
type FeedbackPresentationResponse struct {
	Eligible       bool          `json:"eligible"`
	NextEligibleAt *time.Time    `json:"next_eligible_at,omitempty"`
	PromptID       string        `json:"prompt_id,omitempty"`
	Status         string        `json:"status,omitempty" enum:"shown,dismissed,submitted"`
	Stage          string        `json:"stage,omitempty" enum:"overall,followup"`
	SubmissionID   string        `json:"submission_id,omitempty"`
	Form           *FeedbackForm `json:"form,omitempty" doc:"status が shown のときだけ返る"`
}
type FeedbackSubmissionRequest struct {
	PromptID   string `json:"prompt_id" format:"uuid"`
	QuestionID int64  `json:"question_id" minimum:"1"`
	Score      int32  `json:"score" minimum:"1" maximum:"5"`
}
type FeedbackAnswer struct {
	QuestionID int64 `json:"question_id" minimum:"1"`
	Score      int32 `json:"score" minimum:"1" maximum:"5"`
}
type FeedbackFollowupRequest struct {
	Answers []FeedbackAnswer `json:"answers" minItems:"1" maxItems:"20"`
}
type FeedbackSubmissionResponse struct {
	SubmissionID     string `json:"submission_id"`
	Status           string `json:"status" enum:"partial,completed"`
	FollowupRequired bool   `json:"followup_required"`
}
type FeedbackDismissalRequest struct {
	PromptID string `json:"prompt_id" format:"uuid"`
}
type FeedbackDismissalResponse struct {
	PromptID string `json:"prompt_id"`
	Status   string `json:"status" enum:"shown,dismissed,submitted"`
	Stage    string `json:"stage" enum:"overall,followup"`
}

type statusOutput struct{ Body FeedbackStatusResponse }
type presentationInput struct{ Body FeedbackPresentationRequest }
type presentationOutput struct{ Body FeedbackPresentationResponse }
type submissionInput struct{ Body FeedbackSubmissionRequest }
type followupInput struct {
	SubmissionID string `path:"submission_id" format:"uuid"`
	Body         FeedbackFollowupRequest
}
type submissionOutput struct{ Body FeedbackSubmissionResponse }
type dismissalInput struct{ Body FeedbackDismissalRequest }
type dismissalOutput struct{ Body FeedbackDismissalResponse }

func (h Handler) Register(api huma.API) {
	if api.OpenAPI().Components == nil {
		api.OpenAPI().Components = &huma.Components{}
	}
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes["cookieAuth"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie"}
	security := []map[string][]string{{"cookieAuth": {}}}
	mw := huma.Middlewares{h.readToken}
	tags := []string{"user-feedback"}
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}
	huma.Register(api, huma.Operation{OperationID: "user-feedback-status", Method: http.MethodGet, Path: "/api/v1/user-feedback/status", Summary: "アンケートの表示可否と直近の状態", Tags: tags, Security: security, Middlewares: mw, Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError}}, h.status)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-present", Method: http.MethodPost, Path: "/api/v1/user-feedback/presentations", Summary: "アンケートの表示を要求", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.present)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-submit", Method: http.MethodPost, Path: "/api/v1/user-feedback/submissions", Summary: "総合評価を保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.submit)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-complete", Method: http.MethodPut, Path: "/api/v1/user-feedback/submissions/{submission_id}", Summary: "追加質問の回答を保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.complete)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-dismiss", Method: http.MethodPost, Path: "/api/v1/user-feedback/dismissals", Summary: "回答せず閉じたことを保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.dismiss)
}

func (h Handler) readToken(ctx huma.Context, next func(huma.Context)) {
	token := ""
	if c, err := huma.ReadCookie(ctx, h.CookieName); err == nil {
		token = c.Value
	}
	next(huma.WithValue(ctx, tokenKey{}, token))
}

func (h Handler) userID(ctx context.Context) (int64, error) {
	token, _ := ctx.Value(tokenKey{}).(string)
	session, err := h.Auth.Authenticate(ctx, token)
	if err != nil {
		return 0, httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
	}
	return session.User.ID, nil
}

func problem(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NewProblem(http.StatusNotFound, "not_found", "対象のアンケートが見つかりません")
	case errors.Is(err, ErrInvalidInput):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "回答の内容が正しくありません")
	case errors.Is(err, ErrConflict):
		return httpx.NewProblem(http.StatusConflict, "conflict", "このアンケートはすでに終了しています")
	default:
		return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
}

func toForm(f *Form) *FeedbackForm {
	if f == nil {
		return nil
	}
	out := &FeedbackForm{Title: f.Title, Version: f.Version, Questions: make([]FeedbackQuestion, 0, len(f.Questions))}
	for _, q := range f.Questions {
		out.Questions = append(out.Questions, FeedbackQuestion{ID: q.ID, Key: q.Key, Text: q.Text, SortOrder: q.SortOrder, Required: q.Required, DisplayIfQuestionID: q.DisplayIfQuestionID, DisplayIfScoreMax: q.DisplayIfScoreMax})
	}
	return out
}

func (h Handler) status(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.Service.Status(ctx, uid)
	if err != nil {
		return nil, problem(err)
	}
	out := &statusOutput{Body: FeedbackStatusResponse{Eligible: st.Eligible, NextEligibleAt: st.NextEligibleAt}}
	if p := st.LastPrompt; p != nil {
		last := &FeedbackLastPrompt{Status: string(p.Status), Stage: string(p.Stage), ShownAt: p.ShownAt}
		if p.SubmissionStatus != nil {
			last.SubmissionStatus = string(*p.SubmissionStatus)
		}
		out.Body.LastPrompt = last
	}
	return out, nil
}

func (h Handler) present(ctx context.Context, in *presentationInput) (*presentationOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.Service.Present(ctx, uid, in.Body.PromptID)
	if err != nil {
		return nil, problem(err)
	}
	body := FeedbackPresentationResponse{Eligible: p.Eligible, NextEligibleAt: p.NextEligibleAt, Form: toForm(p.Form)}
	if p.Prompt != nil {
		body.PromptID, body.Status, body.Stage = p.Prompt.ID, string(p.Prompt.Status), string(p.Prompt.Stage)
		if p.Prompt.SubmissionID != nil && p.Prompt.SubmissionStatus != nil && *p.Prompt.SubmissionStatus == SubmissionPartial {
			body.SubmissionID = *p.Prompt.SubmissionID
		}
	}
	return &presentationOutput{Body: body}, nil
}

func (h Handler) submit(ctx context.Context, in *submissionInput) (*submissionOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Service.SubmitOverall(ctx, uid, in.Body.PromptID, in.Body.QuestionID, in.Body.Score)
	if err != nil {
		return nil, problem(err)
	}
	return &submissionOutput{Body: FeedbackSubmissionResponse{SubmissionID: r.SubmissionID, Status: string(r.Status), FollowupRequired: r.FollowupRequired}}, nil
}

func (h Handler) complete(ctx context.Context, in *followupInput) (*submissionOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	answers := make([]Answer, 0, len(in.Body.Answers))
	for _, a := range in.Body.Answers {
		answers = append(answers, Answer{QuestionID: a.QuestionID, Score: a.Score})
	}
	r, err := h.Service.CompleteFollowup(ctx, uid, in.SubmissionID, answers)
	if err != nil {
		return nil, problem(err)
	}
	return &submissionOutput{Body: FeedbackSubmissionResponse{SubmissionID: r.SubmissionID, Status: string(r.Status), FollowupRequired: r.FollowupRequired}}, nil
}

func (h Handler) dismiss(ctx context.Context, in *dismissalInput) (*dismissalOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	s, err := h.Service.Dismiss(ctx, uid, in.Body.PromptID)
	if err != nil {
		return nil, problem(err)
	}
	return &dismissalOutput{Body: FeedbackDismissalResponse{PromptID: s.ID, Status: string(s.Status), Stage: string(s.Stage)}}, nil
}
```

- [ ] **Step 4: 配線する**

`server/internal/app/app.go`:
- import に `"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"` を追加。
- `Dependencies` に `UserFeedback *userfeedback.Service` を追加（`Recommendation` の下）。
- `recommendation.Handler{...}.Register(api)` の `if` ブロックの直後に追加:

```go
	if deps.Auth != nil && deps.UserFeedback != nil {
		userfeedback.Handler{Service: deps.UserFeedback, Auth: *deps.Auth, CookieName: cfg.CookieName}.Register(api)
	}
```

`server/cmd/api/main.go` の `handler, _ := app.New(...)` を次に置き換える（import に `userfeedback` を追加）:

```go
	feedbackService := &userfeedback.Service{Repo: store.NewFeedback(pool), Logger: logger}
	handler, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool, Auth: authService, Recommendation: recommendService, UserFeedback: feedbackService})
```

`server/cmd/openapi/main.go` の `app.Dependencies{...}` に `UserFeedback: &userfeedback.Service{},` を追加（import も追加）。

- [ ] **Step 5: 通ることを確認し、API仕様ファイルを作り直す**

Run:
```bash
cd server && go test ./internal/userfeedback/ ./internal/app/ ./cmd/... -race && go run ./cmd/openapi && git diff --stat openapi/openapi.json
```
Expected: PASS。`openapi/openapi.json` に `/api/v1/user-feedback/` の5経路と `Feedback*` スキーマが増える。

- [ ] **Step 6: サーバー全体の検証**

Run: `cd server && test -z "$(gofmt -l .)" && go vet ./... && go test ./... -race -cover && go build ./cmd/api ./cmd/migrate ./cmd/openapi`
Expected: すべて成功（Windowsの `gofmt -l` が改行コードだけでファイルを列挙した場合は、CIで確認する旨を報告に書く）。

- [ ] **Step 7: コミット**

```bash
git add server/internal/userfeedback/handler.go server/internal/userfeedback/handler_test.go server/internal/app/app.go server/cmd/api/main.go server/cmd/openapi/main.go server/openapi/openapi.json
git commit -m "feat(feedback): アンケートの窓口を5つ追加する" -m "画面からアンケートの表示可否の確認・表示要求・回答・追加回答・閉じた記録を送れるようにするため。記事クリックの窓口（/api/v1/feedback/）と区別して /api/v1/user-feedback/ に置く（設計書 第10章）。" -m "Refs #84"
```

---

### Task 7: 画面側の呼び出し関数と型

**Files:**
- Modify: `frontend/src/api/client.ts:45`（PUT対応）
- Regenerate: `frontend/src/api/generated.ts`（`npm run api:generate`）
- Modify: `frontend/src/api/types.ts`、`frontend/src/api/endpoints.ts`、`frontend/src/api/generated-contract.ts`
- Test: `frontend/src/api/endpoints.test.ts`、`frontend/src/api/client.test.ts`

**Interfaces:**
- Consumes: Task 6 の `openapi.json`。
- Produces:
  - 型: `FeedbackStatusResponse`、`FeedbackPresentationResponse`、`FeedbackForm`、`FeedbackQuestion`、`FeedbackSubmissionRequest`、`FeedbackSubmissionResponse`、`FeedbackAnswer`、`FeedbackDismissalResponse`
  - 関数: `fetchFeedbackStatus()`、`requestFeedbackPresentation(promptId: string)`、`submitFeedbackOverall(body: FeedbackSubmissionRequest)`、`completeFeedbackFollowup(submissionId: string, answers: FeedbackAnswer[])`、`dismissFeedback(promptId: string)`

- [ ] **Step 1: 型を生成する**

Run: `cd frontend && npm run api:generate`
Expected: `src/api/generated.ts` に `/api/v1/user-feedback/...` と `Feedback*` が増える。

- [ ] **Step 2: 失敗する試験を追記する**

`frontend/src/api/endpoints.test.ts` の `describe('endpoints', ...)` の中の末尾に追記:

```ts
  test('アンケートの窓口を正しい方式とパスで呼ぶ', async () => {
    const e = await loadEndpoints()
    const id = '11111111-1111-4111-8111-111111111111'
    await e.fetchFeedbackStatus()
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/status`, method: 'GET', body: undefined })
    await e.requestFeedbackPresentation(id)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/presentations`, method: 'POST', body: `{"prompt_id":"${id}"}` })
    await e.submitFeedbackOverall({ prompt_id: id, question_id: 10, score: 2 })
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/submissions`, method: 'POST', body: `{"prompt_id":"${id}","question_id":10,"score":2}` })
    await e.completeFeedbackFollowup(id, [{ question_id: 11, score: 3 }])
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/submissions/${id}`, method: 'PUT', body: '{"answers":[{"question_id":11,"score":3}]}' })
    await e.dismissFeedback(id)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/dismissals`, method: 'POST', body: `{"prompt_id":"${id}"}` })
  })
```

`frontend/src/api/client.test.ts` の `describe` の中に、PUT にも `X-MTP-CSRF: 1` と `Content-Type: application/json` が付くことを確かめる試験を追記する（`loadClient` と `fetchMock` は同じファイルにある補助）:

```ts
  test('PUTにもCSRF対策のヘッダーを付ける', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    const { request } = await loadClient()
    await request('PUT', '/x', { a: 1 })
    const init = fetchMock.mock.calls.at(-1)![1] as RequestInit
    expect(init.method).toBe('PUT')
    expect(init.headers).toMatchObject({ 'X-MTP-CSRF': '1', 'Content-Type': 'application/json' })
  })
```

- [ ] **Step 3: 失敗を確認する**

Run: `cd frontend && npx vitest run src/api`
Expected: FAIL（`e.fetchFeedbackStatus is not a function`、PUT の型エラー）

- [ ] **Step 4: 実装する**

`frontend/src/api/client.ts` の `request` の引数を変更:

```ts
export async function request<T>(
  method: 'GET' | 'POST' | 'PUT',
  path: string,
  body?: unknown,
): Promise<T> {
```

`frontend/src/api/types.ts` の末尾に追記:

```ts
export type FeedbackStatusResponse = Schemas['FeedbackStatusResponse']
export type FeedbackPresentationResponse = Schemas['FeedbackPresentationResponse']
export type FeedbackForm = Schemas['FeedbackForm']
export type FeedbackQuestion = Schemas['FeedbackQuestion']
export type FeedbackSubmissionRequest = Schemas['FeedbackSubmissionRequest']
export type FeedbackSubmissionResponse = Schemas['FeedbackSubmissionResponse']
export type FeedbackAnswer = Schemas['FeedbackAnswer']
export type FeedbackDismissalResponse = Schemas['FeedbackDismissalResponse']
```

`frontend/src/api/endpoints.ts`: import に型を追加し、末尾に追記:

```ts
/** アンケートを出してよいかと、直近の状態（ログイン後・記事一覧の表示時に呼ぶ） */
export function fetchFeedbackStatus() {
  return request<FeedbackStatusResponse>('GET', '/api/v1/user-feedback/status')
}

/** アンケートの表示を要求する。promptId は要求ごとに作り、再送では同じ値を使う */
export function requestFeedbackPresentation(promptId: string) {
  return request<FeedbackPresentationResponse>('POST', '/api/v1/user-feedback/presentations', {
    prompt_id: promptId,
  })
}

export function submitFeedbackOverall(body: FeedbackSubmissionRequest) {
  return request<FeedbackSubmissionResponse>('POST', '/api/v1/user-feedback/submissions', body)
}

export function completeFeedbackFollowup(submissionId: string, answers: FeedbackAnswer[]) {
  return request<FeedbackSubmissionResponse>(
    'PUT',
    `/api/v1/user-feedback/submissions/${encodeURIComponent(submissionId)}`,
    { answers },
  )
}

export function dismissFeedback(promptId: string) {
  return request<FeedbackDismissalResponse>('POST', '/api/v1/user-feedback/dismissals', {
    prompt_id: promptId,
  })
}
```

import 行は次にする:

```ts
import type {
  AuthResponse,
  FeedbackAnswer,
  FeedbackDismissalResponse,
  FeedbackPresentationResponse,
  FeedbackStatusResponse,
  FeedbackSubmissionRequest,
  FeedbackSubmissionResponse,
  FeedResponse,
  LoginRequest,
  SignupRequest,
  User,
} from './types'
```

`frontend/src/api/generated-contract.ts` を次にする:

```ts
import type { paths } from './generated'

type Login = paths['/api/v1/auth/login']['post']
type Feed = paths['/api/v1/feed']['get']
type Click = paths['/api/v1/feedback/article-clicks']['post']
type FeedbackStatus = paths['/api/v1/user-feedback/status']['get']
type FeedbackPresent = paths['/api/v1/user-feedback/presentations']['post']
type FeedbackSubmit = paths['/api/v1/user-feedback/submissions']['post']
type FeedbackComplete = paths['/api/v1/user-feedback/submissions/{submission_id}']['put']
type FeedbackDismiss = paths['/api/v1/user-feedback/dismissals']['post']

export const generatedContractExists:
  | [Login, Feed, Click, FeedbackStatus, FeedbackPresent, FeedbackSubmit, FeedbackComplete, FeedbackDismiss]
  | null = null
```

- [ ] **Step 5: 通ることを確認する**

Run: `cd frontend && npx vitest run src/api && npx tsc -b`
Expected: PASS、型エラーなし。

- [ ] **Step 6: コミット**

```bash
git add frontend/src/api/
git commit -m "feat(frontend): アンケートの窓口を呼ぶ関数を追加する" -m "画面からアンケートの表示要求・回答・閉じた記録を送るため。追加質問の保存はPUTなので、共通の通信処理もPUTに対応させる（CSRF対策のヘッダーはPOSTと同じく付く）。" -m "Refs #84"
```

---

### Task 8: 記事の開封数と表示停止日時を持つモジュール

**Files:**
- Create: `frontend/src/lib/feedbackTracker.ts`
- Test: `frontend/src/lib/feedbackTracker.test.ts`

**Interfaces:**
- Produces:
  - 定数 `FEEDBACK_ARTICLE_THRESHOLD = 3`、`CLICK_SETTLE_TIMEOUT_MS = 10_000`
  - `recordOpenedArticle(url: string): number`（異なる記事の数を返す）
  - `openedArticleCount(): number`
  - `clearOpenedArticles(): void`
  - `setNextEligibleAt(eligible: boolean, nextEligibleAt?: string | null): void`
  - `canRequestPresentation(now?: number): boolean`
  - `clearFeedbackState(): void`（ログイン成功・ログアウト・認証切れで呼ぶ）
  - `settleWithin(promises: Promise<unknown>[], ms?: number): Promise<void>`

- [ ] **Step 1: 失敗する試験を書く**

`frontend/src/lib/feedbackTracker.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import {
  canRequestPresentation,
  clearFeedbackState,
  clearOpenedArticles,
  openedArticleCount,
  recordOpenedArticle,
  setNextEligibleAt,
  settleWithin,
} from './feedbackTracker'

describe('feedbackTracker', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
    clearFeedbackState()
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  test('同じ記事は1件として数え、異なる記事だけを数える', () => {
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://b')).toBe(2)
    expect(openedArticleCount()).toBe(2)
  })

  test('開いた記事は利用者IDを含まないキーでsessionStorageに残る', () => {
    recordOpenedArticle('https://a')
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBe('["https://a"]')
  })

  test('ログイン・ログアウト時の消去で、前の利用者の状態を引き継がない', () => {
    recordOpenedArticle('https://a')
    setNextEligibleAt(false, '2099-01-01T00:00:00Z')
    clearFeedbackState()
    expect(openedArticleCount()).toBe(0)
    expect(canRequestPresentation()).toBe(true)
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  // Review Focus: 保存領域が使えなくても閲覧を壊さず、メモリで数え続ける
  test('sessionStorageが例外を出してもメモリで数える', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://b')).toBe(2)
    expect(() => clearOpenedArticles()).not.toThrow()
  })

  test('次回表示可能日時より前は要求せず、過ぎたら要求できる', () => {
    const next = Date.parse('2026-12-01T00:00:00Z')
    setNextEligibleAt(false, '2026-12-01T00:00:00Z')
    expect(canRequestPresentation(next - 1)).toBe(false)
    expect(canRequestPresentation(next)).toBe(true)
    setNextEligibleAt(false, null) // 有効なフォームが無い: このログイン中は要求しない
    expect(canRequestPresentation(next * 2)).toBe(false)
    setNextEligibleAt(true)
    expect(canRequestPresentation()).toBe(true)
  })

  // Review Focus: クリック学習の通信が返らなくても10秒で先へ進む
  test('settleWithinは全部終わるか、上限時間で返る', async () => {
    vi.useFakeTimers()
    let done = false
    const never = new Promise(() => {})
    const p = settleWithin([never, Promise.reject(new Error('x'))], 10_000).then(() => {
      done = true
    })
    await vi.advanceTimersByTimeAsync(9_999)
    expect(done).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    await p
    expect(done).toBe(true)
  })

  test('settleWithinは失敗した通信も確定として扱う', async () => {
    await expect(settleWithin([Promise.reject(new Error('x')), Promise.resolve()])).resolves.toBeUndefined()
  })
})
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd frontend && npx vitest run src/lib/feedbackTracker.test.ts`
Expected: FAIL（モジュールが無い）

- [ ] **Step 3: 実装する**

`frontend/src/lib/feedbackTracker.ts`:

```ts
// アンケートを出す条件（異なる記事を3件開いた）と、次に表示を要求してよい日時を、
// 現在のログイン中だけ持つ。設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md 第6章

const OPENED_KEY = 'mtp.feedback.openedArticles'

export const FEEDBACK_ARTICLE_THRESHOLD = 3
export const CLICK_SETTLE_TIMEOUT_MS = 10_000

// sessionStorage が使えない環境（プライベートモード等）でも数え続けるための控え
let memoryOpened: string[] = []
// この時刻（ミリ秒）より前は表示を要求しない。null は制限なし、Infinity はこのログイン中は要求しない
let blockedUntil: number | null = null

function readOpened(): string[] {
  try {
    const raw = window.sessionStorage.getItem(OPENED_KEY)
    if (raw === null) return memoryOpened
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []
  } catch {
    return memoryOpened
  }
}

function writeOpened(urls: string[]) {
  memoryOpened = urls
  try {
    window.sessionStorage.setItem(OPENED_KEY, JSON.stringify(urls))
  } catch {
    // 保存できなくても記事の閲覧を優先する
  }
}

/** 開いた記事を記録し、異なる記事の数を返す */
export function recordOpenedArticle(url: string): number {
  const urls = readOpened()
  if (urls.includes(url)) return urls.length
  const next = [...urls, url]
  writeOpened(next)
  return next.length
}

export function openedArticleCount(): number {
  return readOpened().length
}

export function clearOpenedArticles() {
  memoryOpened = []
  try {
    window.sessionStorage.removeItem(OPENED_KEY)
  } catch {
    // 消せなくても次のログインで上書きされる
  }
}

/** 状態照会・表示要求の結果から、次に要求してよい日時を覚える */
export function setNextEligibleAt(eligible: boolean, nextEligibleAt?: string | null) {
  if (eligible) {
    blockedUntil = null
    return
  }
  const at = nextEligibleAt ? Date.parse(nextEligibleAt) : Number.NaN
  blockedUntil = Number.isNaN(at) ? Number.POSITIVE_INFINITY : at
}

export function canRequestPresentation(now: number = Date.now()): boolean {
  return blockedUntil === null || now >= blockedUntil
}

/** ログイン成功・ログアウト・認証切れで呼び、前の利用者の状態を残さない */
export function clearFeedbackState() {
  clearOpenedArticles()
  blockedUntil = null
}

/** すべての通信が成功か失敗で終わるか、ms が過ぎるまで待つ */
export function settleWithin(
  promises: Promise<unknown>[],
  ms: number = CLICK_SETTLE_TIMEOUT_MS,
): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    void Promise.allSettled(promises).then(() => {
      clearTimeout(timer)
      resolve()
    })
  })
}
```

- [ ] **Step 4: 通ることを確認する**

Run: `cd frontend && npx vitest run src/lib/feedbackTracker.test.ts && npm run lint`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add frontend/src/lib/feedbackTracker.ts frontend/src/lib/feedbackTracker.test.ts
git commit -m "feat(frontend): アンケートを出す条件の記事数と再表示までの日時を覚える" -m "異なる記事を3件開いたときだけアンケートを要求し、60日以内は問い合わせを繰り返さないため。保存先が使えない環境でも閲覧を壊さず、ログインし直したときは前の利用者の状態を残さない（設計書 第6章）。" -m "Refs #84"
```

---

### Task 9: アンケートのポップアップ部品

**Files:**
- Create: `frontend/src/components/FeedbackDialog.tsx`
- Test: `frontend/src/components/FeedbackDialog.test.tsx`

**Interfaces:**
- Consumes: Task 7 の型と関数（`submitFeedbackOverall`、`completeFeedbackFollowup`、`dismissFeedback`、`UnauthorizedError`）。
- Produces:
  - `export type ShownPresentation = FeedbackPresentationResponse & { prompt_id: string; status: 'shown'; form: FeedbackForm }`
  - `export function isShownPresentation(p: FeedbackPresentationResponse): p is ShownPresentation`
  - `export function FeedbackDialog(props: { presentation: ShownPresentation; onUnauthorized: () => void; onClose: () => void })`

- [ ] **Step 1: 失敗する試験を書く**

`frontend/src/components/FeedbackDialog.test.tsx`:

```tsx
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { completeFeedbackFollowup, dismissFeedback, submitFeedbackOverall } from '@/api/endpoints'
import { FeedbackDialog, type ShownPresentation } from './FeedbackDialog'

vi.mock('@/api/endpoints', () => ({
  submitFeedbackOverall: vi.fn(),
  completeFeedbackFollowup: vi.fn(),
  dismissFeedback: vi.fn(),
}))

const PROMPT = '11111111-1111-4111-8111-111111111111'
const SUBMISSION = '22222222-2222-4222-8222-222222222222'

function presentation(overrides: Partial<ShownPresentation> = {}): ShownPresentation {
  return {
    eligible: true,
    status: 'shown',
    stage: 'overall',
    prompt_id: PROMPT,
    form: {
      title: 'おすすめ記事についてのアンケート',
      version: 1,
      questions: [
        { id: 10, key: 'overall', text: '今日のおすすめ記事は役に立ちましたか？', sort_order: 1, required: true },
        { id: 11, key: 'interest_match', text: '興味に合っていましたか？', sort_order: 2, required: true, display_if_question_id: 10, display_if_score_max: 2 },
        { id: 12, key: 'freshness', text: '新しさに満足しましたか？', sort_order: 3, required: true, display_if_question_id: 10, display_if_score_max: 2 },
        { id: 13, key: 'usability', text: '使いやすかったですか？', sort_order: 4, required: true, display_if_question_id: 10, display_if_score_max: 2 },
      ],
    },
    ...overrides,
  }
}

function renderDialog(p = presentation()) {
  const onClose = vi.fn()
  const onUnauthorized = vi.fn()
  render(<FeedbackDialog presentation={p} onClose={onClose} onUnauthorized={onUnauthorized} />)
  return { onClose, onUnauthorized }
}

describe('FeedbackDialog', () => {
  beforeEach(() => {
    vi.mocked(submitFeedbackOverall).mockReset()
    vi.mocked(completeFeedbackFollowup).mockReset()
    vi.mocked(dismissFeedback).mockReset().mockResolvedValue({ prompt_id: PROMPT, status: 'dismissed', stage: 'overall' })
  })
  afterEach(() => cleanup())

  test('最初は総合評価の1問だけを、意味の分かるラベル付きで出し、操作位置を移す', () => {
    renderDialog()
    expect(screen.getByRole('dialog', { name: 'おすすめ記事についてのアンケート' })).toBeInTheDocument()
    expect(screen.getByText('今日のおすすめ記事は役に立ちましたか？')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /5\s*とても満足/ })).toBeInTheDocument()
    expect(screen.queryByText('興味に合っていましたか？')).not.toBeInTheDocument()
    expect(screen.getByText(/アカウントに紐づけて保存し、サービス改善の分析に使います/)).toBeInTheDocument()
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  test('3〜5を選ぶと保存してお礼を出し、閉じても離脱を記録しない', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'completed', followup_required: false })
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /4\s*満足/ }))
    expect(await screen.findByText('ご回答ありがとうございました。')).toBeInTheDocument()
    expect(submitFeedbackOverall).toHaveBeenCalledWith({ prompt_id: PROMPT, question_id: 10, score: 4 })
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    expect(dismissFeedback).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  test('1〜2を選ぶと追加3問を同じ画面に出し、全部選ぶまで送信できない', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'partial', followup_required: true })
    vi.mocked(completeFeedbackFollowup).mockResolvedValue({ submission_id: SUBMISSION, status: 'completed', followup_required: false })
    renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /2\s*不満/ }))
    expect(await screen.findByText('興味に合っていましたか？')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: '送信' })
    expect(send).toBeDisabled()
    for (const name of ['興味に合っていましたか？', '新しさに満足しましたか？', '使いやすかったですか？']) {
      const group = screen.getByRole('group', { name })
      fireEvent.click(group.querySelector('input[value="3"]')!)
    }
    expect(send).toBeEnabled()
    fireEvent.click(send)
    expect(await screen.findByText('ご回答ありがとうございました。')).toBeInTheDocument()
    expect(completeFeedbackFollowup).toHaveBeenCalledWith(SUBMISSION, [
      { question_id: 11, score: 3 },
      { question_id: 12, score: 3 },
      { question_id: 13, score: 3 },
    ])
  })

  test('追加質問の保存に失敗しても選択を残して再送信でき、途中で閉じると離脱を記録する', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'partial', followup_required: true })
    vi.mocked(completeFeedbackFollowup).mockRejectedValue(new ApiError({ status: 500 }))
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /1\s*とても不満/ }))
    await screen.findByText('興味に合っていましたか？')
    for (const name of ['興味に合っていましたか？', '新しさに満足しましたか？', '使いやすかったですか？']) {
      fireEvent.click(screen.getByRole('group', { name }).querySelector('input[value="2"]')!)
    }
    fireEvent.click(screen.getByRole('button', { name: '送信' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: '興味に合っていましたか？' }).querySelector('input[value="2"]')).toBeChecked()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(dismissFeedback).toHaveBeenCalledWith(PROMPT)
    expect(onClose).toHaveBeenCalled()
  })

  test('総合評価の保存に失敗したら追加質問へ進まず、もう一度選べる', async () => {
    vi.mocked(submitFeedbackOverall).mockRejectedValueOnce(new ApiError({ status: 0 }, '接続できません'))
    renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /2\s*不満/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('接続できません')
    expect(screen.queryByText('興味に合っていましたか？')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /2\s*不満/ })).toBeEnabled())
  })

  test('認証切れはログイン画面へ戻す', async () => {
    vi.mocked(submitFeedbackOverall).mockRejectedValue(new UnauthorizedError())
    const { onUnauthorized } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /5\s*とても満足/ }))
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
  })

  test('閉じたときの記録が認証切れで失敗したら、ログイン画面へ戻す', async () => {
    vi.mocked(dismissFeedback).mockRejectedValue(new UnauthorizedError())
    const { onClose, onUnauthorized } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    expect(onClose).toHaveBeenCalled()
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
  })

  test('部分回答が保存済みの再送結果では、追加質問から始める', () => {
    renderDialog(presentation({ stage: 'followup', submission_id: SUBMISSION }))
    expect(screen.getByText('興味に合っていましたか？')).toBeInTheDocument()
  })

  test('Tabキーの移動はポップアップの中で回る', () => {
    renderDialog()
    const dialog = screen.getByRole('dialog')
    const focusables = dialog.querySelectorAll<HTMLElement>('button:not([disabled])')
    focusables[focusables.length - 1].focus()
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(document.activeElement).toBe(focusables[0])
  })
})
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd frontend && npx vitest run src/components/FeedbackDialog.test.tsx`
Expected: FAIL（モジュールが無い）

- [ ] **Step 3: 実装する**

`frontend/src/components/FeedbackDialog.tsx`:

```tsx
import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { UnauthorizedError } from '@/api/client'
import { completeFeedbackFollowup, dismissFeedback, submitFeedbackOverall } from '@/api/endpoints'
import type { FeedbackForm, FeedbackPresentationResponse } from '@/api/types'

export type ShownPresentation = FeedbackPresentationResponse & {
  prompt_id: string
  status: 'shown'
  form: FeedbackForm
}

export function isShownPresentation(p: FeedbackPresentationResponse): p is ShownPresentation {
  return p.eligible && p.status === 'shown' && !!p.prompt_id && !!p.form
}

const SCORES = [1, 2, 3, 4, 5] as const
const SCORE_LABELS: Record<number, string> = {
  1: 'とても不満',
  2: '不満',
  3: 'ふつう',
  4: '満足',
  5: 'とても満足',
}
const FOCUSABLE = 'button:not([disabled]), input:not([disabled])'

type Step = 'overall' | 'followup' | 'thanks'

interface FeedbackDialogProps {
  presentation: ShownPresentation
  onUnauthorized: () => void
  onClose: () => void
}

export function FeedbackDialog({ presentation, onUnauthorized, onClose }: FeedbackDialogProps) {
  const { form, prompt_id: promptId } = presentation
  const root = form.questions.find((q) => q.display_if_question_id === undefined)
  const resumed = presentation.stage === 'followup' && !!presentation.submission_id
  const [step, setStep] = useState<Step>(resumed ? 'followup' : 'overall')
  const [submissionId, setSubmissionId] = useState<string | null>(presentation.submission_id ?? null)
  const [overallScore, setOverallScore] = useState<number | null>(null)
  const [answers, setAnswers] = useState<Record<number, number>>({})
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus()
  }, [step])

  // 再開時は総合評価の値が手元に無い。部分回答は低評価のときだけできるので、追加質問をすべて出す
  const followups = form.questions.filter(
    (q) =>
      root !== undefined &&
      q.display_if_question_id === root.id &&
      (overallScore === null || overallScore <= (q.display_if_score_max ?? 0)),
  )
  const complete = followups.every((q) => !q.required || answers[q.id] !== undefined)

  const fail = (e: unknown) => {
    if (e instanceof UnauthorizedError) {
      onUnauthorized()
      return
    }
    setError(e instanceof Error ? e.message : '送信に失敗しました。もう一度お試しください。')
  }

  const close = () => {
    // 回答を終える前に閉じたら離脱として記録を試みる。届かなくても記事閲覧を優先するが、
    // 認証切れだけはログイン画面へ戻す
    if (step !== 'thanks') {
      void dismissFeedback(promptId).catch((e: unknown) => {
        if (e instanceof UnauthorizedError) onUnauthorized()
      })
    }
    onClose()
  }

  const chooseOverall = async (score: number) => {
    if (!root || sending) return
    setSending(true)
    setError(null)
    try {
      const res = await submitFeedbackOverall({ prompt_id: promptId, question_id: root.id, score })
      setOverallScore(score)
      if (res.followup_required) {
        setSubmissionId(res.submission_id)
        setStep('followup')
      } else {
        setStep('thanks')
      }
    } catch (e) {
      fail(e)
    } finally {
      setSending(false)
    }
  }

  const sendFollowup = async () => {
    if (!submissionId || !complete || sending) return
    setSending(true)
    setError(null)
    try {
      await completeFeedbackFollowup(
        submissionId,
        followups
          .filter((q) => answers[q.id] !== undefined)
          .map((q) => ({ question_id: q.id, score: answers[q.id] })),
      )
      setStep('thanks')
    } catch (e) {
      fail(e)
    } finally {
      setSending(false)
    }
  }

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      close()
      return
    }
    if (event.key !== 'Tab' || !dialogRef.current) return
    const items = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (items.length === 0) return
    const first = items[0]
    const last = items[items.length - 1]
    if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    } else if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/40 p-4 sm:items-center">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="feedback-title"
        onKeyDown={onKeyDown}
        className="max-h-[90vh] w-full max-w-md overflow-y-auto rounded-2xl bg-white p-6 shadow-xl"
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <h2 id="feedback-title" className="text-lg font-bold text-ink">
            {form.title}
          </h2>
          <button
            type="button"
            onClick={close}
            aria-label="閉じる"
            className="rounded-lg px-2 text-xl leading-none text-ink-muted hover:bg-slate-100"
          >
            ×
          </button>
        </div>

        {step === 'overall' && root && (
          <fieldset>
            <legend className="mb-3 text-sm font-bold text-ink">{root.text}</legend>
            <div className="grid grid-cols-5 gap-2">
              {SCORES.map((score) => (
                <button
                  key={score}
                  type="button"
                  disabled={sending}
                  onClick={() => void chooseOverall(score)}
                  className="flex flex-col items-center rounded-lg border border-slate-300 px-1 py-2 text-sm hover:border-brand-500 disabled:opacity-50"
                >
                  <span className="font-bold">{score}</span>
                  <span className="text-[11px] leading-tight text-ink-muted">{SCORE_LABELS[score]}</span>
                </button>
              ))}
            </div>
          </fieldset>
        )}

        {step === 'followup' && (
          <form
            onSubmit={(event) => {
              event.preventDefault()
              void sendFollowup()
            }}
            className="space-y-4"
          >
            {followups.map((q) => (
              <fieldset key={q.id} aria-label={q.text}>
                <legend className="mb-2 text-sm font-bold text-ink">{q.text}</legend>
                <div className="grid grid-cols-5 gap-1">
                  {SCORES.map((score) => (
                    <label key={score} className="flex flex-col items-center rounded-lg border border-slate-200 px-1 py-2 text-xs">
                      <input
                        type="radio"
                        name={`feedback-${q.id}`}
                        value={score}
                        checked={answers[q.id] === score}
                        onChange={() => setAnswers((prev) => ({ ...prev, [q.id]: score }))}
                      />
                      <span className="font-bold">{score}</span>
                      <span className="text-[11px] leading-tight text-ink-muted">{SCORE_LABELS[score]}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
            ))}
            <button
              type="submit"
              disabled={!complete || sending}
              className="w-full rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white hover:bg-brand-700 disabled:opacity-50"
            >
              送信
            </button>
          </form>
        )}

        {step === 'thanks' && (
          <p role="status" className="py-4 text-center text-sm text-ink">
            ご回答ありがとうございました。
          </p>
        )}

        {error && (
          <p role="alert" className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </p>
        )}

        {step !== 'thanks' && (
          <p className="mt-4 text-xs text-ink-muted">
            回答はあなたのアカウントに紐づけて保存し、サービス改善の分析に使います。
          </p>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: 通ることを確認する**

Run: `cd frontend && npx vitest run src/components/FeedbackDialog.test.tsx && npm run lint && npx tsc -b`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add frontend/src/components/FeedbackDialog.tsx frontend/src/components/FeedbackDialog.test.tsx
git commit -m "feat(frontend): アンケートのポップアップを追加する" -m "1問目の総合評価だけを1回の操作で答えられるようにし、低評価のときだけ追加質問へ進むため。キーボードだけでも操作でき、送信に失敗しても選んだ内容を残して送り直せるようにする（設計書 第7章）。" -m "Refs #84"
```

---

### Task 10: 記事一覧画面への組み込みと、ログイン・ログアウト時の消去

**Files:**
- Create: `frontend/src/lib/useUserFeedback.ts`
- Modify: `frontend/src/pages/ArticlesPage.tsx`、`frontend/src/pages/LoginPage.tsx:34-36`、`frontend/src/pages/SignupPage.tsx:71-73`
- Test: `frontend/src/pages/ArticlesPage.test.tsx`（モックと試験を追記）、`frontend/src/pages/LoginPage.test.tsx`（1件追記）

**Interfaces:**
- Consumes: Task 7〜9 の関数・部品。
- Produces: `useUserFeedback(onUnauthorized: () => void): { presentation: ShownPresentation | null; articleOpened: (url: string, click: Promise<unknown>) => void; close: () => void }`

- [ ] **Step 1: 失敗する試験を書く**

`frontend/src/pages/ArticlesPage.test.tsx` の `vi.mock('@/api/endpoints', ...)` を次に置き換える:

```tsx
vi.mock('@/api/endpoints', () => ({
  fetchFeed: vi.fn(),
  recordArticleClick: vi.fn(),
  logout: vi.fn(),
  fetchFeedbackStatus: vi.fn(),
  requestFeedbackPresentation: vi.fn(),
  submitFeedbackOverall: vi.fn(),
  completeFeedbackFollowup: vi.fn(),
  dismissFeedback: vi.fn(),
}))
```

import 行に `fetchFeedbackStatus, requestFeedbackPresentation` と `clearFeedbackState`（`@/lib/feedbackTracker`）を加え、`beforeEach` の中に追記:

```tsx
    clearFeedbackState()
    window.sessionStorage.clear()
    vi.mocked(fetchFeedbackStatus).mockReset().mockResolvedValue({ eligible: true })
    vi.mocked(requestFeedbackPresentation).mockReset().mockResolvedValue({ eligible: false, next_eligible_at: '2099-01-01T00:00:00Z' })
    vi.mocked(recordArticleClick).mockResolvedValue(undefined)
```

`describe('ArticlesPage', ...)` の末尾に追記:

```tsx
  function threeArticlesFeed() {
    return feed({
      qiita_articles: [article('Qiita', 'a', ['go']), article('Qiita', 'b', ['go'])],
      zenn_articles: [article('Zenn', 'c', ['go'])],
    })
  }

  test('異なる記事を3件開くまでは表示を要求せず、同じ記事は1件として数える', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('b'))
    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(3))
    expect(requestFeedbackPresentation).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('c'))
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    expect(vi.mocked(requestFeedbackPresentation).mock.calls[0][0]).toMatch(/^[0-9a-f-]{36}$/)
  })

  test('表示が許可されたらポップアップを出し、開封履歴を消す', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    vi.mocked(requestFeedbackPresentation).mockImplementation(async (promptId: string) => ({
      eligible: true,
      status: 'shown',
      stage: 'overall',
      prompt_id: promptId,
      form: { title: 'おすすめ記事についてのアンケート', version: 1, questions: [{ id: 10, key: 'overall', text: '今日のおすすめ記事は役に立ちましたか？', sort_order: 1, required: true }] },
    }))
    renderArticles()
    await screen.findByText('a')
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    expect(await screen.findByRole('dialog', { name: 'おすすめ記事についてのアンケート' })).toBeInTheDocument()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  test('次回表示可能日時より前は、記事を開いても再要求しない', async () => {
    vi.mocked(fetchFeedbackStatus).mockResolvedValue({ eligible: false, next_eligible_at: '2099-01-01T00:00:00Z' })
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    await waitFor(() => expect(fetchFeedbackStatus).toHaveBeenCalled())
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(3))
    expect(requestFeedbackPresentation).not.toHaveBeenCalled()
  })

  test('状態照会に失敗しても記事は読める', async () => {
    vi.mocked(fetchFeedbackStatus).mockRejectedValue(new ApiError({ status: 500 }))
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    expect(await screen.findByText('a')).toBeInTheDocument()
  })

  test('認証切れでログイン画面へ戻すときは、前の利用者の取得結果と判定記録を捨てる', async () => {
    vi.mocked(fetchFeed).mockRejectedValue(new UnauthorizedError())
    window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
    const client = renderArticles()
    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toBeUndefined()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  test('表示要求の結果が分からないときは開封履歴を消し、3件から数え直す', async () => {
    vi.mocked(requestFeedbackPresentation).mockRejectedValue(new ApiError({ status: 0 }))
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull())
  })
```

（記事カードは `<button onClick={() => onOpen(article)}>` なので、タイトルの文字をクリックすればボタンへ伝わる。）

`frontend/src/pages/LoginPage.test.tsx` の `describe('LoginPage', ...)` の中に追記:

```tsx
  test('成功すると、前の利用者のアンケート判定の記録を消す', async () => {
    window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
    vi.mocked(login).mockResolvedValue({ user })
    renderLogin()

    submit('alice', 'secret')

    expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })
```

- [ ] **Step 2: 失敗を確認する**

Run: `cd frontend && npx vitest run src/pages`
Expected: 新しい試験が FAIL（要求されない・ポップアップが出ない・消えない）

- [ ] **Step 3: フックを実装する**

`frontend/src/lib/useUserFeedback.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from 'react'
import { UnauthorizedError } from '@/api/client'
import { fetchFeedbackStatus, requestFeedbackPresentation } from '@/api/endpoints'
import { isShownPresentation, type ShownPresentation } from '@/components/FeedbackDialog'
import {
  canRequestPresentation,
  clearOpenedArticles,
  FEEDBACK_ARTICLE_THRESHOLD,
  openedArticleCount,
  recordOpenedArticle,
  setNextEligibleAt,
  settleWithin,
} from './feedbackTracker'

/**
 * 記事一覧でアンケートを出すかを判定する（設計書 第6章・第10章）。
 * 異なる記事を3件開き、数えた記事のクリック学習の通信が確定してから（最大10秒）、表示を要求する。
 */
export function useUserFeedback(onUnauthorized: () => void) {
  const [presentation, setPresentation] = useState<ShownPresentation | null>(null)
  const pendingClicks = useRef<Promise<unknown>[]>([])
  const requesting = useRef(false)
  const showing = useRef(false)
  const unauthorized = useRef(onUnauthorized)

  useEffect(() => {
    unauthorized.current = onUnauthorized
  }, [onUnauthorized])

  const maybeRequest = useCallback(async () => {
    if (requesting.current || showing.current) return
    if (openedArticleCount() < FEEDBACK_ARTICLE_THRESHOLD || !canRequestPresentation()) return
    requesting.current = true
    try {
      await settleWithin(pendingClicks.current)
      pendingClicks.current = []
      const promptId = crypto.randomUUID()
      let result
      try {
        result = await requestFeedbackPresentation(promptId)
      } catch (error) {
        // 結果が分からないので、IDを捨てて3件から数え直す
        clearOpenedArticles()
        if (error instanceof UnauthorizedError) unauthorized.current()
        return
      }
      if (!result.eligible) {
        setNextEligibleAt(false, result.next_eligible_at)
        return
      }
      if (isShownPresentation(result)) {
        clearOpenedArticles()
        showing.current = true
        setPresentation(result)
      }
      // すでに終わった表示の結果だけが返った場合は、履歴を残す
    } finally {
      requesting.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    fetchFeedbackStatus()
      .then((status) => {
        if (cancelled) return
        setNextEligibleAt(status.eligible, status.next_eligible_at)
        void maybeRequest()
      })
      .catch(() => {
        // 状態照会の失敗は記事閲覧を妨げない。表示要求の側でも同じ判定が行われる
      })
    return () => {
      cancelled = true
    }
  }, [maybeRequest])

  const articleOpened = useCallback(
    (url: string, click: Promise<unknown>) => {
      recordOpenedArticle(url)
      pendingClicks.current.push(click)
      void maybeRequest()
    },
    [maybeRequest],
  )

  const close = useCallback(() => {
    showing.current = false
    setPresentation(null)
  }, [])

  return { presentation, articleOpened, close }
}
```

- [ ] **Step 4: 記事一覧画面に組み込む**

`frontend/src/pages/ArticlesPage.tsx` を次のように変更する。

import に追加:

```tsx
import { useCallback, useEffect } from 'react'
import { FeedbackDialog } from '@/components/FeedbackDialog'
import { clearFeedbackState } from '@/lib/feedbackTracker'
import { useUserFeedback } from '@/lib/useUserFeedback'
```

（既存の `import { useEffect } from 'react'` は上の行に置き換える。）

`const sessionExpired = ...` の直後からの部分を次にする:

```tsx
  const sessionExpired = error instanceof UnauthorizedError

  // ログアウト・認証切れのどちらでも、利用者ごとの情報が次にログインする人へ見えないよう、
  // ブラウザ内の取得結果とアンケートの判定に使う記録を捨ててログイン画面へ移る
  const leaveSession = useCallback(() => {
    queryClient.removeQueries({ queryKey: authQueryKey })
    queryClient.removeQueries({ queryKey: FEED_QUERY_KEY })
    clearFeedbackState()
    navigate('/login', { replace: true })
  }, [queryClient, navigate])

  useEffect(() => {
    if (sessionExpired) leaveSession()
  }, [sessionExpired, leaveSession])

  const feedback = useUserFeedback(leaveSession)

  // クリック学習の送信。失敗しても閲覧体験を妨げないため、UIには出さない。
  // 二重に学習させないよう再送しない
  const clickMutation = useMutation({
    mutationFn: recordArticleClick,
    retry: false,
    onError: (clickError: Error) => {
      if (clickError instanceof UnauthorizedError) leaveSession()
    },
  })
```

（既存の `const leaveSession = () => {...}` は上の `useCallback` 版に置き換えて削除する。`logoutMutation` はそのまま `leaveSession` を使う。）`handleOpen` を次にする:

```tsx
  const handleOpen = (article: Article) => {
    // ポップアップブロックを避けるため、クリック直後に同期的に開く
    window.open(article.url, '_blank', 'noopener,noreferrer')
    // アンケートの表示判定は、このクリック学習の通信が確定してから行う。失敗も確定として扱う
    const click = clickMutation.mutateAsync(article.tags ?? []).catch(() => undefined)
    feedback.articleOpened(article.url, click)
  }
```

`</AppLayout>` の直前（`</div>` の後）に追加:

```tsx
      {feedback.presentation && (
        <FeedbackDialog
          presentation={feedback.presentation}
          onUnauthorized={leaveSession}
          onClose={feedback.close}
        />
      )}
```

- [ ] **Step 5: ログイン・会員登録の成功時に消す**

`frontend/src/pages/LoginPage.tsx` と `frontend/src/pages/SignupPage.tsx` の `onSuccess` の先頭に `clearFeedbackState()` を追加し、import に `import { clearFeedbackState } from '@/lib/feedbackTracker'` を加える:

```tsx
    onSuccess: (data) => {
      // 同じブラウザで前に使っていた利用者のアンケート判定の記録を残さない
      clearFeedbackState()
      queryClient.setQueryData(authQueryKey, data.user)
      navigate('/articles', { replace: true })
    },
```

- [ ] **Step 6: 画面全体の検証**

Run: `cd frontend && npm run lint && npm run test && npm run build`
Expected: すべて成功。

- [ ] **Step 7: 実際の画面で確かめる**

`.\dev.ps1` で起動し（`server\.env` と `frontend\.env` が必要）、ブラウザで次を確かめる。DBの移行は `cd server && go run ./cmd/migrate` で先に適用する。
1. 新しい利用者で登録 → 異なる記事を3件開く → ポップアップが出る。
2. 「2 不満」→ 追加3問 → 全部選ぶと送信できる → お礼。
3. もう一度3件開いてもポップアップは出ない（60日以内）。
4. スマートフォン幅（幅375px）で追加質問と送信ボタンが欠けない。
5. Tab で操作位置がポップアップの外に出ない。Esc で閉じる。

- [ ] **Step 8: コミット**

```bash
git add frontend/src/lib/useUserFeedback.ts frontend/src/pages/
git commit -m "feat(frontend): 記事を3件開いたらアンケートを出す" -m "記事を読んだ直後の利用者から、短い操作で評価を集めるため。記事を開く操作は待たせず、クリック学習の通信が確定してから（最大10秒）表示を判定する。ログインし直したときは前の利用者の判定記録を残さない（設計書 第6章・第8章）。" -m "Refs #84"
```

---

### Task 11: 資料の更新

**Files:**
- Create: `docs/BasicDesignSpecifications/API/Details/UserFeedback.md`
- Modify: `docs/BasicDesignSpecifications/API/ApiList.md`、`docs/BasicDesignSpecifications/DataBaseArchitecture.md`、`docs/BasicDesignSpecifications/Screen/Details/ArticleList.md`、`docs/BasicDesignSpecifications/FeaturesList.md`、`docs/RequirementsSpecification.md`、`docs/DocumentMap.md`、`TASKS.md`

- [ ] **Step 1: APIの資料を書く**

`docs/BasicDesignSpecifications/API/Details/UserFeedback.md` を、既存の `Details/Article.md` と同じ見出し構成（窓口ごとに「送るもの」「返るもの」「補足」）で作る。窓口は次の5つで、API IDは機能一覧 F-7-2 に合わせて `A-7-1`〜`A-7-5` とする。

| API ID | 名前 | 方式 | パス |
| --- | --- | --- | --- |
| A-7-1 | アンケートの表示可否の確認 | GET | `/api/v1/user-feedback/status` |
| A-7-2 | アンケートの表示要求 | POST | `/api/v1/user-feedback/presentations` |
| A-7-3 | 総合評価の保存 | POST | `/api/v1/user-feedback/submissions` |
| A-7-4 | 追加質問の回答の保存 | PUT | `/api/v1/user-feedback/submissions/{submission_id}` |
| A-7-5 | 回答せず閉じたことの保存 | POST | `/api/v1/user-feedback/dismissals` |

各窓口の項目は `server/openapi/openapi.json` の `Feedback*` スキーマから書き起こす。補足には、60日の再表示制限、同じIDの再送で二重に記録しないこと、HTTPステータス（401・404・409・422）の意味を、平易な日本語で書く。

`ApiList.md` の表に上の5行（本人確認「必要」、対応する機能「F-7-2」、状態「実装済み」、詳細は `UserFeedback.md` の各見出し）を追加し、資料の構成の表に `Details/UserFeedback.md` を足し、「現在の窓口は以上の8つ」を「13」に直す。

- [ ] **Step 2: DB・画面・機能の資料を更新する**

- `DataBaseArchitecture.md`: テーブル一覧とER図に6表を加え、「主要テーブル定義」に `3-5` として6表の列と役割、保持期間2年と削除の仕組みを書く（設計書 第9章・第14章から書き起こす）。
- `Screen/Details/ArticleList.md`: 「操作と遷移」に、異なる記事3件でアンケートのポップアップが出ること、低評価で追加質問へ進むこと、Esc・閉じるボタンで閉じられることを追記する。
- `FeaturesList.md` と `RequirementsSpecification.md`: F-7-2 利用者フィードバックを未実装から実装済みへ移す。
- `DocumentMap.md`: ツリーの `API/Details/` に `UserFeedback.md … アンケート` を足す（この計画書自体は計画書のPRで登録済み）。
- `TASKS.md`: 「アンケート機能（#84）」を完了扱いにする（既存の書き方に合わせる）。

- [ ] **Step 3: 確認する**

Run: `git diff --check && node --test scripts/*.test.mjs`
Expected: 空白の問題なし、試験 PASS。

- [ ] **Step 4: コミット**

```bash
git add docs/ TASKS.md
git commit -m "docs: アンケート機能の窓口・表・画面の資料を追加する" -m "実装したアンケート機能の窓口・DBの表・画面の動きを、仕様ファイル以外からも読めるようにするため。" -m "Refs #84"
```

---

## 完了前の検証（全タスク後）

```bash
cd server && test -z "$(gofmt -l .)" && go vet ./... && go test ./... -race -cover && go build ./cmd/api ./cmd/migrate ./cmd/openapi
cd frontend && npm run lint && npm run test && npm run build
node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs" scripts/*.test.mjs
```

DBが要る試験は `TEST_DATABASE_URL` が無いとスキップされる。スキップされた場合は、その旨を報告に書く。PRの本文は `Closes #84` とする。
