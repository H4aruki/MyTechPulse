#!/usr/bin/env bash
# ディスク使用率と、空きメモリ、スワップ使用量を見て、しきい値を超えたら終了コード 1 と警告を1行出す。
# 超えていなければ何も出さず、終了コード 0（cron の平常時にメールが飛ばないようにするため）。
# 測れないとき（コマンドの失敗、数値でない出力、不正な引数）は終了コード 2。
#
# 使い方:
#   ops/check_resources.sh
#   ops/check_resources.sh --disk-percent 80 --mem-available-mb 100 --swap-used-mb 200
#   ops/check_resources.sh --record-file /path/to/resource-record.log
#
# 環境変数（同じ項目を引数で渡したときは、引数が優先）:
#   CHECK_DISK_PERCENT       既定 80。ルート（/）の使用率がこの値以上なら警告
#   CHECK_MEM_AVAILABLE_MB   既定 100。空きメモリ（free の available）がこの値未満なら警告
#   CHECK_SWAP_USED_MB       既定 200。スワップ使用量がこの値以上なら警告。合計が 0 のときは見ない
#   CHECK_RECORD_FILE        測定値を1行ずつ追記するファイル。未指定（空も同じ）なら記録しない
#
# 記録（CHECK_RECORD_FILE または --record-file を渡したときだけ）:
#   測れたときは、しきい値の内側でも外側でも毎回1行を追記する。標準出力は変えない（平常時は何も出さない）。
#   形式: 日時(JST) disk_used_pct=N mem_available_mb=N swap_used_mb=N|none swap_total_mb=N
#   スワップが無い機械（合計 0）は swap_used_mb=none。
#   追記に失敗したら標準エラーに理由を1行出す。終了コードは測定の結果のまま変えない。
#
# 理由としきい値の説明、cron に入れる例は docs/deploy/logging-and-monitoring.md。
# このスクリプトは測って終了するだけです。本番の crontab へは入れません。
set -euo pipefail

disk_percent="${CHECK_DISK_PERCENT:-80}"
mem_available_mb="${CHECK_MEM_AVAILABLE_MB:-100}"
swap_used_mb="${CHECK_SWAP_USED_MB:-200}"
record_file="${CHECK_RECORD_FILE:-}"

usage() {
  echo "使い方: check_resources.sh [--disk-percent N] [--mem-available-mb N] [--swap-used-mb N] [--record-file PATH]" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --disk-percent)
      [ "$#" -ge 2 ] || usage
      disk_percent="$2"
      shift 2
      ;;
    --mem-available-mb)
      [ "$#" -ge 2 ] || usage
      mem_available_mb="$2"
      shift 2
      ;;
    --swap-used-mb)
      [ "$#" -ge 2 ] || usage
      swap_used_mb="$2"
      shift 2
      ;;
    --record-file)
      [ "$#" -ge 2 ] || usage
      record_file="$2"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

is_uint() {
  [[ "$1" =~ ^[0-9]+$ ]]
}

# 10# を付けて、先頭が 0 の数値を八進数と読まない
as_dec() {
  echo "$((10#$1))"
}

if ! is_uint "$disk_percent" || [ "$(as_dec "$disk_percent")" -lt 1 ] || [ "$(as_dec "$disk_percent")" -gt 100 ]; then
  echo "エラー: ディスクのしきい値は 1 から 100 の整数です" >&2
  exit 2
fi
if ! is_uint "$mem_available_mb" || [ "$(as_dec "$mem_available_mb")" -lt 1 ]; then
  echo "エラー: 空きメモリのしきい値は 1 以上の整数（MB）です" >&2
  exit 2
fi
if ! is_uint "$swap_used_mb" || [ "$(as_dec "$swap_used_mb")" -lt 1 ]; then
  echo "エラー: スワップのしきい値は 1 以上の整数（MB）です" >&2
  exit 2
fi

if ! df_out="$(df -P / 2>/dev/null)"; then
  echo "エラー: df でディスク使用率を読めませんでした" >&2
  exit 2
fi
used_pct="$(printf '%s\n' "$df_out" | awk 'NR==2 { gsub(/%/, "", $5); print $5 }')"
if ! is_uint "$used_pct"; then
  echo "エラー: df の使用率を読み取れませんでした" >&2
  exit 2
fi

if ! free_out="$(free -m 2>/dev/null)"; then
  echo "エラー: free でメモリを読めませんでした" >&2
  exit 2
fi
# 見出しの列名から位置を取る。Mem: や Swap: の行は先頭に名前があるので、見出しより 1 つ右
free_fields="$(printf '%s\n' "$free_out" | awk '
  NR == 1 {
    for (i = 1; i <= NF; i++) {
      if ($i == "available") avail_col = i
      if ($i == "total") total_col = i
      if ($i == "used") used_col = i
    }
    next
  }
  $1 == "Mem:" && avail_col > 0 { mem_avail = $(avail_col + 1) }
  $1 == "Swap:" && total_col > 0 && used_col > 0 {
    swap_total = $(total_col + 1)
    swap_used = $(used_col + 1)
  }
  END { printf "%s|%s|%s\n", mem_avail, swap_total, swap_used }
')"
IFS='|' read -r mem_avail swap_total swap_used <<EOF || true
${free_fields}
EOF
mem_avail="${mem_avail:-}"
swap_total="${swap_total:-}"
swap_used="${swap_used:-}"
if ! is_uint "$mem_avail"; then
  echo "エラー: free の空きメモリ（available）を読み取れませんでした" >&2
  exit 2
fi
if ! is_uint "$swap_total" || ! is_uint "$swap_used"; then
  echo "エラー: free のスワップの行を読み取れませんでした" >&2
  exit 2
fi

# 記録は、測れたあと、しきい値の判定より前に行う。失敗しても終了コードは変えない
if [ -n "$record_file" ]; then
  if [ "$(as_dec "$swap_total")" -gt 0 ]; then
    swap_field="$(as_dec "$swap_used")"
  else
    swap_field="none"
  fi
  record_line="$(TZ=Asia/Tokyo date +%Y-%m-%dT%H:%M:%S%:z) disk_used_pct=$(as_dec "$used_pct") mem_available_mb=$(as_dec "$mem_avail") swap_used_mb=${swap_field} swap_total_mb=$(as_dec "$swap_total")"
  if ! printf '%s\n' "$record_line" >>"$record_file" 2>/dev/null; then
    echo "警告: 測定値を記録ファイルへ追記できませんでした: $record_file" >&2
  fi
fi

warnings=()
if [ "$(as_dec "$used_pct")" -ge "$(as_dec "$disk_percent")" ]; then
  warnings+=("ディスク使用率 ${used_pct}%（しきい値 ${disk_percent}%以上）")
fi
if [ "$(as_dec "$mem_avail")" -lt "$(as_dec "$mem_available_mb")" ]; then
  warnings+=("空きメモリ ${mem_avail}MB（しきい値 ${mem_available_mb}MB未満）")
fi
# スワップが無い機械（合計 0）では、この項目は見ない。開発機で毎回警告にしないため
if [ "$(as_dec "$swap_total")" -gt 0 ] && [ "$(as_dec "$swap_used")" -ge "$(as_dec "$swap_used_mb")" ]; then
  warnings+=("スワップ使用 ${swap_used}MB（しきい値 ${swap_used_mb}MB以上）")
fi

if [ "${#warnings[@]}" -eq 0 ]; then
  exit 0
fi

joined=""
for item in "${warnings[@]}"; do
  if [ -n "$joined" ]; then
    joined="${joined}。${item}"
  else
    joined="$item"
  fi
done
printf '警告: %s\n' "$joined"
exit 1
