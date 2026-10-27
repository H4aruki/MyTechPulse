-- +goose Up
-- 保存による学習の記録（Issue #236 の決定）。保存による学習は、1人1記事につき一生に1回だけにする。
-- 解除で行が消える saved_articles とは別に残すための表。解除して再保存しても、ここに行があれば、もう一度は足さない。
-- 退会（user の削除）では連動して消える。記事を消したときも連動して消える（その記事はもう保存できないため）。
-- 新しい表を足すだけで、既存の表は変更しない。

CREATE TABLE saved_article_learnings (
    "user_ID"  integer NOT NULL REFERENCES "user" ("user_ID") ON DELETE CASCADE,
    article_id bigint  NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    learned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY ("user_ID", article_id)
);
-- 記事の削除の連動時に、この表の行を引く用
CREATE INDEX saved_article_learnings_article_idx ON saved_article_learnings (article_id);

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
DROP TABLE saved_article_learnings;
