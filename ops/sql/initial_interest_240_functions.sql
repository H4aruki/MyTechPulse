-- #240 既存の利用者の興味度を、新しい初期値のルールにそろえる一回だけの作業で使う関数。
-- 呼び出しは ops/fix_initial_interest_240.sh から行う。このファイルを単独で流しても、データは変わらない。
--
-- ここで作るのは、この接続の間だけ存在する一時的な関数(pg_temp)だけ。表や永続的な関数は作らない。
-- psql のメタコマンドは使わない(Goの試験 server/internal/store/initial_interest_240_ops_integration_test.go も同じファイルを流す)。
--
-- 対象は2種類。
--   delete_unselected: #203 の後に登録した利用者の「登録時に選ばず、一度も押していないタグ」の行。消す。
--   raise_legacy     : #203 より前に登録し、一度もクリックしていない利用者(行の値がすべて1)の行。10000(1.0)に直す。
--
-- delete_unselected の見分け方(#240 の調査コメント。2万人のシミュレーションで誤り0件):
--   - 登録時、選ばなかったタグには8000が入り、クリックのたびに全行が floor(v*8/10) になる。
--     そのため、押していない未選択の行は、利用者ごとにすべて同じ値 v(8000 を N 回減衰した値)になる。
--   - 利用者ごとに最も多い値(同数なら大きい方)を v とし、v が 8000 からの減衰列にあり、20行以上あるときだけ対象にする。
--   - 登録より後にできたタグは対象にしない。境目は「その利用者が(正規化した名前で)行を持たない、最も小さい tag_ID」。
--   - v = 0(おおむね36回以上クリック)は対象にしない。値0の行は、選んだタグや昔押したタグとも見分けられない。
--     値0は行が無いのと同じ扱い(フィードの上位5タグに入らず、点数にも効かない)なので、残しても動きは変わらない。
--   - 行の値がすべて1の利用者(raise_legacy の対象)は、v=1 に見えても消さない(#203 より前に20個以上選んだ人の、選んだタグ)。
-- raise_legacy の見分け方(#240 の決定):
--   - #203 より前の登録では、選んだタグが1、未選択の行は無い。1回でもクリックすると1は0になり、押したタグは2000以上になる。
--   - そのため、行が1件以上あり、すべて1の利用者だけを対象にする。

CREATE OR REPLACE FUNCTION pg_temp.ii240_plan(p_user integer)
RETURNS TABLE(kind text, tag_id integer, match_int integer, clicks integer)
LANGUAGE sql STABLE AS $fn$
  WITH RECURSIVE seq(n, v) AS (
    SELECT 0, 8000 UNION ALL SELECT n + 1, (v * 8) / 10 FROM seq WHERE n < 60
  ), seqv AS (
    SELECT s.v, min(s.n) AS clicks FROM seq s WHERE s.v > 0 GROUP BY s.v
  ), rows AS (
    SELECT r."tag_ID" AS tag_id, r.match_int FROM recommend r WHERE r."user_ID" = p_user
  ), legacy AS (
    SELECT count(*) > 0 AND bool_and(x.match_int = 1) AS yes FROM rows x
  ), mode AS (
    SELECT x.match_int AS v, count(*) AS c FROM rows x
    GROUP BY x.match_int ORDER BY count(*) DESC, x.match_int DESC LIMIT 1
  ), cohort AS (
    SELECT m.v, s.clicks FROM mode m JOIN seqv s ON s.v = m.v
    WHERE m.c >= 20 AND NOT (SELECT l.yes FROM legacy l)
  ), boundary AS (
    SELECT COALESCE((
      SELECT min(t."tag_ID") FROM tag t
      WHERE NOT EXISTS (
        SELECT 1 FROM recommend r JOIN tag rt ON rt."tag_ID" = r."tag_ID"
        WHERE r."user_ID" = p_user AND lower(btrim(rt.tag_name)) = lower(btrim(t.tag_name))
      )
    ), 2147483647) AS first_gap
    WHERE EXISTS (SELECT 1 FROM cohort)
  )
  SELECT 'delete_unselected'::text, x.tag_id, x.match_int, c.clicks
  FROM rows x CROSS JOIN cohort c CROSS JOIN boundary b
  WHERE x.match_int = c.v AND x.tag_id < b.first_gap
  UNION ALL
  SELECT 'raise_legacy'::text, x.tag_id, x.match_int, 0
  FROM rows x CROSS JOIN legacy l
  WHERE l.yes
$fn$;

-- 1人分を直す。呼び出し側のトランザクションの中で動く。
-- クリックの記録(server/db/queries/recommendations.sql の LockUserForRecommendation)と同じ行ロックを先に取り、
-- ロックを取った後で対象を数え直すので、実行中のクリックとぶつからない。
CREATE OR REPLACE FUNCTION pg_temp.ii240_apply(p_user integer)
RETURNS TABLE(deleted bigint, raised bigint)
LANGUAGE plpgsql AS $fn$
BEGIN
  deleted := 0;
  raised := 0;
  PERFORM 1 FROM "user" WHERE "user_ID" = p_user FOR UPDATE;
  IF NOT FOUND THEN
    RETURN NEXT;
    RETURN;
  END IF;
  -- 次の2つの文は、ロックを取った後の新しいスナップショットで対象を決める(READ COMMITTED)
  DELETE FROM recommend r
  USING pg_temp.ii240_plan(p_user) p
  WHERE p.kind = 'delete_unselected' AND r."user_ID" = p_user AND r."tag_ID" = p.tag_id AND r.match_int = p.match_int;
  GET DIAGNOSTICS deleted = ROW_COUNT;
  UPDATE recommend r SET match_int = 10000
  FROM pg_temp.ii240_plan(p_user) p
  WHERE p.kind = 'raise_legacy' AND r."user_ID" = p_user AND r."tag_ID" = p.tag_id AND r.match_int = 1;
  GET DIAGNOSTICS raised = ROW_COUNT;
  RETURN NEXT;
END
$fn$;

-- 全員を1人ずつ直す。1人ごとに確定(COMMIT)するので、途中で止めても、済んだ人の分だけが反映される。
-- 何度実行しても結果は同じ(2回目は対象が0件)。CALL はトランザクションの外で呼ぶ。
CREATE OR REPLACE PROCEDURE pg_temp.ii240_run()
LANGUAGE plpgsql AS $fn$
DECLARE
  uid integer;
  d bigint;
  r bigint;
  users_changed bigint := 0;
  total_deleted bigint := 0;
  total_raised bigint := 0;
BEGIN
  FOR uid IN SELECT u."user_ID" FROM "user" u ORDER BY u."user_ID" LOOP
    SELECT a.deleted, a.raised INTO d, r FROM pg_temp.ii240_apply(uid) a;
    COMMIT;
    IF d > 0 OR r > 0 THEN
      users_changed := users_changed + 1;
      total_deleted := total_deleted + d;
      total_raised := total_raised + r;
    END IF;
  END LOOP;
  RAISE NOTICE 'ii240 done: users_changed=% deleted_unselected_rows=% raised_legacy_rows=%',
    users_changed, total_deleted, total_raised;
END
$fn$;

-- 対象の件数(読み取りだけ)。実行前の確認と、実行後に0件になったことの確認に使う。
CREATE OR REPLACE FUNCTION pg_temp.ii240_summary()
RETURNS TABLE(kind text, users bigint, rows bigint)
LANGUAGE sql STABLE AS $fn$
  WITH plan AS (
    SELECT u."user_ID" AS user_id, p.kind FROM "user" u CROSS JOIN LATERAL pg_temp.ii240_plan(u."user_ID") p
  ), kinds(kind) AS (VALUES ('delete_unselected'::text), ('raise_legacy'::text))
  SELECT k.kind, count(DISTINCT p.user_id), count(p.user_id)
  FROM kinds k LEFT JOIN plan p ON p.kind = k.kind
  GROUP BY k.kind ORDER BY k.kind
$fn$;
