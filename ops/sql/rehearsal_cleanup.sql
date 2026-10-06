-- リハーサルのsmokeが作った合成利用者と、その関連行だけを消す（#126）。
-- 使い方: psql -v smoke_user=<合成利用者名> -v tag_max=<smoke前のtag_IDの最大値> < このfile
-- 既存の利用者・tag・興味度には触れない。消す対象は次の3つだけ。
--   1. 名前が完全に一致する合成利用者1人
--   2. その利用者の興味度と認証セッション
--   3. smokeの間に新しく増え、他の誰も使っていないtag（ID が tag_max より大きいもの）
-- 合成利用者がちょうど1人でなければ、何も消さずに失敗する。
\set ON_ERROR_STOP on
BEGIN;

-- 0で割るのは、条件が合わないときに失敗させるため（定数の 1 / 0 は計画時に評価されて常に失敗する）
SELECT 1 / CASE WHEN count(*) = 1 THEN 1 ELSE 0 END
FROM public."user"
WHERE user_name = :'smoke_user' AND user_name ~ '^rehearsal-smoke-[0-9a-f]{12}$';

DELETE FROM public.recommend
WHERE "user_ID" IN (SELECT "user_ID" FROM public."user" WHERE user_name = :'smoke_user');

DELETE FROM public.auth_session
WHERE "user_ID" IN (SELECT "user_ID" FROM public."user" WHERE user_name = :'smoke_user');

DELETE FROM public."user" WHERE user_name = :'smoke_user';

DELETE FROM public.tag AS t
WHERE t."tag_ID" > :tag_max
  AND NOT EXISTS (SELECT 1 FROM public.recommend AS r WHERE r."tag_ID" = t."tag_ID");

COMMIT;
