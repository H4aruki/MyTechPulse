#!/usr/bin/env bash
# snapshot_migration_state_test.sh と compare_migration_state_test.sh が使う共通の準備。
# 開発用composeのdbに、使い捨てのDBを作って試す（既存DBには触れない）。値はすべて合成データ。
# 単独では実行しない。

MS_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MS_TMP=""
MS_DB=""

# 合成データ。公開出力に現れてはいけない値の見本を兼ねる
MS_SYNTH_PASSWORD_1='$2b$12$synthetic.hash.one.not.a.real.password.value'
MS_SYNTH_PASSWORD_2='$2b$12$synthetic.hash.two.not.a.real.password.value'
MS_SYNTH_NAME_1='名前-alpha|beta'
MS_SYNTH_NAME_2='second-user'
MS_SYNTH_TAG_1='タグ-go'
MS_SYNTH_TAG_2='tag-two'

ms_fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

ms_init() {
    cd "$MS_REPO"
    MS_TMP="$(mktemp -d)"
    chmod 700 -- "$MS_TMP"
    MS_PUBLIC_LOG="$MS_TMP/public.log"
    : >"$MS_PUBLIC_LOG"
    MS_DB="mytechpulse_snapshot_test_$(date -u +%Y%m%d%H%M%S)_$RANDOM"
    docker compose up -d --wait db >/dev/null 2>&1
    docker compose exec -T db createdb -U postgres "$MS_DB"
    export MTP_SNAPSHOT_DB="$MS_DB"
    export MTP_SNAPSHOT_NONCE_FILE="$MS_TMP/nonce"
    unset MTP_SNAPSHOT_COMPOSE_ARGS MTP_SNAPSHOT_OUTPUT
}

ms_cleanup() {
    if [ -n "$MS_DB" ]; then
        docker compose exec -T db dropdb -U postgres --if-exists "$MS_DB" >/dev/null 2>&1 || true
    fi
    if [ -n "$MS_TMP" ]; then
        rm -rf -- "$MS_TMP"
    fi
}

ms_sql() {
    docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$MS_DB" "$@"
}

# 1件ごとにスキーマを作り直し、nonceも新しくする
ms_reset() {
    ms_sql -c 'SET client_min_messages = warning; DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
    ms_sql -c "ALTER DATABASE \"$MS_DB\" RESET log_statement" >/dev/null
    rm -f -- "$MS_TMP"/nonce "$MS_TMP"/*.json "$MS_TMP"/*.log.out
    : >"$MS_PUBLIC_LOG"
}

# server/db/migrations の本物の `+goose Up` 部分をそのまま流す
ms_goose_up() {
    local file="$MS_REPO/server/db/migrations/$1"
    sed -n '/^-- +goose Up/,/^-- +goose Down/p' "$file" | grep -v '^-- +goose Down' | ms_sql >/dev/null
}

ms_legacy_schema() {
    ms_goose_up 00001_legacy_baseline.sql
}

ms_migrate() {
    ms_goose_up 00002_user_role.sql
    ms_goose_up 00003_auth_session.sql
}

ms_seed() {
    ms_sql >/dev/null <<SQL
INSERT INTO "user" (user_name, password) VALUES
    ('$MS_SYNTH_NAME_1', '$MS_SYNTH_PASSWORD_1'),
    ('$MS_SYNTH_NAME_2', '$MS_SYNTH_PASSWORD_2');
INSERT INTO tag (tag_name) VALUES ('$MS_SYNTH_TAG_1'), ('$MS_SYNTH_TAG_2');
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 5000
FROM "user" AS u CROSS JOIN tag AS t;
SQL
}

# ms_snap_try NAME : snapshotを NAME.json へ作る。stdout/stderrは公開ログへ集める。
# 失敗しても止まらず、終了コードを MS_SNAP_STATUS へ入れる
MS_SNAP_STATUS=0
ms_snap_try() {
    local name="$1"
    set +e
    MTP_SNAPSHOT_OUTPUT="$MS_TMP/$name.json" \
        bash "$MS_REPO/ops/snapshot_migration_state.sh" >"$MS_TMP/$name.snap.out" 2>"$MS_TMP/$name.snap.err"
    MS_SNAP_STATUS=$?
    set -e
    cat "$MS_TMP/$name.snap.out" "$MS_TMP/$name.snap.err" >>"$MS_PUBLIC_LOG"
}

# 成功するはずのsnapshot
ms_snap() {
    ms_snap_try "$1"
    [ "$MS_SNAP_STATUS" -eq 0 ] || ms_fail "snapshot $1 が失敗した（終了コード $MS_SNAP_STATUS）"
}

# ms_compare BEFORE AFTER [FLAG] : 結果を MS_CMP_STATUS と MS_CMP_OUT へ入れる
MS_CMP_STATUS=0
MS_CMP_OUT=""
ms_compare() {
    set +e
    bash "$MS_REPO/ops/compare_migration_state.sh" "$MS_TMP/$1.json" "$MS_TMP/$2.json" ${3:+"$3"} \
        >"$MS_TMP/cmp.out" 2>"$MS_TMP/cmp.err"
    MS_CMP_STATUS=$?
    set -e
    MS_CMP_OUT="$(cat "$MS_TMP/cmp.out")"
    cat "$MS_TMP/cmp.out" "$MS_TMP/cmp.err" >>"$MS_PUBLIC_LOG"
}

ms_expect_match() {
    [ "$MS_CMP_STATUS" -eq 0 ] || ms_fail "$1: 一致を期待したが終了コード $MS_CMP_STATUS ($MS_CMP_OUT)"
    [ "$MS_CMP_OUT" = '{"matches":true,"mismatched_tables":0}' ] || ms_fail "$1: 出力が想定外 ($MS_CMP_OUT)"
}

# ms_expect_mismatch NAME TABLE_COUNT
ms_expect_mismatch() {
    [ "$MS_CMP_STATUS" -eq 1 ] || ms_fail "$1: 不一致(1)を期待したが終了コード $MS_CMP_STATUS"
    [ "$MS_CMP_OUT" = "{\"matches\":false,\"mismatched_tables\":$2}" ] \
        || ms_fail "$1: 不一致table数 $2 を期待したが $MS_CMP_OUT"
}

ms_expect_unable() {
    [ "$MS_CMP_STATUS" -eq 2 ] || ms_fail "$1: 検査不能(2)を期待したが終了コード $MS_CMP_STATUS"
    [ -z "$MS_CMP_OUT" ] || ms_fail "$1: 検査不能なのに標準出力がある"
}

# 公開ログに、機密の見本・nonce・digestが1つも出ていないこと
ms_assert_no_leak() {
    local label="$1" value digest
    for value in \
        "$MS_SYNTH_PASSWORD_1" "$MS_SYNTH_PASSWORD_2" \
        "$MS_SYNTH_NAME_1" "$MS_SYNTH_NAME_2" "$MS_SYNTH_TAG_1" "$MS_SYNTH_TAG_2"; do
        if grep -qF -- "$value" "$MS_PUBLIC_LOG"; then
            ms_fail "$label: 公開ログに合成値が出ている"
        fi
    done
    if [ -f "$MS_TMP/nonce" ] && grep -qF -- "$(cat "$MS_TMP/nonce")" "$MS_PUBLIC_LOG"; then
        ms_fail "$label: 公開ログにnonceが出ている"
    fi
    local f
    for f in "$MS_TMP"/*.json; do
        [ -f "$f" ] || continue
        while IFS= read -r digest; do
            if grep -qF -- "$digest" "$MS_PUBLIC_LOG"; then
                ms_fail "$label: 公開ログにdigestが出ている"
            fi
        done < <(grep -Eo '[0-9a-f]{64}' "$f")
    done
}
