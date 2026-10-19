#!/usr/bin/env bash
# ops/fix_initial_interest_240.sh の試験（Dockerもデータベースも使わない）。
# 偽の docker で、どのモードでどんなSQLを流すか、実行の前の止め方を確かめる。
# SQLそのものの正しさは、Goの試験（server/internal/store/initial_interest_240_ops_integration_test.go）が本物のPostgreSQLで確かめる。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/ops/fix_initial_interest_240.sh"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

mkdir -p "$work/bin"
cat >"$work/bin/docker" <<'FAKE'
#!/usr/bin/env bash
# 偽の docker。引数を記録し、標準入力を保存する。
printf '%s\n' "$*" >>"$FAKE_LOG"
n="$(wc -l <"$FAKE_LOG")"
cat >"$FAKE_DIR/stdin.$n"
case " $* " in
  *" pg_restore "*) exit 0 ;;
  *" -t "*) printf '%s\n' "${FAKE_REMAINING:-0}" ;;
esac
exit 0
FAKE
chmod +x "$work/bin/docker"
export PATH="$work/bin:$PATH" FAKE_DIR="$work" FAKE_LOG="$work/docker.log"

reset() { rm -f "$work"/docker.log "$work"/stdin.*; : >"$work/docker.log"; }
calls() { wc -l <"$work/docker.log" | tr -d ' '; }
all_stdin() { cat "$work"/stdin.* 2>/dev/null || true; }

run() {
  # run EXPECTED_STATUS ARGS... （標準入力は端末ではない）
  local want="$1"
  shift
  local got=0
  (cd "$repo_root" && "$script" "$@" </dev/null >"$work/out" 2>"$work/err") || got=$?
  [ "$got" = "$want" ] || fail "$* の終了コード $got（期待 $want）: $(cat "$work/err")"
}

# --- 既定は読み取りだけ（dry-run） ---
reset
run 0
[ "$(calls)" = 1 ] || fail "dry-run の docker 呼び出しが $(calls) 回"
grep -q -- '-d mytechpulse' "$work/docker.log" || fail "既定のDB名で接続していない"
all_stdin | grep -q 'BEGIN TRANSACTION READ ONLY;' || fail "dry-run が読み取り専用のトランザクションで動いていない"
all_stdin | grep -q 'CALL pg_temp.ii240_run' && fail "dry-run で実行してしまう"
all_stdin | grep -q 'ii240_summary' || fail "dry-run で件数を出していない"

# --- --report も読み取りだけ ---
reset
run 0 --report
all_stdin | grep -q 'BEGIN TRANSACTION READ ONLY;' || fail "report が読み取り専用でない"
all_stdin | grep -q '消す行の内訳' || fail "report の内訳が無い"
all_stdin | grep -q 'CALL pg_temp.ii240_run' && fail "report で実行してしまう"

# --- --verify は残りが0件のときだけ成功 ---
reset
FAKE_REMAINING=0 run 0 --verify
reset
FAKE_REMAINING=12 run 1 --verify
grep -q '12' "$work/out" || fail "verify が残りの件数を出していない"

# --- DB_NAME で予行用のDBへ向けられる。不正な名前は止める ---
reset
DB_NAME=mytechpulse_rehearsal_240 run 0 --dry-run
grep -q -- '-d mytechpulse_rehearsal_240' "$work/docker.log" || fail "DB_NAME が効いていない"
reset
DB_NAME='x; DROP' run 2
[ "$(calls)" = 0 ] || fail "不正なDB名で接続した"

# --- 引数の誤り ---
reset
run 2 --unknown
run 2 --backup "$work/x.dump"
run 2 --execute
[ "$(calls)" = 0 ] || fail "引数の誤りなのに接続した"

# --- 専用のバックアップ（.keep 付き）が無ければ実行しない ---
printf 'dump' >"$work/b.dump"
(cd "$work" && sha256sum b.dump >b.dump.sha256)
reset
run 1 --execute --backup "$work/b.dump" --yes
[ "$(calls)" = 0 ] || fail ".keep が無いのに接続した"
grep -q '.keep' "$work/err" || fail ".keep が要ることを知らせていない"
touch "$work/b.dump.keep"

# --- 端末でなく --yes も無ければ、件数を出すだけで実行しない ---
reset
run 1 --execute --backup "$work/b.dump"
all_stdin | grep -q 'CALL pg_temp.ii240_run' && fail "確認なしで実行した"

# --- 壊れたバックアップでは実行しない ---
printf 'changed' >"$work/b.dump"
reset
run 1 --execute --backup "$work/b.dump" --yes
all_stdin | grep -q 'CALL pg_temp.ii240_run' && fail "チェックサムが合わないのに実行した"
printf 'dump' >"$work/b.dump"

# --- そろえば実行し、残りが0件なら成功 ---
reset
FAKE_REMAINING=0 run 0 --execute --backup "$work/b.dump" --yes
all_stdin | grep -q 'CALL pg_temp.ii240_run();' || fail "実行していない"
all_stdin | grep -q "SET lock_timeout" || fail "ロック待ちの上限を設定していない"
grep -q 'OK: 完了' "$work/out" || fail "完了を知らせていない"

# --- 実行後に残りがあれば失敗にする ---
reset
FAKE_REMAINING=3 run 1 --execute --backup "$work/b.dump" --yes

# --- SQLファイルは移行ではない（DELETE/UPDATE は ops/ の関数の中だけ） ---
if rg -q 'initial_interest_240|ii240' "$repo_root/server/db/migrations"; then
  fail "移行に #240 の書き換えが入っている"
fi

printf 'OK: fix_initial_interest_240\n'
