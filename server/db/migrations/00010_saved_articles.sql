-- +goose Up
-- 保存した記事（Issue #236 ステップ1）。利用者が記事を「保存」した記録を貯める表を足す。
-- 既存の表は変更しない。退会（user の削除）と記事の削除では、保存の行も連動して消える。
-- 記事の保持期間の削除は、保存されている記事を消さない（問い合わせ側で除く）。

CREATE TABLE saved_articles (
    "user_ID"  integer NOT NULL REFERENCES "user" ("user_ID") ON DELETE CASCADE,
    article_id bigint  NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("user_ID", article_id)
);
-- 一覧（保存の新しい順）用
CREATE INDEX saved_articles_user_created_idx ON saved_articles ("user_ID", created_at DESC, article_id DESC);
-- 記事の削除時に、保存の有無を引く用（保持期間の削除と、記事の削除の連動）
CREATE INDEX saved_articles_article_idx ON saved_articles (article_id);

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
DROP TABLE saved_articles;
