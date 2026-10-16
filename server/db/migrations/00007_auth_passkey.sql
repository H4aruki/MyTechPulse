-- +goose Up
-- パスキー(WebAuthn)の土台。既存の表には触れず、新しい表を3つ追加するだけにする。

-- WebAuthnの利用者ハンドル。連番の"user_ID"から作ると推測できるため、32バイトの乱数を別に持つ。
CREATE TABLE auth_webauthn_user (
    "user_ID" integer PRIMARY KEY REFERENCES "user"("user_ID") ON DELETE CASCADE,
    user_handle bytea NOT NULL UNIQUE CHECK (octet_length(user_handle) = 32),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 登録済みのパスキー。保存するのは公開鍵だけで、秘密鍵はサーバーに無い。
CREATE TABLE auth_passkey (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE CHECK (octet_length(credential_id) BETWEEN 1 AND 1023),
    public_key bytea NOT NULL CHECK (octet_length(public_key) > 0),
    sign_count bigint NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    transports text[] NOT NULL DEFAULT '{}',
    aaguid bytea NOT NULL DEFAULT decode(repeat('00', 16), 'hex') CHECK (octet_length(aaguid) = 16),
    backup_eligible boolean NOT NULL DEFAULT false,
    backup_state boolean NOT NULL DEFAULT false,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX auth_passkey_user_id_idx ON auth_passkey ("user_ID");

-- 登録・ログインの途中経過。idはブラウザへ渡す乱数トークンのSHA-256ハッシュ(平文は保存しない)。
-- 1回だけ使え(used_at)、期限(expires_at)を過ぎると使えない。複数台でも動くよう、メモリではなくDBに置く。
CREATE TABLE auth_webauthn_challenge (
    id bytea PRIMARY KEY CHECK (octet_length(id) = 32),
    purpose text NOT NULL CHECK (purpose IN ('register', 'login')),
    "user_ID" integer REFERENCES "user"("user_ID") ON DELETE CASCADE,
    session_data jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- 登録は利用者に結び付き、ログインは利用者名を受け取らないため結び付かない
    CONSTRAINT auth_webauthn_challenge_user_check CHECK (
        (purpose = 'register' AND "user_ID" IS NOT NULL) OR (purpose = 'login' AND "user_ID" IS NULL)
    )
);
CREATE INDEX auth_webauthn_challenge_expires_at_idx ON auth_webauthn_challenge (expires_at);

-- +goose Down
-- 本番の切り戻しには使わない。既存の表には触れない。
DROP TABLE auth_webauthn_challenge;
DROP TABLE auth_passkey;
DROP TABLE auth_webauthn_user;
