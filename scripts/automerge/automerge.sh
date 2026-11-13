#!/usr/bin/env bash
# 判定OKのPRだけを box からマージするスクリプト。使い方: automerge.sh <PR番号>
# 判定スクリプトとルールは、PRのブランチではなく origin/main の版を使う（PRのコードは動かさない）。
# 詳しくは docs/automerge-runner.md
set -euo pipefail

PR="${1:-}"
case "$PR" in '' | *[!0-9]*) echo "使い方: $0 <PR番号>" >&2; exit 2 ;; esac

HOME_DIR="${AUTOMERGE_HOME:-/home/box/shared/automerge/MyTechPulse}"
STATE_DIR="${AUTOMERGE_STATE_DIR:-/tmp/automerge}"
LOG_DIR="${AUTOMERGE_LOG_DIR:-/home/box/shared/bot-logs/automerge}"
SLEEP_CMD="${AUTOMERGE_SLEEP_CMD:-sleep}"
MARKER='<!-- automerge-eligibility -->'
DEFAULT_WINDOWS='[["05:30","06:20"],["11:00","11:50"],["16:00","16:50"],["03:50","04:20"]]'
SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$STATE_DIR" "$LOG_DIR"
WORK="$(mktemp -d)"
PIDF="$STATE_DIR/pr-$PR.pid"
cleanup() {
  [ "$(cat "$PIDF" 2>/dev/null || true)" = "$$" ] && rm -f "$PIDF"
  rm -rf "$WORK"
}
trap cleanup EXIT

log() { # 判定 マージ 理由
  printf '%s\tPR#%s\t%s\t%s\t%s\t%s\n' "$(TZ=Asia/Tokyo date '+%Y-%m-%d %H:%M:%S')" "$PR" "${SHA:--}" "$1" "$2" "$3" >> "$LOG_DIR/automerge.log"
}
SHA=""

# --- 同じPRの古い automerge.sh を止める
if [ -f "$PIDF" ]; then
  old="$(cat "$PIDF" 2>/dev/null || true)"
  if [ -n "$old" ] && [ "$old" != "$$" ] && kill -0 "$old" 2>/dev/null \
    && grep -qa 'automerge.sh' "/proc/$old/cmdline" 2>/dev/null; then
    pkill -P "$old" 2>/dev/null || true
    kill "$old" 2>/dev/null || true
  fi
fi
echo "$$" > "$PIDF"

# --- 判定スクリプトとルールを origin/main の版で用意する
GUARDS="$SELF_DIR/guards.mjs"
RULES=""
if [ -n "${AUTOMERGE_ELIGIBILITY_CMD:-}" ]; then
  ELIG_CMD="$AUTOMERGE_ELIGIBILITY_CMD"
else
  # 判定のPRが main に入る前は、判定スクリプトもルールも無い。取り出せなければ、何もせず終了コード3で終わる。
  if ! { git -C "$HOME_DIR" fetch -q origin main \
    && git -C "$HOME_DIR" archive origin/main scripts/automerge .github/automerge-rules.json | tar -x -C "$WORK" \
    && [ -f "$WORK/scripts/automerge/eligibility.mjs" ]; }; then
    log "判定不能" no "判定スクリプトが main に無い（先に判定のPRをマージする）"
    echo "判定スクリプトが main に無い（先に判定のPRをマージする）" >&2
    exit 3
  fi
  ELIG_CMD="node $WORK/scripts/automerge/eligibility.mjs"
  [ -f "$WORK/scripts/automerge/guards.mjs" ] && GUARDS="$WORK/scripts/automerge/guards.mjs"
fi
[ -f "$WORK/.github/automerge-rules.json" ] && RULES="$WORK/.github/automerge-rules.json"
[ -n "${AUTOMERGE_RULES:-}" ] && RULES="$AUTOMERGE_RULES"

cd "$HOME_DIR" 2>/dev/null || cd "$WORK"
REPO="${GH_REPO:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}"
export GH_REPO="$REPO"

now_jst() { echo "${AUTOMERGE_NOW:-$(TZ=Asia/Tokyo date +%H:%M)}"; }
rule_get() { # jqの式 既定値
  if [ -n "$RULES" ]; then jq -r "$1 // empty" "$RULES" 2>/dev/null || true; fi
}
windows_file() { # 窓の一覧を持つファイルを返す（無ければ既定を書く）
  local f="$WORK/windows.json"
  if [ -n "$RULES" ] && jq -e '.mergeGuards.blackoutWindowsJst' "$RULES" >/dev/null 2>&1; then
    echo "$RULES"
  else
    printf '{"mergeGuards":{"blackoutWindowsJst":%s}}' "$DEFAULT_WINDOWS" > "$f"
    echo "$f"
  fi
}

upsert_comment() { # 本文
  local body="$1" id
  case "$body" in "$MARKER"*) ;; *) body="$MARKER"$'\n'"$body" ;; esac # 本文が既にマーカーで始まるなら足さない
  id="$(gh api "repos/$REPO/issues/$PR/comments?per_page=100" --paginate \
    --jq "[.[] | select(.body | startswith(\"$MARKER\")) | .id][0] // empty" | sed -n 1p)"
  if [ -n "$id" ]; then
    gh api -X PATCH "repos/$REPO/issues/comments/$id" -f body="$body" >/dev/null
  else
    gh api -X POST "repos/$REPO/issues/$PR/comments" -f body="$body" >/dev/null
  fi
}

# 1回分の判定とマージ。リポジトリ全体のロックの中で動かす。
# 終了コード: 0=終わり / 10=時間帯のため保留（待ってやり直す。窓の終わりまでの秒数を $WORK/hold に書く）
round() { # 何回目か(0,1)
  local attempt="$1" json ec=0 eligible class status verdict merged=no jf="$WORK/elig.json" md
  json="$($ELIG_CMD --pr "$PR" ${RULES:+--rules "$RULES"} --json)" || ec=$?
  if [ "$ec" -gt 1 ] || ! echo "$json" | jq -e . >/dev/null 2>&1; then
    log "判定エラー" no "判定スクリプトが失敗（終了コード $ec）"
    upsert_comment "判定できなかった（取得失敗）。マージはしていません。" || true
    return 0
  fi
  echo "$json" > "$jf"
  eligible="$(echo "$json" | jq -r '.eligible')"
  class="$(echo "$json" | jq -r '.class // "-"')"
  SHA="$(echo "$json" | jq -r '.headSha // ""')"
  verdict="対象外"; [ "$eligible" = true ] && verdict="対象($class)"

  local hold=0
  if [ "$eligible" != true ]; then
    status="マージ: しない（対象外）"
  elif [ "${AUTOMERGE_ENABLED:-false}" != true ]; then
    status="マージ: 停止中（AUTOMERGE_ENABLED=false、コメントだけ）"
  else
    local now secs bec=0
    now="$(now_jst)"
    node "$GUARDS" in-blackout --now "$now" --rules "$(windows_file)" || bec=$?
    if [ "$bec" -gt 1 ]; then
      # 0=窓の中、1=窓の外。それ以外は設定エラーなどで、確かめられないのでマージしない
      status="マージ: 保留（時間帯の確認に失敗）"
    elif [ "$bec" -eq 0 ]; then
      secs="$(node "$GUARDS" seconds-until-end --now "$now" --rules "$(windows_file)")" || secs=""
      case "$secs" in
        '' | *[!0-9]*) status="マージ: 保留（時間帯の確認に失敗）" ;;
        *)
          if [ "$attempt" -eq 0 ]; then
            status="マージ: 保留（取得・バックアップの時間帯。あと約$((secs / 60))分で再判定します）"
            echo "$((secs + 60))" > "$WORK/hold"
            hold=1
          else
            status="マージ: 保留（再判定でも時間帯の中でした。今回は終了します）"
          fi
          ;;
      esac
    else
      # ここから先の gh は、失敗したら保留にして終わる（round は || の文脈で set -e が効かないため、自分で見る）
      local runs latest running=false fresh prot
      if ! runs="$(gh run list --workflow ci.yml --branch main --event push -L 5 --json status,conclusion)" \
        || ! echo "$runs" | jq -e 'type == "array"' >/dev/null 2>&1; then
        status="マージ: 保留（mainの検査結果を取得できない）"
      else
        latest="$(echo "$runs" | jq -r '[.[] | select(.status == "completed")][0].conclusion // "none"')"
        if [ "$(rule_get '.mergeGuards.blockWhileMainDeployRunning')" = true ] \
          && [ "$(echo "$runs" | jq '[.[] | select(.status != "completed")] | length')" -gt 0 ]; then
          running=true
        fi
        if [ "$running" = true ]; then
          status="マージ: 保留（mainの検査が実行中）"
        elif [ "$latest" != success ]; then
          status="マージ: 保留（mainが赤）"
          [ "$latest" = none ] && status="マージ: 保留（mainの検査結果が見つからない）"
        elif ! fresh="$(gh pr view "$PR" --json headRefOid -q .headRefOid)" || [ -z "$fresh" ]; then
          status="マージ: 保留（PRの最新コミットを確認できない）"
        elif [ -z "$SHA" ] || [ "$fresh" != "$SHA" ]; then
          status="マージ: 保留（判定のあとにコミットが増えた）"
        elif ! prot="$(gh api "repos/$REPO/rules/branches/main")" \
          || [ "$(echo "$prot" | jq '[.[]? | select(.type == "pull_request" or .type == "required_status_checks")] | length' 2>/dev/null || echo 0)" -lt 1 ]; then
          status="マージ: 保留（mainの保護を確認できない）"
        elif gh pr merge "$PR" --squash --match-head-commit "$SHA" >/dev/null; then
          merged=yes
          status="マージ: した"
          gh pr edit "$PR" --add-label automerged >/dev/null 2>&1 || true
        else
          status="マージ: 失敗（gh pr merge が失敗）"
        fi
      fi
    fi
  fi
  # 判定は上の1回だけ。保存したJSONから、ネットワークを使わず本文を作る。
  md="$($ELIG_CMD --render-comment "$jf" --merge-status "$status")" || md="判定の本文を作れなかった。
$status"
  upsert_comment "$md" || true
  log "$verdict" "$merged" "${status#マージ: }"
  [ "$hold" -eq 1 ] && return 10
  return 0
}

locked_round() {
  local rc=0
  ( flock 9; round "$1" ) 9> "$STATE_DIR/merge.lock" || rc=$?
  return "$rc"
}

# --- CI の終了を待つ（結果の良し悪しは判定スクリプトが見る）
gh pr checks "$PR" --watch --interval 30 >/dev/null 2>&1 || true

rc=0
locked_round 0 || rc=$?
if [ "$rc" -eq 10 ]; then
  $SLEEP_CMD "$(cat "$WORK/hold")"
  rc=0
  locked_round 1 || rc=$?
fi
exit "$rc"
