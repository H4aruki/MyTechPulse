-- +goose Up
-- 既存利用者は既定値のmemberになる。現行Python版はこの列を読まないため、そのまま動作する。
ALTER TABLE "user" ADD COLUMN role varchar(20) NOT NULL DEFAULT 'member';
ALTER TABLE "user" ADD CONSTRAINT user_role_check CHECK (role IN ('member', 'admin'));

-- +goose Down
-- 本番の切り戻しには使わない。開発環境で順方向を作り直すためだけに置く。
ALTER TABLE "user" DROP CONSTRAINT user_role_check;
ALTER TABLE "user" DROP COLUMN role;
