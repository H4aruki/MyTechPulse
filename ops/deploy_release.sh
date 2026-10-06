#!/usr/bin/env bash
# manifestで指定されたreleaseを「使える状態」まで準備する。公開は切り替えない。
#
#   使い方: MTP_RELEASE_MANIFEST=... MTP_MANIFEST_SHA256=... \
#           MTP_RELEASE_RUN_ID=... MTP_RELEASE_RUN_ATTEMPT=... MTP_RELEASES_ROOT=/絶対path \
#           bash ops/deploy_release.sh <同じrunからdownloadしたarchiveのdirectory>
#
# やること（この順番）
#   1. 直前のreleaseの記録を確かめる（切り戻しの入力になるため、無ければ始めない）
#   2. verify_release.sh で manifest・2つのarchiveのhashと中身を検証し、
#      release固有のdirectoryへ運用一式とfrontend archiveを展開する
#   3. 直前のreleaseの記録を previous-release.json として同じdirectoryへ残す
#   4. 展開したcomposeで、manifestのdigestで指定したAPI imageをpullする
#
# やらないこと: sourceの更新、server上のbuild、不要データの掃除、DBのmigration、
#   Cloudflareへの公開、公開先の切り替え、起動。これらは #127 の手順が承認後に行う。
# 失敗したときは終了するだけで、旧release directory・container・imageには触れない。
#
# 直前のreleaseの記録は、通常は $MTP_RELEASES_ROOT/current-release.json（#127が切り替え後に更新する）。
# 初回（Python版が稼働中）は、稼働中のimage ID/digestと実際のfrontend・compose・ops一式を
# 保全した記録を作り、MTP_PREVIOUS_RELEASE_RECORD でそのpathを渡す。
#
# 終了コード: 2=入力・検証の拒否 / 1=それ以外の失敗（pull失敗など）
set -euo pipefail

reject() {
  echo "拒否: $*" >&2
  exit 2
}

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[ "$#" -eq 1 ] && [ -n "$1" ] || reject "使い方: deploy_release.sh <archiveのdirectory>"
archive_dir="$1"

releases_root="${MTP_RELEASES_ROOT:-}"
[ -n "$releases_root" ] || reject "MTP_RELEASES_ROOT が未設定です"

# ---- 1. 直前のreleaseの記録 ----
previous_record="${MTP_PREVIOUS_RELEASE_RECORD:-${releases_root}/current-release.json}"
# shellcheck source=ops/lib/previous_release_record.sh
source "$here/lib/previous_release_record.sh"
parse_previous_release_record "$previous_record"

# ---- 2. 検証と展開（検証入口は、このscriptと同じ場所にある承認済みのもの） ----
verify_output="$(bash "$here/verify_release.sh" "$archive_dir")"

release_dir=""
api_image=""
while IFS= read -r line; do
  case "$line" in
    MTP_RELEASE_DIR=*) release_dir="${line#MTP_RELEASE_DIR=}" ;;
    GO_API_IMAGE=*) api_image="${line#GO_API_IMAGE=}" ;;
  esac
done <<<"$verify_output"
[ -n "$release_dir" ] && [ -n "$api_image" ] || { echo "検証結果を読み取れませんでした" >&2; exit 1; }

# ---- 3. 切り戻し用の記録を残す ----
cp "$previous_record" "${release_dir}/previous-release.json"

# ---- 4. 展開したcomposeで、digest指定のAPI imageをpullする ----
# project名を固定し、release directoryが変わっても別のvolumeを作らない。
# secretや本番の設定は、このrelease directoryの外にある承認済みの設定から渡す
echo "APIのimageをpullします: ${api_image}"
if ! GO_API_IMAGE="$api_image" docker compose -p mytechpulse -f "${release_dir}/docker-compose.yml" \
  --profile go-preview pull api-go; then
  echo "APIのimageのpullに失敗しました。旧releaseはそのままです（準備途中のdirectory: ${release_dir}）" >&2
  exit 1
fi

echo "release を準備しました（公開は切り替えていません）"
echo "  release directory: ${release_dir}"
echo "  API image: ${api_image}"
echo "  切り戻し用の記録: ${release_dir}/previous-release.json"
