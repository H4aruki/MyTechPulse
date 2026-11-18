-- +goose Up
-- 利用規約・プライバシーポリシーへの同意の記録（Issue #185）。同意した日時と版を、あとから確かめられるようにする。
-- 規約とプライバシーポリシーは1つの版で扱う。会員登録のトランザクションの中で、利用者の作成と同時に1行を足す。
-- 退会（user の削除）では連動して消える。新しい表を足すだけで、既存の表は変更しない。
-- 登録済みの利用者には行がない（再同意は別に決める）。

CREATE TABLE user_terms_agreement (
    "user_ID"     integer NOT NULL REFERENCES "user" ("user_ID") ON DELETE CASCADE,
    terms_version text NOT NULL CHECK (char_length(terms_version) BETWEEN 1 AND 64),
    agreed_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("user_ID", terms_version)
);

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
DROP TABLE user_terms_agreement;
