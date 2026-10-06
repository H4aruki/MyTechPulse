#!/usr/bin/env bash
# ops/sql/rehearsal_cleanup.sql が、合成利用者とその関連行だけを消し、
# 後始末後に3表の内容が元と一致することを、合成データを入れた使い捨てDBで確かめる。
# 使い方: bash ops/rehearsal_cleanup_test.sh（Docker Desktopが起動していること）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/migration_state_fixture.sh
source "$repo_root/ops/tests/migration_state_fixture.sh"
cleanup_sql="$repo_root/ops/sql/rehearsal_cleanup.sql"

trap ms_cleanup EXIT
ms_init

SMOKE_USER='rehearsal-smoke-0123456789ab'

passed=0
run_case() {
    local name="$1"
    ms_reset
    "$name"
    ms_assert_no_leak "$name"
    passed=$((passed + 1))
    printf 'ok: %s\n' "$name"
}

# 後始末SQLを流す。終了コードを MS_CLEAN_STATUS へ入れる
MS_CLEAN_STATUS=0
run_cleanup() {
    local user="$1" tag_max="$2"
    set +e
    ms_sql -v "smoke_user=$user" -v "tag_max=$tag_max" <"$cleanup_sql" >"$MS_TMP/clean.out" 2>"$MS_TMP/clean.err"
    MS_CLEAN_STATUS=$?
    set -e
}

count() {
    ms_sql -At -c "SELECT count(*) FROM $1"
}

# 移行後・smoke前の状態を作り、その時点のsnapshotとtag最大IDを控える
prepare_baseline() {
    ms_legacy_schema
    ms_seed
    ms_migrate
    ms_snap before
    TAG_MAX="$(ms_sql -At -c 'SELECT coalesce(max("tag_ID"), 0) FROM tag')"
}

# smokeが書くものを真似る: 合成利用者・認証セッション・興味度、新しいtag、既存tagの興味度
simulate_smoke_writes() {
    ms_sql >/dev/null <<SQL
INSERT INTO "user" (user_name, password) VALUES ('$SMOKE_USER', 'synthetic-smoke-hash');
INSERT INTO tag (tag_name) VALUES ('smoke-only-tag');
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 100
FROM "user" AS u CROSS JOIN tag AS t WHERE u.user_name = '$SMOKE_USER';
INSERT INTO auth_session (token_hash, "user_ID", expires_at)
SELECT decode(repeat('ab', 32), 'hex'), "user_ID", now() + interval '1 hour' FROM "user" WHERE user_name = '$SMOKE_USER';
SQL
}

case_cleanup_restores_content_but_sequences_advance() {
    prepare_baseline
    simulate_smoke_writes
    ms_snap after-write
    # 合成writeの差分は検出される
    ms_compare before after-write
    [ "$MS_CMP_STATUS" -eq 1 ] || ms_fail "${FUNCNAME[0]}: 合成writeの差分が検出されない"

    run_cleanup "$SMOKE_USER" "$TAG_MAX"
    [ "$MS_CLEAN_STATUS" -eq 0 ] || ms_fail "${FUNCNAME[0]}: 後始末が失敗した: $(cat "$MS_TMP/clean.err")"
    [ "$(count '"user"')" -eq 2 ] || ms_fail "${FUNCNAME[0]}: 利用者数が元に戻っていない"
    [ "$(count tag)" -eq 2 ] || ms_fail "${FUNCNAME[0]}: tag数が元に戻っていない"
    [ "$(count recommend)" -eq 4 ] || ms_fail "${FUNCNAME[0]}: 興味度の数が元に戻っていない"
    [ "$(count auth_session)" -eq 0 ] || ms_fail "${FUNCNAME[0]}: 認証セッションが残っている"

    ms_snap after-cleanup
    # 採番を進めたままなので、許可しない比較は不一致になる
    ms_compare before after-cleanup
    [ "$MS_CMP_STATUS" -eq 1 ] || ms_fail "${FUNCNAME[0]}: 採番が進んだのに通常比較が一致した"
    # 採番の前進だけを許す比較なら、3表の内容は元と一致する
    ms_compare before after-cleanup --allow-sequence-advance
    ms_expect_match "${FUNCNAME[0]}"
}

case_cleanup_keeps_existing_users_and_tags() {
    prepare_baseline
    simulate_smoke_writes
    run_cleanup "$SMOKE_USER" "$TAG_MAX"
    [ "$MS_CLEAN_STATUS" -eq 0 ] || ms_fail "${FUNCNAME[0]}: 後始末が失敗した"
    [ "$(ms_sql -At -c "SELECT count(*) FROM \"user\" WHERE user_name IN ('$MS_SYNTH_NAME_1', '$MS_SYNTH_NAME_2')")" -eq 2 ] \
        || ms_fail "${FUNCNAME[0]}: 既存の利用者が消えた"
    [ "$(ms_sql -At -c "SELECT count(*) FROM tag WHERE tag_name IN ('$MS_SYNTH_TAG_1', '$MS_SYNTH_TAG_2')")" -eq 2 ] \
        || ms_fail "${FUNCNAME[0]}: 既存のtagが消えた"
    [ "$(ms_sql -At -c "SELECT count(*) FROM tag WHERE tag_name = 'smoke-only-tag'")" -eq 0 ] \
        || ms_fail "${FUNCNAME[0]}: smokeが増やしたtagが残っている"
}

# smokeが増やしたtagでも、他の利用者が使っていれば消さない
case_cleanup_keeps_new_tag_used_by_others() {
    prepare_baseline
    simulate_smoke_writes
    ms_sql >/dev/null <<SQL
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 1
FROM "user" AS u, tag AS t WHERE u.user_name = '$MS_SYNTH_NAME_2' AND t.tag_name = 'smoke-only-tag';
SQL
    run_cleanup "$SMOKE_USER" "$TAG_MAX"
    [ "$MS_CLEAN_STATUS" -eq 0 ] || ms_fail "${FUNCNAME[0]}: 後始末が失敗した"
    [ "$(ms_sql -At -c "SELECT count(*) FROM tag WHERE tag_name = 'smoke-only-tag'")" -eq 1 ] \
        || ms_fail "${FUNCNAME[0]}: 他の利用者が使うtagを消した"
}

# 対象の利用者がいなければ、何も消さずに失敗する
case_cleanup_fails_when_user_is_missing() {
    prepare_baseline
    run_cleanup "$SMOKE_USER" "$TAG_MAX"
    [ "$MS_CLEAN_STATUS" -ne 0 ] || ms_fail "${FUNCNAME[0]}: 対象が無いのに成功した"
    [ "$(count '"user"')" -eq 2 ] && [ "$(count recommend)" -eq 4 ] || ms_fail "${FUNCNAME[0]}: 失敗したのに表が変わった"
}

# 合成の名前の形でない利用者（本物の利用者を含む）は、名前が合っていても消さない
case_cleanup_refuses_non_synthetic_name() {
    prepare_baseline
    run_cleanup "$MS_SYNTH_NAME_2" "$TAG_MAX"
    [ "$MS_CLEAN_STATUS" -ne 0 ] || ms_fail "${FUNCNAME[0]}: 合成でない利用者の削除が成功した"
    [ "$(count '"user"')" -eq 2 ] && [ "$(count recommend)" -eq 4 ] || ms_fail "${FUNCNAME[0]}: 失敗したのに表が変わった"
}

# 途中で失敗したら、一部だけ消えた状態を残さない（transactionで巻き戻る）
case_cleanup_is_all_or_nothing() {
    prepare_baseline
    simulate_smoke_writes
    local before_users before_recommend
    before_users="$(count '"user"')"
    before_recommend="$(count recommend)"
    # tag_maxに数字でない値を渡すと、tagの削除で失敗する
    run_cleanup "$SMOKE_USER" "not-a-number"
    [ "$MS_CLEAN_STATUS" -ne 0 ] || ms_fail "${FUNCNAME[0]}: 不正な値で成功した"
    [ "$(count '"user"')" -eq "$before_users" ] && [ "$(count recommend)" -eq "$before_recommend" ] \
        || ms_fail "${FUNCNAME[0]}: 失敗したのに一部だけ消えた"
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
    run_case "$case_name"
done

printf 'OK: %s rehearsal cleanup cases passed\n' "$passed"
