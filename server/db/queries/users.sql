-- name: FindUserByUsername :one
SELECT "user_ID", user_name, password, role FROM "user" WHERE user_name = $1;

-- name: CreateUser :one
INSERT INTO "user" (user_name, password, role) VALUES ($1, $2, $3)
RETURNING "user_ID", user_name, role;
