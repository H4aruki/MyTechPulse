#!/usr/bin/env bash
# 移行前後のDB状態を、中身を表に出さずにJSONへ記録する。
#
# 入力（環境変数）:
#   MTP_SNAPSHOT_DB            対象DB名（小文字・数字・アンダースコアだけ）
#   MTP_SNAPSHOT_NONCE_FILE    秘密nonceのfile。無ければ0600で新しく作り、あれば再利用する。
#                              before/after/cleanupで同じfileを使う
#   MTP_SNAPSHOT_OUTPUT        書き出すsnapshot JSONのfile。0700のdirectory内に置く
#   MTP_SNAPSHOT_COMPOSE_ARGS  任意。docker composeへ渡す追加引数
#                              （例: "-p mytechpulse-rehearsal -f docker-compose.rehearsal.yml"）
# 公開する出力は成功/失敗の固定文だけ。snapshot・nonce・digestは機密として扱い、
# artifactにもIssueにも通常ログにも出さない。nonceの掃除は、作成した側（orchestrator）が行う。
set -euo pipefail
set +x
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

err_file=""
out_tmp=""

cleanup() {
    rm -f -- "$err_file" "$out_tmp"
}
trap cleanup EXIT

fail() {
    printf 'snapshot: failed (%s)\n' "$1" >&2
    exit "${2:-1}"
}

# shellcheck source=ops/lib/migration_state_common.sh
source "$repo_root/ops/lib/migration_state_common.sh"

validate_db_name
nonce_file="${MTP_SNAPSHOT_NONCE_FILE:-}"
output_file="${MTP_SNAPSHOT_OUTPUT:-}"
[ -n "$nonce_file" ] && [ -n "$output_file" ] || fail E_CONFIG 2

output_dir="$(dirname -- "$output_file")"
[ -d "$output_dir" ] || fail E_OUTPUT 2
require_private_mode "$output_dir" 700 "$output_dir" || fail E_OUTPUT 2
[ ! -e "$output_file" ] || fail E_OUTPUT 2

# 接続失敗などの出力は、値を含みうるため、保護した一時fileにだけ残す
err_file="$(mktemp "$output_dir/snapshot-err.XXXXXX")"
out_tmp="$(mktemp "$output_dir/snapshot-out.XXXXXX")"

# nonceが無いときだけ、この実行で作る（既存fileは上書きしない）
if [ ! -e "$nonce_file" ]; then
    (
        set -C
        head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' >"$nonce_file"
    ) 2>/dev/null || fail E_NONCE 2
fi
[ -f "$nonce_file" ] && [ ! -L "$nonce_file" ] || fail E_NONCE 2
require_private_mode "$nonce_file" 600 "$output_dir" || fail E_NONCE 2
grep -Eq '^[0-9a-f]{64}$' "$nonce_file" || fail E_NONCE 2

require_safe_server_logging

# nonceは標準入力のCOPYだけで一時表へ渡す。引数・環境変数・SQL文字列へは展開しない
if ! {
    printf '%s\n' \
        'CREATE TEMP TABLE snapshot_nonce (n text NOT NULL);' \
        'COPY snapshot_nonce (n) FROM STDIN;'
    cat "$nonce_file"
    printf '\n\\.\n'
    cat ops/sql/snapshot_migration_state.sql
} | run_psql >"$out_tmp" 2>"$err_file"; then
    fail E_DB
fi

# 1行のJSONだけが返っていることを確かめてから、本来の置き場所へ移す
[ "$(wc -l <"$out_tmp")" -eq 1 ] && grep -q '^{' "$out_tmp" || fail E_OUTPUT
mv -- "$out_tmp" "$output_file"
chmod 600 -- "$output_file"
out_tmp=""

printf 'snapshot: ok\n'
