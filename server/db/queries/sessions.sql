-- name: CreateSession :exec
INSERT INTO auth_session (token_hash, "user_ID", expires_at) VALUES ($1, $2, $3);

-- name: FindSessionUser :one
SELECT u."user_ID", u.user_name, u.role, s.expires_at
FROM auth_session s JOIN "user" u ON u."user_ID" = s."user_ID"
WHERE s.token_hash = $1 AND s.expires_at > $2;

-- name: DeleteSession :exec
DELETE FROM auth_session WHERE token_hash = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM auth_session WHERE expires_at <= $1;
