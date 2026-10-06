#!/usr/bin/env bash
# snapshot_migration_state.sh と compare_migration_state.sh が共有する部品。
# 単独では実行しない。呼び出し側が `set -euo pipefail`、`set +x`、`umask 077` を済ませ、
# 失敗の公開文を決める `fail CODE [EXIT]` と、一時fileのパス `err_file` を用意しておく。

mode_of() {
    stat -c '%a' -- "$1" 2>/dev/null || stat -f '%Lp' -- "$1"
}

# NTFS上のGit Bashなど、chmodの結果を読み戻せない環境ではmodeを検査できない。
# その場合は検査を省く（利用者のprofile配下のACLに任せる）。検査できる環境では必ず守らせる
modes_enforceable() {
    local probe result
    probe="$(mktemp "$1/mode-probe.XXXXXX")" || return 1
    chmod 600 -- "$probe"
    result="$(mode_of "$probe")"
    rm -f -- "$probe"
    [ "$result" = "600" ]
}

# require_private_mode PATH EXPECTED_MODE PROBE_DIR
require_private_mode() {
    if modes_enforceable "$3"; then
        [ "$(mode_of "$1")" = "$2" ] || return 1
    fi
}

# 対象DBでpsqlを実行する。stdoutにはJSONや比較結果が出るため、呼び出し側が保護されたfileへ受ける
run_psql() {
    local -a compose_args=()
    if [ -n "${MTP_SNAPSHOT_COMPOSE_ARGS:-}" ]; then
        read -r -a compose_args <<<"$MTP_SNAPSHOT_COMPOSE_ARGS"
    fi
    docker compose "${compose_args[@]}" exec -T db \
        psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "$MTP_SNAPSHOT_DB" "$@"
}

validate_db_name() {
    [ -n "${MTP_SNAPSHOT_DB:-}" ] || fail E_CONFIG 2
    [[ "$MTP_SNAPSHOT_DB" =~ ^[a-z0-9_]+$ ]] || fail E_CONFIG 2
}

# COPYで渡したデータがサーバーのログへ残る設定なら、データを渡す前に止める
require_safe_server_logging() {
    local logging
    logging="$(run_psql -c "SELECT current_setting('log_statement') || ',' || current_setting('log_parameter_max_length_on_error')" 2>"$err_file")" \
        || fail E_DB
    case "$logging" in
        none,0 | ddl,0) ;;
        *) fail E_LOGGING ;;
    esac
}
