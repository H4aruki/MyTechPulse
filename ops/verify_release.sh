#!/usr/bin/env bash
# manifestと2つのarchiveを検証し、問題が無ければ運用一式をrelease固有のdirectoryへ展開する。
#
#   使い方: MTP_RELEASE_MANIFEST=... MTP_MANIFEST_SHA256=... \
#           MTP_RELEASE_RUN_ID=... MTP_RELEASE_RUN_ATTEMPT=... MTP_RELEASES_ROOT=/絶対path \
#           bash ops/verify_release.sh <同じrunからdownloadしたarchiveのdirectory>
#
# 本番のサーバーにはNode・jq・追加ツールを要求しない。bashと標準のhash/archiveコマンドだけで動く。
# manifestの形式は scripts/release-manifest.mjs が出力する形に1行単位で固定してある。
# 形式を変えるときは、両方を同時に直すこと。
#
# 拒否した場合は終了コード2。拒否した場合にrelease directoryは作らない。
# 成功すると次の3行を標準出力に出し、同じ内容を release directory の release.env にも書く。
#   MTP_RELEASE_DIR / MTP_FRONTEND_ARCHIVE / GO_API_IMAGE
# このscriptは git・docker・curl を一切呼ばない。migration・Cloudflare公開・起動もしない。
set -euo pipefail

reject() {
  echo "拒否: $*" >&2
  exit 2
}

[ "$#" -eq 1 ] && [ -n "$1" ] || reject "使い方: verify_release.sh <archiveのdirectory>"
archive_dir="$1"
[ -d "$archive_dir" ] || reject "archiveのdirectoryがありません"

manifest="${MTP_RELEASE_MANIFEST:-}"
manifest_sha="${MTP_MANIFEST_SHA256:-}"
run_id="${MTP_RELEASE_RUN_ID:-}"
run_attempt="${MTP_RELEASE_RUN_ATTEMPT:-}"
releases_root="${MTP_RELEASES_ROOT:-}"

[ -n "$manifest" ] || reject "MTP_RELEASE_MANIFEST が未設定です"
[ -n "$manifest_sha" ] || reject "MTP_MANIFEST_SHA256 が未設定です"
[ -n "$run_id" ] || reject "MTP_RELEASE_RUN_ID が未設定です"
[ -n "$run_attempt" ] || reject "MTP_RELEASE_RUN_ATTEMPT が未設定です"
[ -n "$releases_root" ] || reject "MTP_RELEASES_ROOT が未設定です"

hex64='[0-9a-f]{64}'
hex40='[0-9a-f]{40}'
digits='[1-9][0-9]*'

[[ "$manifest_sha" =~ ^${hex64}$ ]] || reject "MTP_MANIFEST_SHA256 は64桁の小文字16進数で指定してください"
[[ "$run_id" =~ ^${digits}$ ]] || reject "MTP_RELEASE_RUN_ID は数字で指定してください"
[[ "$run_attempt" =~ ^${digits}$ ]] || reject "MTP_RELEASE_RUN_ATTEMPT は数字で指定してください"
# 空白や特殊文字を含まない絶対pathだけを受け付ける（release.envへ安全に書くため）
[[ "$releases_root" =~ ^/[A-Za-z0-9._/-]+$ ]] || reject "MTP_RELEASES_ROOT は絶対pathで指定してください"
[ -d "$releases_root" ] || reject "MTP_RELEASES_ROOT のdirectoryがありません"

[ -f "$manifest" ] && [ ! -L "$manifest" ] || reject "manifestのファイルがありません"
actual_manifest_sha="$(sha256sum "$manifest" | cut -d' ' -f1)"
[ "$actual_manifest_sha" = "$manifest_sha" ] || reject "manifestのSHA256が保存済みの値と一致しません"

# ---- manifestを1行ずつ、決められた形式と照合する ----
mapfile -t lines < "$manifest"
[ "${#lines[@]}" -eq 20 ] || reject "manifestの形式が正しくありません（行数）"
if grep -q $'\r' "$manifest"; then
  reject "manifestの形式が正しくありません（改行）"
fi

match_line() {
  # $1=行番号(0始まり) $2=正規表現 $3=拒否理由（省略可）。一致した括弧内の値は BASH_REMATCH に残る
  [[ "${lines[$1]}" =~ $2 ]] || reject "manifestの${3:-形式が正しくありません}（$(($1 + 1))行目）"
}

image_repo='ghcr\.io/h4aruki/mytechpulse-api-go'
match_line 0 '^\{$'
match_line 1 '^  "schema_version": 1,$'
match_line 2 "^  \"commit_sha\": \"(${hex40})\",\$"
commit_sha="${BASH_REMATCH[1]}"
# tagだけ・短いdigest・別repositoryは、ここで拒否される
match_line 3 "^  \"api_image\": \"(${image_repo}@sha256:${hex64})\",\$" \
  "api_image が、このrepositoryのdigest指定ではありません"
api_image="${BASH_REMATCH[1]}"
match_line 4 '^  "frontend": \{$'
match_line 5 "^    \"artifact_name\": \"frontend-${commit_sha}\",\$"
match_line 6 "^    \"file\": \"frontend-${commit_sha}\\.tar\\.gz\",\$"
match_line 7 "^    \"sha256\": \"(${hex64})\"\$"
frontend_sha="${BASH_REMATCH[1]}"
match_line 8 '^  \},$'
match_line 9 '^  "ops": \{$'
match_line 10 "^    \"artifact_name\": \"ops-${commit_sha}\",\$"
match_line 11 "^    \"file\": \"ops-${commit_sha}\\.tar\\.gz\",\$"
match_line 12 "^    \"sha256\": \"(${hex64})\"\$"
ops_sha="${BASH_REMATCH[1]}"
match_line 13 '^  \},$'
match_line 14 '^  "workflow": \{$'
match_line 15 "^    \"run_id\": \"(${digits})\",\$"
manifest_run_id="${BASH_REMATCH[1]}"
match_line 16 "^    \"run_attempt\": \"(${digits})\",\$"
manifest_run_attempt="${BASH_REMATCH[1]}"
match_line 17 "^    \"url\": \"https://github\\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/actions/runs/${manifest_run_id}/attempts/${manifest_run_attempt}\"\$"
match_line 18 '^  \}$'
match_line 19 '^\}$'

[ "$manifest_run_id" = "$run_id" ] || reject "manifestの run_id が期待する値と一致しません"
[ "$manifest_run_attempt" = "$run_attempt" ] || reject "manifestの run_attempt が期待する値と一致しません"

frontend_file="frontend-${commit_sha}.tar.gz"
ops_file="ops-${commit_sha}.tar.gz"

# ---- archiveのhashと中身の安全性 ----
check_hash() {
  local name="$1" file="$2" expected="$3" actual
  [ -f "$file" ] && [ ! -L "$file" ] || reject "${name} のarchive（$(basename "$file")）が見つかりません"
  actual="$(sha256sum "$file" | cut -d' ' -f1)"
  [ "$actual" = "$expected" ] || reject "${name} のarchiveのsha256がmanifestと一致しません"
}

# 絶対path、..、symlink、hardlink、特殊fileを含むarchiveは展開しない
check_entries() {
  local name="$1" file="$2" names types type_line entry count_names count_types
  names="$(tar -P -tzf "$file")" || reject "${name} のarchiveを読めません"
  types="$(tar -P -tvzf "$file")" || reject "${name} のarchiveを読めません"
  [ -n "$names" ] || reject "${name} のarchiveが空です"
  count_names="$(printf '%s\n' "$names" | wc -l)"
  count_types="$(printf '%s\n' "$types" | wc -l)"
  [ "$count_names" = "$count_types" ] || reject "${name} のarchiveに不正なfile名があります"
  while IFS= read -r type_line; do
    case "${type_line:0:1}" in
      - | d) ;;
      *) reject "${name} のarchiveにsymlink・hardlink・特殊fileが含まれています" ;;
    esac
  done <<<"$types"
  while IFS= read -r entry; do
    case "$entry" in
      /* | [A-Za-z]:* | *\\*) reject "${name} のarchiveに絶対pathまたは不正な区切りがあります" ;;
    esac
    case "/${entry}/" in
      */../*) reject "${name} のarchiveに .. を含むpathがあります" ;;
    esac
  done <<<"$names"
}

check_hash "frontend" "$archive_dir/$frontend_file" "$frontend_sha"
check_hash "ops" "$archive_dir/$ops_file" "$ops_sha"
check_entries "frontend" "$archive_dir/$frontend_file"
check_entries "ops" "$archive_dir/$ops_file"

# ---- release固有directoryへ展開する（既存directoryは上書きしない） ----
release_dir="${releases_root}/${commit_sha}-${run_id}-${run_attempt}"
if [ -e "$release_dir" ] || [ -L "$release_dir" ]; then
  reject "release directory が既に存在します（上書きしません）: ${release_dir}"
fi
mkdir "$release_dir"

tar -xzf "$archive_dir/$ops_file" -C "$release_dir" --no-same-owner --no-same-permissions
[ -f "$release_dir/docker-compose.yml" ] && [ ! -L "$release_dir/docker-compose.yml" ] ||
  reject "opsのarchiveに docker-compose.yml がありません（${release_dir} は検証失敗のため使わないでください）"
if [ -n "$(find "$release_dir" -type l -print -quit)" ]; then
  reject "展開結果にsymlinkが含まれています（${release_dir} は使わないでください）"
fi

frontend_dest="${release_dir}/${frontend_file}"
if [ -e "$frontend_dest" ] || [ -L "$frontend_dest" ]; then
  reject "opsのarchiveがfrontend archiveと同名のfileを含んでいます（${release_dir} は使わないでください）"
fi
cp "$archive_dir/$frontend_file" "$frontend_dest"
[ "$(sha256sum "$frontend_dest" | cut -d' ' -f1)" = "$frontend_sha" ] ||
  reject "配置したfrontend archiveのsha256が一致しません（${release_dir} は使わないでください）"

result="MTP_RELEASE_DIR=${release_dir}
MTP_FRONTEND_ARCHIVE=${frontend_dest}
GO_API_IMAGE=${api_image}"
printf '%s\n' "$result" > "${release_dir}/release.env"
printf '%s\n' "$result"
