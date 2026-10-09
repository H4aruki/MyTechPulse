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
