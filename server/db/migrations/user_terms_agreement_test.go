package migrations_test

import "testing"

func TestUserTermsAgreementTableAndConstraints(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, `INSERT INTO "user" (user_name, password) VALUES ('agreer', 'h')`)
	const agree = `INSERT INTO user_terms_agreement ("user_ID", terms_version)
		SELECT "user_ID", $1 FROM "user" WHERE user_name = 'agreer'`

	mustExec(t, db, agree, "2026-10-draft")
	if n := count(t, db, `SELECT count(*) FROM user_terms_agreement WHERE agreed_at <= now() AND agreed_at > now() - interval '1 minute'`); n != 1 {
		t.Fatalf("agreed_at の既定値が入らない: %d", n)
	}
	mustFail(t, db, "同じ利用者の同じ版", agree, "2026-10-draft")
	mustExec(t, db, agree, "2026-11")
	mustFail(t, db, "空の版", agree, "")
	mustFail(t, db, "存在しない利用者", `INSERT INTO user_terms_agreement ("user_ID", terms_version) VALUES (999999, 'v')`)
}

func TestUserTermsAgreementCascadesFromUser(t *testing.T) {
	db := migratedDatabase(t)
	mustExec(t, db, `INSERT INTO "user" (user_name, password) VALUES ('u1', 'h'), ('u2', 'h')`)
	mustExec(t, db, `INSERT INTO user_terms_agreement ("user_ID", terms_version) SELECT "user_ID", 'v1' FROM "user"`)
	mustExec(t, db, `DELETE FROM "user" WHERE user_name = 'u1'`)
	if n := count(t, db, `SELECT count(*) FROM user_terms_agreement`); n != 1 {
		t.Fatalf("user_terms_agreement = %d, want 1", n)
	}
}
