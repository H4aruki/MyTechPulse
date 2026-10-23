-- +goose Up
-- 保存した記事の学習を遅らせる（Issue #236 決めごと5）。誤タップで保存しても、すぐ解除すれば学習に影響しないようにする。
-- learned_at は、その保存を興味度へ反映した時刻。NULL は未反映。保存から10秒たった行を、次に興味度を読む・更新するときに反映する。
-- 00010 はまだ本番に入っていないので、既存の行（NULL のまま）の扱いは気にしない。
-- 既存の表に NULL を許す列を足すだけで、直前の版の書き込みは失敗しない。

ALTER TABLE saved_articles ADD COLUMN learned_at timestamptz;
-- 未反映の行だけを、利用者ごと・保存の古い順に引く用
CREATE INDEX saved_articles_unlearned_idx ON saved_articles ("user_ID", created_at) WHERE learned_at IS NULL;

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
DROP INDEX saved_articles_unlearned_idx;
ALTER TABLE saved_articles DROP COLUMN learned_at;
