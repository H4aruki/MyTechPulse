#!/usr/bin/env bash
# ops/snapshot_migration_state.sh と SQL の動作を、合成データを入れた使い捨てDBで確かめる。
# 使い方: bash ops/snapshot_migration_state_test.sh（Docker Desktopが起動していること）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/migration_state_fixture.sh
source "$repo_root/ops/tests/migration_state_fixture.sh"

trap ms_cleanup EXIT

# SQLは読み取り専用で、固定のhex値を持たない（静的な確認）
if grep -nE 'INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER|CREATE' "$repo_root/ops/sql/snapshot_migration_state.sql" \
    | grep -vE '^[0-9]+:--'; then
    ms_fail 'snapshot SQLに書き込み系の文がある'
fi
grep -q 'BEGIN READ ONLY' "$repo_root/ops/sql/snapshot_migration_state.sql" \
    || ms_fail 'snapshot SQLが読み取り専用のtransactionでない'
if grep -nE '[0-9a-f]{32}' "$repo_root/ops/sql/snapshot_migration_state.sql"; then
    ms_fail 'snapshot SQLに固定のhex値がある'
fi

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

# chmodの結果を読み戻せない環境（NTFS上のGit Bash）ではmodeの検査を省く
modes_check_available() {
    local probe mode
    probe="$(mktemp "$MS_TMP/probe.XXXXXX")"
    chmod 600 -- "$probe"
    mode="$(stat -c '%a' "$probe")"
    rm -f -- "$probe"
    [ "$mode" = "600" ]
}

# snapshotを任意の環境で呼び、終了コードと出力を控える（ms_snap_tryと同じ控え方）
run_snapshot() {
    set +e
    bash "$repo_root/ops/snapshot_migration_state.sh" >"$MS_TMP/x.snap.out" 2>"$MS_TMP/x.snap.err"
    MS_SNAP_STATUS=$?
    set -e
    cat "$MS_TMP/x.snap.out" "$MS_TMP/x.snap.err" >>"$MS_PUBLIC_LOG"
}

# 失敗したsnapshotが、固定の失敗文以外・出力file・一時fileを残さない
assert_failure_is_clean() {
    local name="$1" expected_code="$2" expected_status="$3"
    [ "$MS_SNAP_STATUS" -eq "$expected_status" ] \
        || ms_fail "$name: 終了コード $MS_SNAP_STATUS（$expected_status を期待）"
    [ ! -s "$MS_TMP/x.snap.out" ] || ms_fail "$name: 標準出力がある"
    [ "$(cat "$MS_TMP/x.snap.err")" = "snapshot: failed ($expected_code)" ] \
        || ms_fail "$name: 失敗文が固定でない: $(cat "$MS_TMP/x.snap.err")"
    [ ! -e "$MS_TMP/x.json" ] || ms_fail "$name: 失敗したのにsnapshotが残っている"
    local leftover
    leftover="$(find "$MS_TMP" -maxdepth 1 \( -name 'snapshot-*' -o -name 'mode-probe.*' \) | head -1)"
    [ -z "$leftover" ] || ms_fail "$name: 一時fileが残っている"
}

case_snapshot_shape_and_secrecy() {
    ms_legacy_schema
    ms_seed
    ms_migrate
    ms_snap x
    [ "$(cat "$MS_TMP/x.snap.out")" = 'snapshot: ok' ] || ms_fail "${FUNCNAME[0]}: 成功文が固定でない"
    [ ! -s "$MS_TMP/x.snap.err" ] || ms_fail "${FUNCNAME[0]}: 標準エラーがある"
    [ "$(wc -l <"$MS_TMP/x.json")" -eq 1 ] || ms_fail "${FUNCNAME[0]}: JSONが1行でない"
    local key
    for key in schema_version run_id counts constraints sequences table_digests; do
        grep -q "\"$key\":" "$MS_TMP/x.json" || ms_fail "${FUNCNAME[0]}: $key が無い"
    done
    [ "$(grep -Eo '"(user|tag|recommend)": "[0-9a-f]{64}"' "$MS_TMP/x.json" | wc -l)" -eq 3 ] \
        || ms_fail "${FUNCNAME[0]}: 3表のdigestが揃っていない"
    grep -q '"counts": {"tag": 2, "user": 2, "recommend": 4}' "$MS_TMP/x.json" \
        || ms_fail "${FUNCNAME[0]}: 件数が合わない"
    grep -q 'user_role_check' "$MS_TMP/x.json" || ms_fail "${FUNCNAME[0]}: migration後の制約が無い"
    local value
    for value in "$MS_SYNTH_PASSWORD_1" "$MS_SYNTH_PASSWORD_2" "$MS_SYNTH_NAME_1" "$MS_SYNTH_NAME_2" \
        "$MS_SYNTH_TAG_1" "$MS_SYNTH_TAG_2"; do
        if grep -qF -- "$value" "$MS_TMP/x.json"; then
            ms_fail "${FUNCNAME[0]}: snapshotに生の値がある"
        fi
    done
    if grep -qF -- "$(cat "$MS_TMP/nonce")" "$MS_TMP/x.json"; then
        ms_fail "${FUNCNAME[0]}: snapshotにnonceがある"
    fi
}

case_nonce_is_created_and_reused() {
    ms_legacy_schema
    ms_seed
    [ ! -e "$MS_TMP/nonce" ] || ms_fail "${FUNCNAME[0]}: 準備が不正"
    ms_snap a
    grep -Eq '^[0-9a-f]{64}$' "$MS_TMP/nonce" || ms_fail "${FUNCNAME[0]}: nonceが64桁のhexでない"
    local first_nonce
    first_nonce="$(cat "$MS_TMP/nonce")"
    ms_snap b
    [ "$(cat "$MS_TMP/nonce")" = "$first_nonce" ] || ms_fail "${FUNCNAME[0]}: nonceが書き換わった"
    # 同じnonce・同じ内容なら、同じsnapshotになる
    cmp -s "$MS_TMP/a.json" "$MS_TMP/b.json" || ms_fail "${FUNCNAME[0]}: 同じ内容なのにsnapshotが違う"
    if modes_check_available; then
        [ "$(stat -c '%a' "$MS_TMP/nonce")" = "600" ] || ms_fail "${FUNCNAME[0]}: nonceが0600でない"
        [ "$(stat -c '%a' "$MS_TMP/a.json")" = "600" ] || ms_fail "${FUNCNAME[0]}: snapshotが0600でない"
    fi
}

case_different_nonce_gives_different_digests() {
    ms_legacy_schema
    ms_seed
    ms_snap a
    rm -f -- "$MS_TMP/nonce"
    ms_snap b
    local digest_a digest_b
    digest_a="$(grep -Eo '"user": "[0-9a-f]{64}"' "$MS_TMP/a.json")"
    digest_b="$(grep -Eo '"user": "[0-9a-f]{64}"' "$MS_TMP/b.json")"
    [ -n "$digest_a" ] && [ "$digest_a" != "$digest_b" ] || ms_fail "${FUNCNAME[0]}: nonceが違うのにdigestが同じ"
}

case_snapshot_does_not_change_database() {
    ms_legacy_schema
    ms_seed
    ms_migrate
    local query before after
    query="SELECT (SELECT count(*) FROM \"user\") || ',' || (SELECT count(*) FROM pg_tables WHERE schemaname = 'public')"
    before="$(ms_sql -At -c "$query")"
    ms_snap a
    after="$(ms_sql -At -c "$query")"
    [ "$before" = "$after" ] || ms_fail "${FUNCNAME[0]}: snapshotでDBが変わった"
}

case_invalid_settings_are_rejected() {
    ms_legacy_schema
    local out="$MS_TMP/x.json"

    MTP_SNAPSHOT_OUTPUT='' run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}(出力先なし)" E_CONFIG 2

    MTP_SNAPSHOT_DB='bad name;drop' MTP_SNAPSHOT_OUTPUT="$out" run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}(DB名)" E_CONFIG 2

    MTP_SNAPSHOT_OUTPUT="$MS_TMP/no-such-dir/x.json" run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}(出力directory)" E_OUTPUT 2

    # 既存のsnapshotは上書きしない
    printf 'existing\n' >"$out"
    MTP_SNAPSHOT_OUTPUT="$out" run_snapshot
    [ "$MS_SNAP_STATUS" -eq 2 ] || ms_fail "${FUNCNAME[0]}(既存出力): 終了コード $MS_SNAP_STATUS"
    [ "$(cat "$out")" = 'existing' ] || ms_fail "${FUNCNAME[0]}: 既存のfileを上書きした"
    [ "$(cat "$MS_TMP/x.snap.err")" = 'snapshot: failed (E_OUTPUT)' ] \
        || ms_fail "${FUNCNAME[0]}: 失敗文が固定でない"
    rm -f -- "$out"

    # nonceが壊れている
    printf 'not-a-nonce' >"$MS_TMP/nonce"
    chmod 600 -- "$MS_TMP/nonce"
    MTP_SNAPSHOT_OUTPUT="$out" run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}(壊れたnonce)" E_NONCE 2
}

case_unreachable_database_has_fixed_message() {
    ms_legacy_schema
    MTP_SNAPSHOT_DB=mytechpulse_no_such_database MTP_SNAPSHOT_OUTPUT="$MS_TMP/x.json" run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}" E_DB 1
}

# サーバーがstatementを記録する設定だと、COPYで渡すnonceが残りうるため、渡す前に止める
case_server_logging_blocks_snapshot() {
    ms_legacy_schema
    ms_sql -c "ALTER DATABASE \"$MS_DB\" SET log_statement = 'all'" >/dev/null
    MTP_SNAPSHOT_OUTPUT="$MS_TMP/x.json" run_snapshot
    assert_failure_is_clean "${FUNCNAME[0]}" E_LOGGING 1
}

# shell traceを有効にして呼んでも、nonceが出ない
case_shell_trace_does_not_expose_nonce() {
    ms_legacy_schema
    ms_seed
    ms_snap a
    set +e
    MTP_SNAPSHOT_OUTPUT="$MS_TMP/traced.json" \
        env SHELLOPTS=xtrace bash -x "$repo_root/ops/snapshot_migration_state.sh" >"$MS_TMP/traced.out" 2>"$MS_TMP/traced.err"
    local status=$?
    set -e
    [ "$status" -eq 0 ] || ms_fail "${FUNCNAME[0]}: trace付きで失敗した"
    cat "$MS_TMP/traced.out" "$MS_TMP/traced.err" >>"$MS_PUBLIC_LOG"
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
    run_case "$case_name"
done

printf 'OK: %s snapshot cases passed\n' "$passed"
