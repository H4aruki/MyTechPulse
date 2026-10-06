#!/usr/bin/env bash
# ops/compare_migration_state.sh の判定を、合成データを入れた使い捨てDBで確かめる。
# 使い方: bash ops/compare_migration_state_test.sh（Docker Desktopが起動していること）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/migration_state_fixture.sh
source "$repo_root/ops/tests/migration_state_fixture.sh"

trap ms_cleanup EXIT
ms_init

passed=0
run_case() {
    local name="$1"
    ms_reset
    "$name"
    ms_assert_no_leak "$name"
    passed=$((passed + 1))
    printf 'ok: %s\n' "$name"
}

# 3表に内容があり、migrationだけを適用した前後は一致する
case_same_content_after_migration() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_snap after
    ms_compare before after
    ms_expect_match "${FUNCNAME[0]}"
}

case_empty_tables_match() {
    ms_legacy_schema
    ms_snap before
    ms_migrate
    ms_snap after
    ms_compare before after
    ms_expect_match "${FUNCNAME[0]}"
}

# 物理的な行の並びが変わっても、内容が同じなら一致する
case_physical_row_order_ignored() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_sql >/dev/null <<'SQL'
UPDATE "user" SET user_name = user_name;
UPDATE tag SET tag_name = tag_name;
UPDATE recommend SET match_int = match_int;
SQL
    ms_migrate
    ms_snap after
    ms_compare before after
    ms_expect_match "${FUNCNAME[0]}"
}

case_empty_to_one_row_fails() {
    ms_legacy_schema
    ms_snap before
    ms_migrate
    ms_sql -c "INSERT INTO tag (tag_name) VALUES ('$MS_SYNTH_TAG_1')" >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

case_one_row_removed_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql -c 'DELETE FROM recommend WHERE ctid = (SELECT ctid FROM recommend LIMIT 1)' >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

# 件数を変えずに、1つの値だけを変える。それぞれ検出できる
change_one_value() {
    local name="$1" statement="$2" tables="$3"
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql -c "$statement" >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "$name" "$tables"
}
case_user_name_change_fails() {
    change_one_value "${FUNCNAME[0]}" "UPDATE \"user\" SET user_name = 'changed' WHERE user_name = '$MS_SYNTH_NAME_2'" 1
}
case_password_change_fails() {
    change_one_value "${FUNCNAME[0]}" "UPDATE \"user\" SET password = 'changed' WHERE user_name = '$MS_SYNTH_NAME_2'" 1
}
case_tag_name_change_fails() {
    change_one_value "${FUNCNAME[0]}" "UPDATE tag SET tag_name = 'changed' WHERE tag_name = '$MS_SYNTH_TAG_2'" 1
}
case_match_int_change_fails() {
    change_one_value "${FUNCNAME[0]}" 'UPDATE recommend SET match_int = match_int + 1 WHERE ctid = (SELECT ctid FROM recommend LIMIT 1)' 1
}

# IDだけを付け替える（関連する行のキーも一緒に直す）。FKの検査を外した接続で行う
case_user_id_change_with_related_keys_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql >/dev/null <<'SQL'
SET session_replication_role = replica;
UPDATE recommend SET "user_ID" = 100 WHERE "user_ID" = (SELECT min("user_ID") FROM "user");
UPDATE "user" SET "user_ID" = 100 WHERE "user_ID" = (SELECT min("user_ID") FROM "user");
SQL
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 2
}

case_tag_id_change_with_related_keys_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql >/dev/null <<'SQL'
SET session_replication_role = replica;
UPDATE recommend SET "tag_ID" = 100 WHERE "tag_ID" = (SELECT min("tag_ID") FROM tag);
UPDATE tag SET "tag_ID" = 100 WHERE "tag_ID" = (SELECT min("tag_ID") FROM tag);
SQL
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 2
}

# recommendの複合キーだけを変える（他の表は同じ）
case_recommend_composite_key_change_fails() {
    ms_legacy_schema
    ms_seed
    ms_sql -c "DELETE FROM recommend WHERE (\"user_ID\", \"tag_ID\") IN (SELECT max(\"user_ID\"), min(\"tag_ID\") FROM \"user\", tag)" >/dev/null
    ms_snap before
    ms_migrate
    ms_sql >/dev/null <<'SQL'
UPDATE recommend SET "tag_ID" = (SELECT min("tag_ID") FROM tag)
WHERE "user_ID" = (SELECT max("user_ID") FROM "user");
SQL
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

# 区切り文字・空文字・'null'という文字列・Unicodeで、連結の曖昧さが生まれない
ambiguous_user_pair() {
    local name="$1" a_name="$2" a_pass="$3" b_name="$4" b_pass="$5"
    ms_legacy_schema
    ms_sql >/dev/null <<SQL
INSERT INTO "user" (user_name, password) VALUES ('$a_name', '$a_pass');
SQL
    ms_snap before
    ms_migrate
    ms_sql >/dev/null <<SQL
UPDATE "user" SET user_name = '$b_name', password = '$b_pass';
SQL
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "$name" 1
}
case_boundary_shift_is_detected() {
    ambiguous_user_pair "${FUNCNAME[0]}" 'ab' 'c' 'a' 'bc'
}
case_delimiter_in_value_is_detected() {
    ambiguous_user_pair "${FUNCNAME[0]}" 'a|b' 'c' 'a' 'b|c'
}
case_empty_vs_null_text_is_detected() {
    ambiguous_user_pair "${FUNCNAME[0]}" 'x' '' 'x' 'null'
}
case_unicode_is_detected() {
    ambiguous_user_pair "${FUNCNAME[0]}" '日本語' 'p' '日本' '語p'
}

# 必須の制約が足りないsnapshotは失敗する
case_missing_role_check_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_goose_up 00003_auth_session.sql
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

case_missing_auth_session_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_goose_up 00002_user_role.sql
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

case_missing_foreign_key_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql -c 'ALTER TABLE recommend DROP CONSTRAINT "recommend_tag_ID_fkey"' >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

case_missing_unique_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql -c 'ALTER TABLE tag DROP CONSTRAINT tag_tag_name_key' >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

case_missing_constraint_in_before_fails() {
    ms_legacy_schema
    ms_seed
    ms_sql -c 'ALTER TABLE "user" DROP CONSTRAINT user_user_name_key' >/dev/null
    ms_snap before
    ms_migrate
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

# sequenceが既存IDの最大値より後ろにあると、次の登録がぶつかる
case_sequence_behind_max_id_fails() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_sql -c "SELECT setval(pg_get_serial_sequence('\"user\"', 'user_ID'), 1)" >/dev/null
    ms_snap after
    ms_compare before after
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

# cleanup後の比較では、採番が進んでいるだけなら許す。厳密な比較では不一致にする
write_then_cleanup() {
    ms_sql >/dev/null <<'SQL'
INSERT INTO "user" (user_name, password) VALUES ('__synthetic_write__', 'synthetic');
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 1 FROM "user" AS u, tag AS t WHERE u.user_name = '__synthetic_write__';
DELETE FROM "user" WHERE user_name = '__synthetic_write__';
SQL
}
case_sequence_advance_needs_flag() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    write_then_cleanup
    ms_snap cleanup
    ms_compare before cleanup
    ms_expect_mismatch "${FUNCNAME[0]}" 1
    ms_compare before cleanup --allow-sequence-advance
    ms_expect_match "${FUNCNAME[0]}(flag)"
}

case_sequence_regress_fails_even_with_flag() {
    ms_legacy_schema
    ms_seed
    write_then_cleanup
    ms_snap before
    ms_migrate
    ms_sql -c "SELECT setval(pg_get_serial_sequence('\"user\"', 'user_ID'), 2)" >/dev/null
    ms_snap after
    ms_compare before after --allow-sequence-advance
    ms_expect_mismatch "${FUNCNAME[0]}" 1
}

# 同じnonceのbefore/after/cleanupを通しで比べる。writeした状態は不一致、cleanup後は一致
case_before_after_cleanup_with_same_nonce() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    ms_snap after
    ms_compare before after
    ms_expect_match "${FUNCNAME[0]}(after)"
    ms_sql >/dev/null <<'SQL'
INSERT INTO "user" (user_name, password) VALUES ('__synthetic_write__', 'synthetic');
SQL
    ms_snap written
    ms_compare before written
    ms_expect_mismatch "${FUNCNAME[0]}(written)" 1
    ms_sql -c "DELETE FROM \"user\" WHERE user_name = '__synthetic_write__'" >/dev/null
    ms_snap cleanup
    ms_compare before cleanup --allow-sequence-advance
    ms_expect_match "${FUNCNAME[0]}(cleanup)"
}

# 別のnonceで作ったsnapshotは、内容が同じでも拒否する（不一致ではなく検査不能）
case_different_nonce_is_rejected() {
    ms_legacy_schema
    ms_seed
    ms_snap before
    ms_migrate
    rm -f -- "$MS_TMP/nonce"
    ms_snap after
    [ "$MS_SNAP_STATUS" -eq 0 ] || ms_fail "${FUNCNAME[0]}: 2回目のsnapshotが失敗"
    ms_compare before after
    ms_expect_unable "${FUNCNAME[0]}"
    grep -q 'E_RUN' "$MS_TMP/cmp.err" || ms_fail "${FUNCNAME[0]}: 拒否理由がE_RUNでない"
}

# 入力が不正なら、一致とも不一致とも言わず検査不能にする
case_invalid_inputs_are_unable() {
    ms_legacy_schema
    ms_snap before
    printf 'not json\n' >"$MS_TMP/broken.json"
    ms_compare before broken
    ms_expect_unable "${FUNCNAME[0]}(broken json)"
    printf '{"schema_version":1}\n{"x":1}\n' >"$MS_TMP/multi.json"
    ms_compare before multi
    ms_expect_unable "${FUNCNAME[0]}(multi line)"
    printf '{"schema_version":2,"run_id":"x"}\n' >"$MS_TMP/future.json"
    ms_compare before future
    ms_expect_unable "${FUNCNAME[0]}(schema)"
    : >"$MS_TMP/empty.json"
    ms_compare before empty
    ms_expect_unable "${FUNCNAME[0]}(empty)"
    ms_compare before missing
    ms_expect_unable "${FUNCNAME[0]}(missing file)"
    set +e
    bash "$repo_root/ops/compare_migration_state.sh" "$MS_TMP/before.json" "$MS_TMP/before.json" --unknown >/dev/null 2>&1
    local status=$?
    set -e
    [ "$status" -eq 2 ] || ms_fail "${FUNCNAME[0]}: 未知のflagを拒否しない"
}

# DBへ接続できなくても、接続情報や値を出さずに検査不能で止まる
case_unreachable_database_is_unable() {
    ms_legacy_schema
    ms_snap before
    set +e
    MTP_SNAPSHOT_DB=mytechpulse_no_such_database \
        bash "$repo_root/ops/compare_migration_state.sh" "$MS_TMP/before.json" "$MS_TMP/before.json" \
        >"$MS_TMP/cmp.out" 2>"$MS_TMP/cmp.err"
    local status=$?
    set -e
    cat "$MS_TMP/cmp.out" "$MS_TMP/cmp.err" >>"$MS_PUBLIC_LOG"
    [ "$status" -eq 2 ] || ms_fail "${FUNCNAME[0]}: 終了コード $status"
    [ ! -s "$MS_TMP/cmp.out" ] || ms_fail "${FUNCNAME[0]}: 標準出力がある"
    grep -q '^compare: unable (E_DB)$' "$MS_TMP/cmp.err" || ms_fail "${FUNCNAME[0]}: 固定の失敗文でない"
    [ "$(wc -l <"$MS_TMP/cmp.err")" -eq 1 ] || ms_fail "${FUNCNAME[0]}: 標準エラーが1行でない"
}

# サーバーがstatementを記録する設定だと、COPYで渡すデータが残りうるため、比較を始めない
case_server_logging_blocks_compare() {
    ms_legacy_schema
    ms_snap before
    ms_sql -c "ALTER DATABASE \"$MS_DB\" SET log_statement = 'all'" >/dev/null
    ms_compare before before
    ms_expect_unable "${FUNCNAME[0]}"
    grep -q 'E_LOGGING' "$MS_TMP/cmp.err" || ms_fail "${FUNCNAME[0]}: 拒否理由がE_LOGGINGでない"
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
    run_case "$case_name"
done

printf 'OK: %s compare cases passed\n' "$passed"
