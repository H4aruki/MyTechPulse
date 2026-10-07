#!/usr/bin/env bash
# 本番のGo版への切り替えの事前準備を、1コマンドで行う（#127・#161）。本番サーバーで、人間が実行する。
#
#   使い方: bash <opsのarchiveを展開した場所>/ops/cutover_prepare.sh <3成果物のdirectory> <ActionsのSummaryのManifest SHA256> [旧版の場所]
#     3成果物のdirectory : 同じ実行回の frontend-*.tar.gz・ops-*.tar.gz・release-manifest.json を置いたdirectory
#     旧版の場所         : いま本番で動いているPython版の運用ファイルの場所（既定: ~/MyTechPulse）
#
# やること（この順番。どれかが失敗したら、そこで止まる）
#   1. 入力の確認。manifestのSHA256を、Summaryに出た値と照合する。このscriptの置き場所の中身が、
#      照合したopsのarchiveと同じであることも確かめる
#   2. 本番データの事前確認（読み取りだけ）。タグの衝突、利用者名の長さ
#   3. 切り戻し用の記録（previous-release.json）を、いま動いているPython版から作る。
#      Python版の箱には、消えないように別名（mytechpulse-legacy-api:pre-go-<日付>）を付ける
#   4. 配布物の検証と展開、Go版の箱のpull（ops/deploy_release.sh）。公開は切り替わらない
#   5. Go版の箱の中身の確認。ファイル一覧・環境変数・ビルド履歴に、想定外のものや秘密らしき語が無いこと
#      （箱は公開されているため、配布のたびに確かめる。docs/deploy/go-migration-rehearsal.md §2-2）
#   6. Go版の設定ファイル（~/mytechpulse-production.env）が無ければ、旧版の設定から作る。あれば触らない
#   7. 切り替えの事前確認（cutover.sh preflight）。何も変えない
# 最後に、当日に実行するコマンドを表示する。
#
# 入力（環境変数。すべて任意）
#   MTP_RELEASES_ROOT           release directoryの置き場（既定: ~/releases）
#   MTP_ENV_FILE                Go版の設定ファイル（既定: ~/mytechpulse-production.env）
#   MTP_CORS_ALLOWED_ORIGINS    設定ファイルを作るときの、画面のオリジン
#                               （既定: https://mytechpulse.net,https://www.mytechpulse.net）
#
# 出力は工程の名前と成否、固定の文だけ。設定ファイルの値・パスワード・トークンは出さない。
# 変えるもの: previous-release.json、release directory、設定ファイル（無いときだけ）、Python版の箱への別名、
#   箱の中身の確認に使う一時のコンテナ（確認が終わると片付ける）
# 変えないもの: 動いているサービス、データベース、Caddy、旧版の運用ファイル
# 終了コード: 0=成功、1=工程の失敗、2=入力の拒否
set -euo pipefail
set +x
umask 077

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
boot_dir="$(dirname "$here")"

reject() {
  echo "拒否: $*" >&2
  exit 2
}

fail_step() {
  echo "prepare: $1 failed ($2)" >&2
  exit 1
}

[ "$#" -ge 2 ] && [ "$#" -le 3 ] || reject "使い方: cutover_prepare.sh <3成果物のdirectory> <Manifest SHA256> [旧版の場所]"
archive_dir="${1%/}"
manifest_sha="$2"
legacy_dir="${3:-$HOME/MyTechPulse}"
legacy_dir="${legacy_dir%/}"
releases_root="${MTP_RELEASES_ROOT:-$HOME/releases}"
env_file="${MTP_ENV_FILE:-$HOME/mytechpulse-production.env}"
cors_origins="${MTP_CORS_ALLOWED_ORIGINS:-https://mytechpulse.net,https://www.mytechpulse.net}"
project="mytechpulse"

[[ "$manifest_sha" =~ ^[0-9a-f]{64}$ ]] || reject "Manifest SHA256 は64桁の小文字16進数で指定してください"
[ -d "$archive_dir" ] || reject "3成果物のdirectoryがありません"
[[ "$archive_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] || reject "3成果物のdirectoryは、空白の無い絶対pathで指定してください"
[[ "$legacy_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -f "$legacy_dir/docker-compose.yml" ] ||
  reject "旧版の運用ファイル（docker-compose.yml）が $legacy_dir にありません"
[[ "$releases_root" =~ ^/[A-Za-z0-9._/-]+$ ]] || reject "MTP_RELEASES_ROOT は、空白の無い絶対pathで指定してください"
[[ "$env_file" =~ ^/[A-Za-z0-9._/-]+$ ]] || reject "MTP_ENV_FILE は、空白の無い絶対pathで指定してください"
[[ "$cors_origins" =~ ^https://[A-Za-z0-9.-]+(,https://[A-Za-z0-9.-]+)*$ ]] ||
  reject "MTP_CORS_ALLOWED_ORIGINS は、https のオリジンをカンマ区切りで指定してください"
manifest="$archive_dir/release-manifest.json"
[ -f "$manifest" ] && [ ! -L "$manifest" ] || reject "release-manifest.json が $archive_dir にありません"

work="$(mktemp -d)"
chmod 700 -- "$work"
trap 'rm -rf -- "$work"' EXIT

step() {
  # step NAME FUNCTION : 工程の名前と秒数、成否だけを出す
  local started=$SECONDS status
  set +e
  (
    set -e
    "$2"
  )
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    printf 'prepare: %s %ss ok\n' "$1" "$((SECONDS - started))"
  else
    printf 'prepare: %s %ss failed\n' "$1" "$((SECONDS - started))" >&2
    exit "$status"
  fi
}

legacy_dc() {
  (cd "$legacy_dir" && COMPOSE_PROJECT_NAME="$project" docker compose "$@")
}

# ---- 1. 入力の確認 ----
# manifestの形式は verify_release.sh が厳密に確かめる。ここでは、その検証に渡す値を取り出すだけ
manifest_value() {
  grep -m1 "^    \"$1\": \"" "$manifest" | sed -E 's/^[^:]+: "([^"]*)".*$/\1/'
}

step_inputs() {
  local actual ops_sha ops_file commit tmp
  actual="$(sha256sum "$manifest" | cut -d' ' -f1)"
  [ "$actual" = "$manifest_sha" ] || reject "manifestのSHA256が、Summaryの値と一致しません"
  run_id="$(manifest_value run_id)"
  run_attempt="$(manifest_value run_attempt)"
  [[ "$run_id" =~ ^[1-9][0-9]*$ ]] && [[ "$run_attempt" =~ ^[1-9][0-9]*$ ]] || reject "manifestから実行回を読み取れません"
  commit="$(grep -m1 '^  "commit_sha": "' "$manifest" | sed -E 's/^[^:]+: "([^"]*)".*$/\1/')"
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || reject "manifestからcommitを読み取れません"

  # このscriptの置き場所（opsのarchiveを展開したもの）が、照合したarchiveと同じであること。
  # 違うscriptで検証を進めないための確認
  ops_file="$archive_dir/ops-${commit}.tar.gz"
  [ -f "$ops_file" ] && [ ! -L "$ops_file" ] || reject "opsのarchive（ops-${commit}.tar.gz）がありません"
  ops_sha="$(grep -A4 '^  "ops": {' "$manifest" | grep -m1 '"sha256"' | sed -E 's/^[^:]+: "([^"]*)".*$/\1/')"
  [ "$(sha256sum "$ops_file" | cut -d' ' -f1)" = "$ops_sha" ] || reject "opsのarchiveが、manifestの値と一致しません"
  tmp="$work/ops-extract"
  mkdir -m 700 "$tmp"
  tar -xzf "$ops_file" -C "$tmp" --no-same-owner --no-same-permissions || reject "opsのarchiveを展開できません"
  diff -r -q "$tmp" "$boot_dir" >/dev/null 2>&1 ||
    reject "このscriptの置き場所が、照合したopsのarchiveの中身と違います（archiveを展開し直してください）"
}

# ---- 2. 本番データの事前確認（読み取りだけ） ----
step_audit() {
  local result
  # 衝突があるとSQLが失敗する（0以外が出る）。中身は表に出さない
  result="$(legacy_dc exec -T db psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d mytechpulse <"$here/sql/audit_tag_collisions.sql" 2>/dev/null | tr -d '\r' | tail -1)" ||
    fail_step audit "タグの衝突を確認できない、または衝突がある"
  [ "$result" = "0" ] || fail_step audit "タグの衝突がある"
  result="$(legacy_dc exec -T db psql -X -q -At -U postgres -d mytechpulse \
    -c "SELECT count(*) FROM \"user\" WHERE char_length(user_name) > 50 OR user_name <> btrim(user_name)" 2>/dev/null | tr -d '\r' | tail -1)" ||
    fail_step audit "利用者名を確認できない"
  [ "$result" = "0" ] || fail_step audit "50文字を超える、または前後に空白がある利用者名がある"
}

# ---- 3. 切り戻し用の記録 ----
step_previous_record() {
  local container container_image image_ref image_id container_created image_created tag
  container="$(legacy_dc ps -q api 2>/dev/null | head -1 | tr -d '\r')"
  [ -n "$container" ] || fail_step previous-record "Python版のAPIが動いていない"

  # 1. コンテナが持つ識別子で箱を引く（従来の保存方式ではこれで引ける）
  container_image="$(docker inspect --format '{{.Image}}' "$container" 2>/dev/null | tr -d '\r')"
  image_id="$(docker image inspect "$container_image" --format '{{.Id}}' 2>/dev/null | tr -d '\r' || true)"

  # 2. 引けないとき（containerdの保存方式では、コンテナが持つ識別子で箱を引けない）は、箱の名前から引く。
  #    ただし、名前の箱がいま動いているものと同じとは限らない（作り直された後かもしれない）ので、
  #    コンテナが箱より後に作られていること（＝いま動いているのが、この箱であること）を確かめる
  if [[ ! "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    image_ref="$(docker inspect --format '{{.Config.Image}}' "$container" 2>/dev/null | tr -d '\r')"
    [[ "$image_ref" =~ ^[A-Za-z0-9._/:@-]+$ ]] || fail_step previous-record "Python版の箱の名前を取得できない"
    image_id="$(docker image inspect "$image_ref" --format '{{.Id}}' 2>/dev/null | tr -d '\r' || true)"
    [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || fail_step previous-record "Python版の箱の識別子を取得できない"
    container_created="$(date -d "$(docker inspect --format '{{.Created}}' "$container" 2>/dev/null | tr -d '\r')" +%s 2>/dev/null || true)"
    image_created="$(date -d "$(docker image inspect "$image_ref" --format '{{.Created}}' 2>/dev/null | tr -d '\r')" +%s 2>/dev/null || true)"
    [[ "$container_created" =~ ^[0-9]+$ ]] && [[ "$image_created" =~ ^[0-9]+$ ]] ||
      fail_step previous-record "コンテナと箱の作成日時を確認できない"
    [ "$container_created" -ge "$image_created" ] ||
      fail_step previous-record "箱が、いま動いているコンテナより後に作り直されている（動いている箱と同じ保証がない）"
  fi

  # 別名を付けておく（以後 `docker compose build` などで元の名前が付け替わっても、箱が消えない）
  tag="mytechpulse-legacy-api:pre-go-$(date -u +%Y%m%d)"
  docker tag "$image_id" "$tag" >/dev/null 2>&1 || fail_step previous-record "Python版の箱に別名を付けられない"

  mkdir -p "$releases_root"
  record="$releases_root/previous-release.json"
  cat >"$record.tmp" <<JSON
{
  "manifest_sha256": "legacy-python-no-manifest",
  "api_image": "${tag}",
  "frontend_deployment_id": "see-cloudflare-pages-deployments",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "legacy-no-hash",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "legacy-no-hash",
  "ops_release_dir": "${legacy_dir}"
}
JSON
  mv -f -- "$record.tmp" "$record"
}

# ---- 4. 配布物の検証と展開、Go版の箱のpull ----
step_release() {
  local output line
  output="$(
    MTP_RELEASE_MANIFEST="$manifest" MTP_MANIFEST_SHA256="$manifest_sha" \
      MTP_RELEASE_RUN_ID="$run_id" MTP_RELEASE_RUN_ATTEMPT="$run_attempt" \
      MTP_RELEASES_ROOT="$releases_root" MTP_PREVIOUS_RELEASE_RECORD="$record" \
      bash "$here/deploy_release.sh" "$archive_dir"
  )" || fail_step release "配布物の検証・展開、または箱のpullに失敗した"
  release_dir=""
  while IFS= read -r line; do
    case "$line" in
      "  release directory: "*) release_dir="${line#  release directory: }" ;;
    esac
  done <<<"$output"
  [ -n "$release_dir" ] && [ -d "$release_dir" ] || fail_step release "release directoryを特定できない"
  printf '%s' "$release_dir" >"$work/release_dir"
}

# ---- 5. Go版の箱の中身の確認 ----
step_image_inspect() {
  local image="" line cid files extra env_json
  while IFS= read -r line; do
    case "$line" in
      GO_API_IMAGE=*) image="${line#GO_API_IMAGE=}" ;;
    esac
  done <"$(cat "$work/release_dir")/release.env"
  [[ "$image" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] ||
    fail_step image-inspect "箱の識別子を読み取れない"

  # 箱を起動せずに作り、中身の一覧だけを見て、確認が終わったら片付ける
  cid="$(docker create "$image" 2>/dev/null | tr -d '\r')" || fail_step image-inspect "箱から確認用のコンテナを作れない"
  [ -n "$cid" ] || fail_step image-inspect "箱から確認用のコンテナを作れない"
  files="$(docker export "$cid" 2>/dev/null | tar -t 2>/dev/null | grep -v '/$' || true)"
  docker rm "$cid" >/dev/null 2>&1 || true
  # 期待するのは、api・migrate・証明書と、Dockerが付ける空の項目だけ
  extra="$(printf '%s\n' "$files" | grep -vxE 'api|migrate|etc/ssl/certs/ca-certificates\.crt|\.dockerenv|dev/console|etc/hostname|etc/hosts|etc/mtab|etc/resolv\.conf|' || true)"
  [ -z "$extra" ] || fail_step image-inspect "箱に想定外のファイルがある"
  printf '%s\n' "$files" | grep -qx 'api' && printf '%s\n' "$files" | grep -qx 'migrate' ||
    fail_step image-inspect "箱に api または migrate が無い"

  # 環境変数は PATH だけ（秘密の値が焼き込まれていない）
  env_json="$(docker image inspect "$image" --format '{{json .Config.Env}}' 2>/dev/null | tr -d '\r')" ||
    fail_step image-inspect "箱の環境変数を確認できない"
  [[ "$env_json" =~ ^\[\"PATH=[^\"]*\"\]$ ]] || fail_step image-inspect "箱に PATH 以外の環境変数がある"

  # ビルドの履歴に、秘密らしき語が無い
  if docker history --no-trunc "$image" --format '{{.CreatedBy}}' 2>/dev/null | grep -qiE 'token|secret|password|key'; then
    fail_step image-inspect "箱のビルド履歴に、秘密らしき語がある"
  fi
}

# ---- 6. 設定ファイル ----
read_config_value() {
  # read_config_value FILE KEY : 値を標準出力へ。値は出力の表示には使わない
  local line
  line="$(grep -m1 "^$2=" "$1" 2>/dev/null || true)"
  [ -n "${line#*=}" ] || return 1
  printf '%s' "${line#*=}"
}

step_env_file() {
  if [ -f "$env_file" ]; then
    # 既にあるものは触らない（権限だけ確かめる）
    chmod 600 -- "$env_file"
    return 0
  fi
  local password domain token
  password="$(read_config_value "$legacy_dir/.env" POSTGRES_PASSWORD)" ||
    fail_step env-file "旧版の .env に POSTGRES_PASSWORD がない"
  domain="$(read_config_value "$legacy_dir/.env" API_DOMAIN)" ||
    fail_step env-file "旧版の .env に API_DOMAIN がない"
  token="$(read_config_value "$legacy_dir/backend/.env" QIITA_ACCESS_TOKEN)" ||
    fail_step env-file "旧版の backend/.env に QIITA_ACCESS_TOKEN がない"
  {
    printf 'POSTGRES_PASSWORD=%s\n' "$password"
    printf 'API_DOMAIN=%s\n' "$domain"
    printf 'APP_ENV=production\n'
    printf 'QIITA_ACCESS_TOKEN=%s\n' "$token"
    printf 'CORS_ALLOWED_ORIGINS=%s\n' "$cors_origins"
    printf 'SWAGGER_ENABLED=false\n'
  } >"$env_file.tmp"
  chmod 600 -- "$env_file.tmp"
  mv -- "$env_file.tmp" "$env_file"
}

# ---- 7. 切り替えの事前確認（何も変えない） ----
step_preflight() {
  MTP_ENV_FILE="$env_file" bash "$(cat "$work/release_dir")/ops/cutover.sh" preflight >/dev/null ||
    fail_step preflight "事前確認に失敗した。cutover.sh preflight の出力を確認してください"
}

record=""
run_id=""
run_attempt=""

# 各工程は別のshellで実行されるため、後の工程で使う値は、ここで（検証済みのmanifestから）取り直すか、$work に残す
step inputs step_inputs
run_id="$(manifest_value run_id)"
run_attempt="$(manifest_value run_attempt)"
step audit step_audit
step previous-record step_previous_record
record="$releases_root/previous-release.json"
step release step_release
step image-inspect step_image_inspect
step env-file step_env_file
step preflight step_preflight

release_dir="$(cat "$work/release_dir")"
echo "prepare: ok"
echo "prepare: 当日は、サーバーで次の2つを実行します（間に、新しい画面をCloudflare Pagesへ公開します）"
echo "  bash ${release_dir}/ops/cutover.sh part1"
echo "  bash ${release_dir}/ops/cutover.sh part2"
