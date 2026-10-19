-- #240 読み取りだけの確認(本番で最初に流す)。ops/fix_initial_interest_240.sh --report から、
-- ops/sql/initial_interest_240_functions.sql の後に続けて流す。SELECTだけで、データは変えない。
-- 出すのは件数だけ。利用者名・タグ名などの中身は出さない(Issue・チャットへ貼れるように)。
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '120s';

\echo '== 全体'
SELECT (SELECT count(*) FROM "user") AS users,
       (SELECT count(*) FROM tag) AS tags,
       (SELECT count(*) FROM recommend) AS recommend_rows,
       (SELECT count(*) FROM recommend WHERE match_int = 0) AS zero_rows;

\echo '== 対象（delete_unselected=消す行、raise_legacy=10000に直す行）'
SELECT * FROM pg_temp.ii240_summary();

\echo '== 消す行の内訳（推定クリック回数ごと。上位5タグが変わる人数、上位5タグが埋まらず人気の記事で埋まる人数）'
WITH plan AS (
  SELECT u."user_ID" AS user_id, p.* FROM "user" u CROSS JOIN LATERAL pg_temp.ii240_plan(u."user_ID") p
  WHERE p.kind = 'delete_unselected'
), ranked AS (
  SELECT r."user_ID" AS user_id, r.match_int, lower(btrim(t.tag_name)) COLLATE "C" AS name, r."tag_ID" AS tag_id,
         EXISTS (SELECT 1 FROM plan p WHERE p.user_id = r."user_ID" AND p.tag_id = r."tag_ID") AS is_target
  FROM recommend r JOIN tag t USING ("tag_ID")
  WHERE r."user_ID" IN (SELECT DISTINCT user_id FROM plan)
), before AS (
  SELECT user_id, string_agg(tag_id::text, ',' ORDER BY rk) AS top FROM (
    SELECT user_id, tag_id, row_number() OVER (PARTITION BY user_id ORDER BY match_int DESC, name, tag_id) AS rk
    FROM ranked WHERE match_int > 0) x WHERE rk <= 5 GROUP BY user_id
), after AS (
  SELECT user_id, string_agg(tag_id::text, ',' ORDER BY rk) AS top, count(*) AS slots FROM (
    SELECT user_id, tag_id, row_number() OVER (PARTITION BY user_id ORDER BY match_int DESC, name, tag_id) AS rk
    FROM ranked WHERE match_int > 0 AND NOT is_target) x WHERE rk <= 5 GROUP BY user_id
), per_user AS (
  SELECT p.user_id, min(p.clicks) AS clicks, count(*) AS rows FROM plan p GROUP BY p.user_id
)
SELECT CASE WHEN pu.clicks = 0 THEN '0回' WHEN pu.clicks <= 3 THEN '1-3回' WHEN pu.clicks <= 10 THEN '4-10回' ELSE '11回以上' END AS clicks,
       count(*) AS users, sum(pu.rows) AS rows,
       count(*) FILTER (WHERE b.top IS DISTINCT FROM a.top) AS top5_changed,
       count(*) FILTER (WHERE COALESCE(a.slots, 0) < 5) AS popular_fill
FROM per_user pu LEFT JOIN before b USING (user_id) LEFT JOIN after a USING (user_id)
GROUP BY 1 ORDER BY min(pu.clicks);

\echo '== 対象にしないもの（参考）'
SELECT
  (SELECT count(*) FROM "user" u WHERE NOT EXISTS (SELECT 1 FROM recommend r WHERE r."user_ID" = u."user_ID")) AS users_without_rows,
  (SELECT count(*) FROM (
     SELECT r."user_ID" FROM recommend r GROUP BY r."user_ID"
     HAVING count(*) FILTER (WHERE r.match_int = 0) >= 20
        AND count(*) FILTER (WHERE r.match_int = 0) >= ALL (SELECT count(*) FROM recommend r2 WHERE r2."user_ID" = r."user_ID" GROUP BY r2.match_int)
   ) z) AS users_mostly_zero;

ROLLBACK;
