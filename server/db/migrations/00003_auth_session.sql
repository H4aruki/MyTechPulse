-- +goose Up
-- ブラウザへ渡す乱数トークンのSHA-256ハッシュだけを保存する。平文トークンは保存しない。
CREATE TABLE auth_session (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_session_expires_at_idx ON auth_session (expires_at);
CREATE INDEX auth_session_user_id_idx ON auth_session ("user_ID");

-- +goose Down
-- 本番の切り戻しには使わない。既存の3表(user、tag、recommend)には触れない。
DROP TABLE auth_session;
