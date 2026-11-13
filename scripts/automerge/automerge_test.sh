#!/usr/bin/env bash
# automerge.sh の試験。本物の gh・ネットワークは使わず、PATH の先頭に置いたフェイクの gh と判定スクリプトで確かめる。
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/automerge.sh"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin"

# フェイクの gh: 呼ばれた引数を $FAKE_CALLS に1行ずつ記録し、環境変数のシナリオで答える
cat > "$T/bin/gh" <<'F'
#!/usr/bin/env bash
printf 'gh %s\n' "$*" | tr '\n' ' ' | sed 's/ $//' >> "$FAKE_CALLS"
echo >> "$FAKE_CALLS"
case "$1 $2" in
  "repo view") echo "o/r" ;;
  "pr checks") ;;
  "run list") if [ -n "${FAKE_RUNS:-}" ]; then echo "$FAKE_RUNS"; else echo '[{"status":"completed","conclusion":"success"}]'; fi ;;
  "pr view") echo "${FAKE_HEAD_SHA:-abc123}" ;;
  "api "*|"api") case "$*" in *"-X "*) ;; *) echo "${FAKE_COMMENT_ID:-}" ;; esac ;;
esac
exit 0
F
# フェイクの判定スクリプト
cat > "$T/elig" <<'F'
#!/usr/bin/env bash
case "$*" in
  *--json*) echo "{\"eligible\":${FAKE_ELIGIBLE:-true},\"class\":\"docs\",\"failures\":[],\"headSha\":\"abc123\"}" ;;
  *) echo "## 自動マージの判定: fake" ;;
esac
[ "${FAKE_ELIGIBLE:-true}" = true ]
F
chmod +x "$T/bin/gh" "$T/elig"

fail=0
# run <名前> [環境変数=値 ...]: 1シナリオ実行して $T/calls に呼び出しを残す
run() {
  NAME="$1"; shift
  rm -rf "$T/state" "$T/logs"; : > "$T/calls"
  env PATH="$T/bin:$PATH" FAKE_CALLS="$T/calls" AUTOMERGE_ELIGIBILITY_CMD="$T/elig" \
    AUTOMERGE_STATE_DIR="$T/state" AUTOMERGE_LOG_DIR="$T/logs" AUTOMERGE_HOME="$T/none" \
    AUTOMERGE_SLEEP_CMD=true AUTOMERGE_NOW=12:00 GH_REPO=o/r "$@" bash "$SCRIPT" 7 > "$T/out" 2>&1 \
    || { echo "NG: $NAME: スクリプトが失敗"; cat "$T/out"; fail=1; }
}
count() { grep -c -- "$1" "$T/calls" || true; }
expect() { # 名前 パターン 回数
  local n; n="$(count "$2")"
  if [ "$n" != "$3" ]; then echo "NG: $1: '$2' が $n 回（期待 $3）"; cat "$T/calls"; fail=1; else echo "ok: $1"; fi
}
MERGE='pr merge 7 --squash --match-head-commit abc123'

run 既定 ; expect "既定: マージしない" 'pr merge' 0; expect "既定: コメントは付く" 'api -X POST' 1
grep -q '停止中' "$T/calls" || { echo "NG: 停止中の文言なし"; fail=1; }

run 通常 AUTOMERGE_ENABLED=true; expect "条件が揃えばマージ" "$MERGE" 1; expect "マージは1回だけ" 'pr merge' 1
expect "ラベルを付ける" 'pr edit 7 --add-label automerged' 1

run 対象外 AUTOMERGE_ENABLED=true FAKE_ELIGIBLE=false; expect "対象外はマージしない" 'pr merge' 0

run 時間帯 AUTOMERGE_ENABLED=true AUTOMERGE_NOW=11:10
expect "時間帯: マージしない" 'pr merge' 0; expect "時間帯: コメントは2回（保留→やり直し）" 'api -X POST' 2
grep -q '保留' "$T/calls" || { echo "NG: 保留の文言なし"; fail=1; }

run main赤 AUTOMERGE_ENABLED=true 'FAKE_RUNS=[{"status":"completed","conclusion":"failure"}]'
expect "mainが赤ならマージしない" 'pr merge' 0

run SHA変化 AUTOMERGE_ENABLED=true FAKE_HEAD_SHA=def456; expect "SHAが変わればマージしない" 'pr merge' 0

run 既存コメント AUTOMERGE_ENABLED=true FAKE_COMMENT_ID=555
expect "既存コメントはPATCHで上書き" 'api -X PATCH repos/o/r/issues/comments/555' 1
expect "既存コメントがあれば新規作成しない" 'api -X POST' 0

[ "$fail" -eq 0 ] && echo "すべて成功" || { echo "失敗あり"; exit 1; }
