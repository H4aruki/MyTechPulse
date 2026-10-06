#!/usr/bin/env bash
# 直前のreleaseの記録（切り戻しの入力）を読む部品。deploy_release.sh と rehearsal.sh が共有する。
# 単独では実行しない。呼び出し側が `set -euo pipefail` と、拒否して終了する `reject MESSAGE` を用意しておく。

# parse_previous_release_record FILE
# 形式が正しければ、次の変数へ値を入れる。正しくなければ reject する。
#   PREV_API_IMAGE / PREV_FRONTEND_DEPLOYMENT_ID / PREV_OPS_DIR
parse_previous_release_record() {
  local record="$1"
  [ -f "$record" ] && [ ! -L "$record" ] ||
    reject "直前のreleaseの記録がありません（current-release.json、または MTP_PREVIOUS_RELEASE_RECORD）"

  local -a prev_lines
  mapfile -t prev_lines < "$record"
  local -a prev_keys=(manifest_sha256 api_image frontend_deployment_id frontend_artifact_name frontend_sha256 ops_artifact_name ops_sha256 ops_release_dir)
  [ "${#prev_lines[@]}" -eq $((${#prev_keys[@]} + 2)) ] || reject "直前のreleaseの記録の形式が正しくありません（行数）"
  [ "${prev_lines[0]}" = "{" ] && [ "${prev_lines[$((${#prev_keys[@]} + 1))]}" = "}" ] ||
    reject "直前のreleaseの記録の形式が正しくありません（括弧）"

  local safe_value='[A-Za-z0-9._:/@+=-]+'
  local index key line separator
  PREV_API_IMAGE=""
  PREV_FRONTEND_DEPLOYMENT_ID=""
  PREV_OPS_DIR=""
  for index in "${!prev_keys[@]}"; do
    key="${prev_keys[$index]}"
    line="${prev_lines[$((index + 1))]}"
    separator=","
    [ "$index" -eq $((${#prev_keys[@]} - 1)) ] && separator=""
    [[ "$line" =~ ^\ \ \"${key}\":\ \"(${safe_value})\"${separator}$ ]] ||
      reject "直前のreleaseの記録に ${key} が無い、または値が正しくありません"
    case "$key" in
      api_image) PREV_API_IMAGE="${BASH_REMATCH[1]}" ;;
      frontend_deployment_id) PREV_FRONTEND_DEPLOYMENT_ID="${BASH_REMATCH[1]}" ;;
      ops_release_dir) PREV_OPS_DIR="${BASH_REMATCH[1]}" ;;
    esac
  done
  [[ "$PREV_OPS_DIR" == /* ]] && [ -d "$PREV_OPS_DIR" ] ||
    reject "直前のreleaseの運用一式（ops_release_dir）のdirectoryがありません"
}
