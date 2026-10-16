package migrations_test

import "testing"

func TestPasskeySchema(t *testing.T) {
	db := newIsolatedDatabase(t)
	applyAll(t, db)
	assertForeignKey(t, db, `auth_webauthn_user`, `user_ID`, `user`, `user_ID`, `CASCADE`)
	assertForeignKey(t, db, `auth_passkey`, `user_ID`, `user`, `user_ID`, `CASCADE`)
	assertForeignKey(t, db, `auth_webauthn_challenge`, `user_ID`, `user`, `user_ID`, `CASCADE`)
	assertColumn(t, db, `auth_webauthn_user`, `user_handle`, `bytea`, false)
	assertColumn(t, db, `auth_passkey`, `credential_id`, `bytea`, false)
	assertColumn(t, db, `auth_passkey`, `public_key`, `bytea`, false)
	assertColumn(t, db, `auth_passkey`, `sign_count`, `bigint`, false)
	assertColumn(t, db, `auth_passkey`, `transports`, `ARRAY`, false)
	assertColumn(t, db, `auth_passkey`, `last_used_at`, `timestamp with time zone`, true)
	assertColumn(t, db, `auth_webauthn_challenge`, `session_data`, `jsonb`, false)
	assertColumn(t, db, `auth_webauthn_challenge`, `user_ID`, `integer`, true)
	assertColumn(t, db, `auth_webauthn_challenge`, `used_at`, `timestamp with time zone`, true)
	assertIndex(t, db, `auth_passkey`, `auth_passkey_user_id_idx`)
	assertIndex(t, db, `auth_webauthn_challenge`, `auth_webauthn_challenge_expires_at_idx`)
}

func TestPasskeySchemaConstraintsAndCascade(t *testing.T) {
	db := newIsolatedDatabase(t)
	applyAll(t, db)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('a', 'h')`)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('b', 'h')`)

	handle := `decode(repeat('ab', 32), 'hex')`
	mustExec(t, db, `INSERT INTO auth_webauthn_user("user_ID", user_handle) VALUES (1, `+handle+`)`)
	rejected := func(cases map[string]string) {
		t.Helper()
		for name, q := range cases {
			if _, err := db.Exec(q); err == nil {
				t.Fatalf("%s must be rejected", name)
			}
		}
	}
	rejected(map[string]string{
		"同じ利用者の2件目":    `INSERT INTO auth_webauthn_user("user_ID", user_handle) VALUES (1, decode(repeat('cd', 32), 'hex'))`,
		"同じハンドル":       `INSERT INTO auth_webauthn_user("user_ID", user_handle) VALUES (2, ` + handle + `)`,
		"32バイトでないハンドル": `INSERT INTO auth_webauthn_user("user_ID", user_handle) VALUES (2, '\x00')`,
		"存在しない利用者":     `INSERT INTO auth_webauthn_user("user_ID", user_handle) VALUES (99, decode(repeat('ef', 32), 'hex'))`,
	})

	mustExec(t, db, `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (1, '\x0102', '\x03', 'iPhone')`)
	var signCount int64
	var backupEligible, backupState bool
	if err := db.QueryRow(`SELECT sign_count, backup_eligible, backup_state FROM auth_passkey`).Scan(&signCount, &backupEligible, &backupState); err != nil ||
		signCount != 0 || backupEligible || backupState {
		t.Fatalf("defaults: %d %v %v %v", signCount, backupEligible, backupState, err)
	}
	rejected(map[string]string{
		"認証情報IDの重複":      `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (2, '\x0102', '\x03', 'x')`,
		"空の認証情報ID":       `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (2, '\x', '\x03', 'x')`,
		"空の公開鍵":          `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (2, '\x04', '\x', 'x')`,
		"空の名前":           `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (2, '\x05', '\x03', '')`,
		"65文字の名前":        `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name) VALUES (2, '\x06', '\x03', repeat('a', 65))`,
		"負の認証回数":         `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name, sign_count) VALUES (2, '\x07', '\x03', 'x', -1)`,
		"16バイトでないAAGUID": `INSERT INTO auth_passkey("user_ID", credential_id, public_key, name, aaguid) VALUES (2, '\x08', '\x03', 'x', '\x00')`,
	})

	id := `decode(repeat('11', 32), 'hex')`
	mustExec(t, db, `INSERT INTO auth_webauthn_challenge(id, purpose, "user_ID", session_data, expires_at)
		VALUES (`+id+`, 'register', 1, '{}', now() + interval '5 minutes')`)
	mustExec(t, db, `INSERT INTO auth_webauthn_challenge(id, purpose, session_data, expires_at)
		VALUES (decode(repeat('22', 32), 'hex'), 'login', '{}', now() + interval '5 minutes')`)
	rejected(map[string]string{
		"idの重複":      `INSERT INTO auth_webauthn_challenge(id, purpose, "user_ID", session_data, expires_at) VALUES (` + id + `, 'register', 1, '{}', now())`,
		"不正な種別":      `INSERT INTO auth_webauthn_challenge(id, purpose, session_data, expires_at) VALUES (decode(repeat('33', 32), 'hex'), 'other', '{}', now())`,
		"利用者のいない登録":  `INSERT INTO auth_webauthn_challenge(id, purpose, session_data, expires_at) VALUES (decode(repeat('44', 32), 'hex'), 'register', '{}', now())`,
		"利用者付きのログイン": `INSERT INTO auth_webauthn_challenge(id, purpose, "user_ID", session_data, expires_at) VALUES (decode(repeat('55', 32), 'hex'), 'login', 1, '{}', now())`,
		"32バイトでないid": `INSERT INTO auth_webauthn_challenge(id, purpose, session_data, expires_at) VALUES ('\x00', 'login', '{}', now())`,
	})

	mustExec(t, db, `DELETE FROM "user" WHERE "user_ID" = 1`)
	for _, table := range []string{"auth_webauthn_user", "auth_passkey"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 0 {
			t.Fatalf("%s must cascade on user delete, got %d", table, n)
		}
	}
	// 登録のチャレンジは利用者と一緒に消え、利用者に結び付かないログインのチャレンジは残る
	if n := count(t, db, `SELECT count(*) FROM auth_webauthn_challenge`); n != 1 {
		t.Fatalf("challenges after cascade = %d, want 1", n)
	}
}
