-- name: UpsertWebAuthnUser :one
-- 初回だけ乱数のハンドルを保存し、2回目以降は保存済みの値を返す(同時に呼ばれても1つに決まる)。
INSERT INTO auth_webauthn_user ("user_ID", user_handle) VALUES ($1, $2)
ON CONFLICT ("user_ID") DO UPDATE SET user_handle = auth_webauthn_user.user_handle
RETURNING user_handle;

-- name: FindUserIDByWebAuthnHandle :one
SELECT "user_ID" FROM auth_webauthn_user WHERE user_handle = $1;

-- name: LockUserForPasskey :one
SELECT "user_ID" FROM "user" WHERE "user_ID" = $1 FOR UPDATE;

-- name: CountPasskeysByUser :one
SELECT count(*) FROM auth_passkey WHERE "user_ID" = $1;

-- name: CreatePasskey :one
INSERT INTO auth_passkey ("user_ID", credential_id, public_key, sign_count, transports, aaguid, backup_eligible, backup_state, name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListPasskeysByUser :many
SELECT * FROM auth_passkey WHERE "user_ID" = $1 ORDER BY created_at, id;

-- name: FindPasskeyByCredentialID :one
SELECT * FROM auth_passkey WHERE credential_id = $1;

-- name: UpdatePasskeyUsage :execrows
-- 認証回数が増えたときだけ更新する(巻き戻りと同時ログインの競合を、DBの側で止める)。0のまま変わらない認証器(同期パスキー)だけは許す。
UPDATE auth_passkey SET sign_count = $2, backup_state = $3, last_used_at = $4
WHERE credential_id = $1 AND (sign_count < $2 OR (sign_count = 0 AND $2 = 0));

-- name: RenamePasskey :execrows
UPDATE auth_passkey SET name = $3 WHERE id = $1 AND "user_ID" = $2;

-- name: DeletePasskey :execrows
DELETE FROM auth_passkey WHERE id = $1 AND "user_ID" = $2;

-- name: CreateWebAuthnChallenge :exec
INSERT INTO auth_webauthn_challenge (id, purpose, "user_ID", session_data, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: ConsumeWebAuthnChallenge :one
-- 未使用かつ期限内のときだけ、使用済みにして返す(1回限り。同時に呼ばれても1回しか成功しない)。
UPDATE auth_webauthn_challenge SET used_at = $3
WHERE id = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3
RETURNING "user_ID", session_data;

-- name: DeleteExpiredWebAuthnChallenges :exec
DELETE FROM auth_webauthn_challenge WHERE expires_at <= $1 OR used_at IS NOT NULL;
