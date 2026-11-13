#!/usr/bin/env bash
# ops/check_resources.sh の試験。df と free の出力を差し替え、実機のディスクやメモリは見ない。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/ops/check_resources.sh"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

mkdir -p "$work/bin"
export FAKE_DIR="$work"
export PATH="$work/bin:$PATH"

cat >"$work/bin/df" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DIR/df.args"
if [ "${FAKE_DF_RC:-0}" -ne 0 ]; then
  exit "$FAKE_DF_RC"
fi
cat "$FAKE_DIR/df.out"
EOF
cat >"$work/bin/free" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DIR/free.args"
if [ "${FAKE_FREE_RC:-0}" -ne 0 ]; then
  exit "$FAKE_FREE_RC"
fi
cat "$FAKE_DIR/free.out"
EOF
chmod +x "$work/bin/df" "$work/bin/free"

write_df() {
  # write_df USED_PERCENT
  cat >"$work/df.out" <<EOF
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/root        40123456  10000000  30123456      ${1}% /
EOF
}

write_free() {
  # write_free AVAILABLE_MB SWAP_TOTAL_MB SWAP_USED_MB
  cat >"$work/free.out" <<EOF
               total        used        free      shared  buff/cache   available
Mem:            1024         400         200          10         424         ${1}
Swap:           ${2}         ${3}        0
EOF
}

reset_case() {
  rm -f "$work/df.args" "$work/free.args" "$work/stderr"
  unset FAKE_DF_RC FAKE_FREE_RC
  export FAKE_DF_RC=0 FAKE_FREE_RC=0
  unset CHECK_DISK_PERCENT CHECK_MEM_AVAILABLE_MB CHECK_SWAP_USED_MB
}

run_check() {
  set +e
  stdout="$("$script" "$@" 2>"$work/stderr")"
  code=$?
  set -e
  stderr="$(cat "$work/stderr")"
}

assert_ok() {
  [ "$code" -eq 0 ] || fail "$1: 終了コード $code（stdout=$stdout stderr=$stderr）"
  [ -z "$stdout" ] || fail "$1: 成功なのに標準出力がある: $stdout"
  [ -z "$stderr" ] || fail "$1: 成功なのに標準エラーがある: $stderr"
}

assert_warn() {
  local want="$1"
  [ "$code" -eq 1 ] || fail "警告のはずが終了コード $code（stdout=$stdout stderr=$stderr）"
  [ "$stdout" = "$want" ] || fail "警告の文が違う: [$stdout] 期待: [$want]"
  [ "$(printf '%s\n' "$stdout" | wc -l | tr -d ' ')" -eq 1 ] || fail "警告が1行でない"
  [ -z "$stderr" ] || fail "警告なのに標準エラーがある: $stderr"
}

assert_error() {
  [ "$code" -eq 2 ] || fail "$1: 終了コード $code（stdout=$stdout stderr=$stderr）"
  [ -z "$stdout" ] || fail "$1: エラーなのに標準出力がある: $stdout"
  printf '%s\n' "$stderr" | grep -q "$2" || fail "$1: 標準エラーに「$2」が無い: $stderr"
}

# --- しきい値の内側は、何も出さず成功 ---
reset_case
write_df 79
write_free 100 2048 199
run_check
assert_ok "79%・空き100MB・スワップ199MB"
grep -qx -- '-P /' "$work/df.args" || fail "df を -P / で呼んでいない: $(cat "$work/df.args")"
grep -qx -- '-m' "$work/free.args" || fail "free を -m で呼んでいない: $(cat "$work/free.args")"
echo "ok: しきい値の内側"

# --- ちょうど境目。ディスクとスワップは以上、空きメモリは未満 ---
reset_case
write_df 80
write_free 100 2048 199
run_check
assert_warn "警告: ディスク使用率 80%（しきい値 80%以上）"
echo "ok: ディスク 80% で警告"

reset_case
write_df 79
write_free 99 2048 199
run_check
assert_warn "警告: 空きメモリ 99MB（しきい値 100MB未満）"
echo "ok: 空きメモリ 99MB で警告"

reset_case
write_df 79
write_free 100 2048 200
run_check
assert_warn "警告: スワップ使用 200MB（しきい値 200MB以上）"
echo "ok: スワップ 200MB で警告"

# --- 複数は1行にまとめる ---
reset_case
write_df 91
write_free 40 2048 300
run_check
assert_warn "警告: ディスク使用率 91%（しきい値 80%以上）。空きメモリ 40MB（しきい値 100MB未満）。スワップ使用 300MB（しきい値 200MB以上）"
echo "ok: 複数の警告は1行"

# --- スワップが無い（合計 0）ときは、使用量が大きくても見ない ---
reset_case
write_df 10
write_free 500 0 999
run_check
assert_ok "スワップ合計 0"
echo "ok: スワップが無いときは見ない"

# --- 環境変数と、それを上書きする引数 ---
reset_case
write_df 60
write_free 500 2048 0
CHECK_DISK_PERCENT=50 run_check
assert_warn "警告: ディスク使用率 60%（しきい値 50%以上）"
echo "ok: 環境変数"

reset_case
write_df 60
write_free 500 2048 0
CHECK_DISK_PERCENT=50 run_check --disk-percent 90
assert_ok "引数が環境変数より優先"
echo "ok: 引数が優先"

reset_case
write_df 10
write_free 50 2048 10
run_check --mem-available-mb 40 --swap-used-mb 100
assert_ok "引数で緩めたしきい値"
echo "ok: メモリとスワップの引数"

# --- 不正な引数と、読めない出力 ---
reset_case
write_df 10
write_free 500 2048 0
run_check --disk-percent 0
assert_error "0%" "1 から 100"
run_check --disk-percent 101
assert_error "101%" "1 から 100"
run_check --mem-available-mb 0
assert_error "メモリ 0" "空きメモリ"
run_check --swap-used-mb abc
assert_error "スワップ abc" "スワップ"
run_check --nope
assert_error "不明な引数" "使い方"
echo "ok: 引数の拒否"

reset_case
write_df 10
write_free 500 2048 0
FAKE_DF_RC=1 run_check
assert_error "df 失敗" "df"
echo "ok: df の失敗"

reset_case
write_df 10
write_free 500 2048 0
FAKE_FREE_RC=1 run_check
assert_error "free 失敗" "free"
echo "ok: free の失敗"

reset_case
printf '%s\n' 'Filesystem 1024-blocks Used Available Capacity Mounted on' >"$work/df.out"
write_free 500 2048 0
run_check
assert_error "df の行が足りない" "使用率"
echo "ok: df の形が違う"

reset_case
write_df 10
cat >"$work/free.out" <<'EOF'
               total        used        free
Mem:            1024         400         200
Swap:              0           0           0
EOF
run_check
assert_error "available 列が無い" "available"
echo "ok: available 列が無い"

echo "OK: check_resources の試験にすべて成功しました"
