#!/usr/bin/env bash
# 本番のGo版APIを、新しい箱へ入れ替える（#125）。本番サーバーで、自動デプロイ（GitHub Actions）が実行する。
#
#   使い方: bash <opsのarchiveを展開した場所>/ops/deploy_go.sh <3成果物のdirectory> <Manifest SHA256>
#     3成果物のdirectory : 同じ実行回の frontend-*.tar.gz・ops-*.tar.gz・release-manifest.json を置いたdirectory
#
# やること（この順番。どれかが失敗したら、そこで止まる）
#   1. 同時に実行されていないことの確認（ロック）
#   2. 入力の確認。manifestのSHA256を照合し、このscriptの置き場所が、照合したopsのarchiveと同じ中身であることを確かめる
#   3. いま動いているGo版（箱の識別子・運用ファイルの場所）の記録。入れ替えに失敗したときの戻し先になる
#   4. 配布物の検証と展開、新しい箱の取得（ops/deploy_release.sh）。まだ何も入れ替えない
#   5. DBのバックアップ（書き込みは止めない）。件数は出さない
#   6. DBの移行（追加だけの変更。入れ替えより先に行う。失敗したら入れ替えない）
#   7. Go版の入れ替え。新しい箱でAPIを起動し、準備ができる（/health/ready が200）まで待つ
#   8. 窓口（Caddy）経由の疎通確認
#   → 7・8が失敗したら、確認なしで、直前の箱へ戻す（DBは巻き戻さない）。戻した後の疎通も確認する
#
# 入れ替えるのはGo版APIだけ。DB・窓口（Caddy）・Python版には触れない。画面（Cloudflare Pages）は別のworkflowが行う。
# 本番のDBの書き込みはしない（動作確認用の登録なども行わない）。動いているものを消さない。
#
# 入力（環境変数。すべて任意）
#   MTP_RELEASES_ROOT        release directoryの置き場（既定: ~/releases）
#   MTP_ENV_FILE             Go版の設定ファイル（既定: ~/mytechpulse-production.env）
#   MTP_COMPOSE_PROJECT      composeのproject名（既定: mytechpulse）
#   MTP_BACKUP_DIR           バックアップのscript（ops/backup_db.sh）がある運用ファイルの場所（既定: ~/MyTechPulse）
#   試験用: MTP_DEPLOY_LOCAL_URL / MTP_DEPLOY_BASE_URL / MTP_DEPLOY_WAIT_SECONDS / MTP_COMPOSE_EXTRA_FILES、
#           MTP_DEPLOY_NEW_LOCAL_URL（新しい箱の確認先だけを変える。戻しの確認は MTP_DEPLOY_LOCAL_URL のまま）、
#           MTP_DEPLOY_PREV_DIR（戻し先の運用ファイルの場所を、composeの記録でなく、指定する）
#
# 出力は工程の名前と秒数・成否、固定の文だけ。設定ファイルの値・パスワード・バックアップの中身は出さない。
# 終了コード: 0=入れ替え成功、1=失敗して直前の箱へ戻した（または入れ替え前に止まった）、
#             2=入力の拒否、3=別のデプロイが実行中、4=戻すことにも失敗した（要・手での確認）
set -euo pipefail
set +x
umask 077

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
boot_dir="$(dirname "$here")"

reject() {
  echo "拒否: $*" >&2
  exit 2
}

[ "$#" -eq 2 ] || reject "使い方: deploy_go.sh <3成果物のdirectory> <Manifest SHA256>"
archive_dir="${1%/}"
manifest_sha="$2"
releases_root="${MTP_RELEASES_ROOT:-$HOME/releases}"
env_file="${MTP_ENV_FILE:-$HOME/mytechpulse-production.env}"
project="${MTP_COMPOSE_PROJECT:-mytechpulse}"
backup_dir="${MTP_BACKUP_DIR:-$HOME/MyTechPulse}"
wait_seconds="${MTP_DEPLOY_WAIT_SECONDS:-60}"

[[ "$manifest_sha" =~ ^[0-9a-f]{64}$ ]] || reject "Manifest SHA256 は64桁の小文字16進数で指定してください"
[[ "$archive_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -d "$archive_dir" ] || reject "3成果物のdirectoryは、空白の無い絶対pathで、存在するものを指定してください"
[[ "$releases_root" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -d "$releases_root" ] || reject "MTP_RELEASES_ROOT は、空白の無い絶対pathで、存在するものを指定してください"
[[ "$env_file" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -f "$env_file" ] && [ ! -L "$env_file" ] || reject "MTP_ENV_FILE（絶対path）のファイルがありません"
[[ "$backup_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -x "$backup_dir/ops/backup_db.sh" ] && [ -x "$backup_dir/ops/verify_backup.sh" ] ||
  reject "バックアップのscriptが $backup_dir/ops/ にありません"
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || reject "MTP_COMPOSE_PROJECT の形式が正しくありません"
[[ "$wait_seconds" =~ ^[0-9]+$ ]] || reject "MTP_DEPLOY_WAIT_SECONDS は数字で指定してください"
manifest="$archive_dir/release-manifest.json"
[ -f "$manifest" ] && [ ! -L "$manifest" ] || reject "release-manifest.json が $archive_dir にありません"

extra_args=()
if [ -n "${MTP_COMPOSE_EXTRA_FILES:-}" ]; then
  for extra_file in $MTP_COMPOSE_EXTRA_FILES; do
    extra_args+=(-f "$extra_file")
  done
fi

env_value() {
  local line
  line="$(grep -m1 "^$1=" "$env_file" || true)"
  printf '%s' "${line#*=}"
}

api_domain="$(env_value API_DOMAIN)"
[[ "$api_domain" =~ ^[A-Za-z0-9.-]+$ ]] || reject "設定ファイルの API_DOMAIN の形式が正しくありません"
local_url="${MTP_DEPLOY_LOCAL_URL:-http://127.0.0.1:8001}"
new_local_url="${MTP_DEPLOY_NEW_LOCAL_URL:-$local_url}"
base_url="${MTP_DEPLOY_BASE_URL:-https://${api_domain}}"

# ---- 1. ロック（同時に2つのデプロイを走らせない） ----
work="$(mktemp -d)"
chmod 700 -- "$work"
lock_dir=""
cleanup() {
  rm -rf -- "$work"
  [ -z "$lock_dir" ] || rmdir -- "$lock_dir" 2>/dev/null || true
}
trap cleanup EXIT

if command -v flock >/dev/null 2>&1; then
  # 本番（Ubuntu）はこちら。プロセスが落ちても、ロックは自動で外れる
  exec 9>"$releases_root/.deploy.lock"
  flock -n 9 || { echo "拒否: 別のデプロイが実行中です" >&2; exit 3; }
else
  # flockが無い環境（手元のWindowsでの試験）用。作れたときだけ、終了時に外す
  if mkdir "$releases_root/.deploy.lock.d" 2>/dev/null; then
    lock_dir="$releases_root/.deploy.lock.d"
  else
    echo "拒否: 別のデプロイが実行中です" >&2
    exit 3
  fi
fi

state_set() { printf '%s' "$2" >"$work/$1"; }
state_get() { cat "$work/$1"; }

try_step() {
  # try_step NAME FUNCTION : 工程を別shellで実行し、名前・秒数・成否だけを出す。失敗の終了コードを返す（終了はしない）
  local started=$SECONDS status
  set +e
  (
    set -e
    "$2"
  )
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    printf 'deploy: %s %ss ok\n' "$1" "$((SECONDS - started))"
  else
    printf 'deploy: %s %ss failed\n' "$1" "$((SECONDS - started))" >&2
  fi
  return "$status"
}

step() {
  # step NAME FUNCTION : 失敗したら、そこで終了する
  try_step "$1" "$2" || exit $?
}

fail_step() {
  echo "deploy: $1 failed ($2)" >&2
  exit 1
}

manifest_value() {
  grep -m1 "^    \"$1\": \"" "$manifest" | sed -E 's/^[^:]+: "([^"]*)".*$/\1/'
}

# 新しいrelease directoryのcomposeで動かす（GO_API_IMAGE は呼び出し側が指定する）
dc() {
  docker compose --env-file "$env_file" -p "$project" -f "$(state_get new_dir)/docker-compose.yml" ${extra_args[@]+"${extra_args[@]}"} "$@"
}

wait_status() {
  # wait_status URL EXPECTED_STATUS : 秒数の上限まで待つ
  local url="$1" expected="$2" tries code
  for ((tries = 0; tries <= wait_seconds; tries++)); do
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null || true)"
    if [ "$code" = "$expected" ]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# ---- 2. 入力の確認 ----
step_inputs() {
  local actual ops_sha ops_file commit tmp
  actual="$(sha256sum "$manifest" | cut -d' ' -f1)"
  [ "$actual" = "$manifest_sha" ] || reject "manifestのSHA256が、渡された値と一致しません"
  commit="$(grep -m1 '^  "commit_sha": "' "$manifest" | sed -E 's/^[^:]+: "([^"]*)".*$/\1/')"
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || reject "manifestからcommitを読み取れません"
  [[ "$(manifest_value run_id)" =~ ^[1-9][0-9]*$ ]] && [[ "$(manifest_value run_attempt)" =~ ^[1-9][0-9]*$ ]] ||
    reject "manifestから実行回を読み取れません"
  # このscriptの置き場所が、照合したopsのarchiveと同じ中身であること（違うscriptで進めない）
  ops_file="$archive_dir/ops-${commit}.tar.gz"
  [ -f "$ops_file" ] && [ ! -L "$ops_file" ] || reject "opsのarchive（ops-${commit}.tar.gz）がありません"
  ops_sha="$(grep -A4 '^  "ops": {' "$manifest" | grep -m1 '"sha256"' | sed -E 's/^[^:]+: "([^"]*)".*$/\1/')"
  [ "$(sha256sum "$ops_file" | cut -d' ' -f1)" = "$ops_sha" ] || reject "opsのarchiveが、manifestの値と一致しません"
  tmp="$work/ops-extract"
  mkdir -m 700 "$tmp"
  tar -xzf "$ops_file" -C "$tmp" --no-same-owner --no-same-permissions || reject "opsのarchiveを展開できません"
  diff -r -q "$tmp" "$boot_dir" >/dev/null 2>&1 ||
    reject "このscriptの置き場所が、照合したopsのarchiveの中身と違います"
}

# ---- 3. いま動いているGo版の記録（入れ替えに失敗したときの戻し先） ----
step_current() {
  local container image dir
  container="$(docker ps -q --filter "label=com.docker.compose.project=${project}" --filter "label=com.docker.compose.service=api-go" | head -1 | tr -d '\r')"
  [ -n "$container" ] || fail_step current "Go版のAPIが動いていない（切り戻し先が分からない）"
  image="$(docker inspect --format '{{.Config.Image}}' "$container" 2>/dev/null | tr -d '\r')"
  [[ "$image" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] ||
    fail_step current "動いているGo版が、digest指定の箱ではない"
  dir="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null | tr -d '\r')"
  # 試験用: 指定があれば、composeの記録でなく、それを使う
  dir="${MTP_DEPLOY_PREV_DIR:-$dir}"
  [[ "$dir" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -f "$dir/docker-compose.yml" ] ||
    fail_step current "動いているGo版の運用ファイルの場所を特定できない"
  state_set prev_image "$image"
  state_set prev_dir "$dir"
}

# ---- 4. 配布物の検証と展開、新しい箱の取得 ----
step_release() {
  local output line release_dir="" record
  # deploy_release.sh が要る、直前のreleaseの記録を、いま動いているGo版から作る
  record="$work/previous-release.json"
  cat >"$record" <<JSON
{
  "manifest_sha256": "running-go-no-manifest",
  "api_image": "$(state_get prev_image)",
  "frontend_deployment_id": "see-cloudflare-pages-deployments",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "running-go-no-hash",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "running-go-no-hash",
  "ops_release_dir": "$(state_get prev_dir)"
}
JSON
  output="$(
    MTP_RELEASE_MANIFEST="$manifest" MTP_MANIFEST_SHA256="$manifest_sha" \
      MTP_RELEASE_RUN_ID="$(manifest_value run_id)" MTP_RELEASE_RUN_ATTEMPT="$(manifest_value run_attempt)" \
      MTP_RELEASES_ROOT="$releases_root" MTP_PREVIOUS_RELEASE_RECORD="$record" \
      bash "$here/deploy_release.sh" "$archive_dir"
  )" || fail_step release "配布物の検証・展開、または箱の取得に失敗した"
  while IFS= read -r line; do
    case "$line" in
      "  release directory: "*) release_dir="${line#  release directory: }" ;;
    esac
  done <<<"$output"
  [ -n "$release_dir" ] && [ -f "$release_dir/docker-compose.yml" ] || fail_step release "release directoryを特定できない"
  state_set new_dir "$release_dir"
  local new_image=""
  while IFS= read -r line; do
    case "$line" in
      GO_API_IMAGE=*) new_image="${line#GO_API_IMAGE=}" ;;
    esac
  done <"$release_dir/release.env"
  [[ "$new_image" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] || fail_step release "新しい箱の識別子を読み取れない"
  state_set new_image "$new_image"
}

# ---- 5. DBのバックアップ（書き込みは止めない。件数などの中身は出さない） ----
step_backup() {
  local dump
  dump="$(cd "$backup_dir" && COMPOSE_PROJECT_NAME="$project" ./ops/backup_db.sh | tail -1)" || fail_step backup "バックアップを取れない"
  [ -n "$dump" ] || fail_step backup "バックアップのpathが分からない"
  (cd "$backup_dir" && COMPOSE_PROJECT_NAME="$project" ./ops/verify_backup.sh "$dump" >/dev/null 2>&1) ||
    fail_step backup "バックアップの検証に失敗した"
}

# ---- 6. DBの移行（追加だけ。入れ替えより先。失敗したら入れ替えない） ----
step_migrate() {
  GO_API_IMAGE="$(state_get new_image)" dc --profile go-migrate run --rm migrate-go >/dev/null 2>&1 ||
    fail_step migrate "migrationに失敗した（入れ替えは行わない。DBは追加分だけの状態で、動いているGo版はそのまま）"
}

# ---- 7・8. 入れ替えと疎通確認 ----
step_swap() {
  GO_API_IMAGE="$(state_get new_image)" dc --profile go-preview up -d --no-deps api-go >/dev/null 2>&1 ||
    fail_step swap "新しい箱を起動できない"
  wait_status "$new_local_url/health/live" 200 || fail_step swap "新しい箱が起動しない（live）"
  wait_status "$new_local_url/health/ready" 200 || fail_step swap "新しい箱の準備ができない（ready）"
}

step_public_check() {
  wait_status "$base_url/health/ready" 200 || fail_step public-check "窓口経由でGo版に届かない"
}

# 直前の箱へ戻す。直前のreleaseの運用ファイルと箱で、そのまま起動し直す（DBは巻き戻さない）
rollback_to_previous() {
  echo "deploy: 失敗したため、直前の箱へ戻します" >&2
  GO_API_IMAGE="$(state_get prev_image)" docker compose --env-file "$env_file" -p "$project" -f "$(state_get prev_dir)/docker-compose.yml" \
    ${extra_args[@]+"${extra_args[@]}"} --profile go-preview up -d --no-deps api-go >/dev/null 2>&1 || return 1
  wait_status "$local_url/health/ready" 200 || return 1
  wait_status "$base_url/health/ready" 200 || return 1
}

step inputs step_inputs
step current step_current
step release step_release
step backup step_backup
step migrate step_migrate

# 入れ替えと確認のどちらかが失敗したら、確認なしで直前の箱へ戻す
swap_status=0
try_step swap step_swap || swap_status=$?
if [ "$swap_status" -eq 0 ]; then
  try_step public-check step_public_check || swap_status=$?
fi
if [ "$swap_status" -ne 0 ]; then
  if rollback_to_previous; then
    echo "deploy: 直前の箱へ戻しました（DBは巻き戻していません）。新しい箱の入れ替えは失敗です" >&2
    exit 1
  fi
  echo "deploy: 直前の箱へ戻すことにも失敗しました。サーバーで状態を確認してください（docker compose ps）" >&2
  exit 4
fi

echo "deploy: ok"
echo "deploy: 新しい箱: $(state_get new_image)"
echo "deploy: 直前の箱（戻し先）: $(state_get prev_image)"
