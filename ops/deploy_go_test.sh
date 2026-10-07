#!/usr/bin/env bash
# ops/deploy_go.sh を、偽のdocker・curlと、本物の配布検証script（verify_release.sh・deploy_release.sh）で確かめる（#125）。
# 本物のdocker・本番・ネットワークには触れない。設定ファイルの値はすべて合成のダミー。
# 使い方: bash ops/deploy_go_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  [ -z "${FX_TMP:-}" ] || cat "$FX_TMP/out" "$FX_TMP/err" 2>/dev/null >&2 || true
  exit 1
}

run_case() {
  local name="$1" status
  shift
  set +e
  (
    set -e
    setup
    trap 'rm -rf "$FX_TMP"' EXIT
    "$@"
  )
  status=$?
  set -e
  [ "$status" -eq 0 ] || fail "$name"
  passed=$((passed + 1))
  printf 'ok: %s\n' "$name"
}

SECRET_PASSWORD='S3cretDbPassword-synthetic-0001'
PREV_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:$(printf 'b%.0s' $(seq 1 64))"

setup() {
  fx_init
  # 本番の配布と同じ作りのopsのarchive（リポジトリの運用ファイル一式）を作る
  local src="$FX_TMP/src/ops-real"
  mkdir -p "$src"
  (cd "$repo_root" && tar -cf - docker-compose.yml Caddyfile ops) | tar -xf - -C "$src"
  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src" docker-compose.yml Caddyfile ops
  fx_write_manifest
  MANIFEST_SHA="$MTP_MANIFEST_SHA256"
  unset MTP_RELEASE_MANIFEST MTP_MANIFEST_SHA256 MTP_RELEASE_RUN_ID MTP_RELEASE_RUN_ATTEMPT MTP_RELEASES_ROOT

  # opsのarchiveを、実際の自動デプロイと同じように展開する
  BOOT="$FX_TMP/boot"
  mkdir -p "$BOOT"
  tar -xzf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$BOOT"

  # いま動いているGo版のrelease directory（戻し先）
  PREV_DIR="$FX_TMP/previous-release-dir"
  mkdir -p "$PREV_DIR"
  echo 'services: {}' >"$PREV_DIR/docker-compose.yml"

  # バックアップのscriptがある運用ファイルの場所（偽物）
  BACKUP="$FX_TMP/legacy"
  mkdir -p "$BACKUP/ops"
  cat >"$BACKUP/ops/backup_db.sh" <<'FAKE'
#!/usr/bin/env bash
echo "backup cwd=${PWD##*/} project=${COMPOSE_PROJECT_NAME:-}" >>"$FAKE_CALLS"
[ "${FAKE_BACKUP_FAIL:-}" != 1 ] || exit 1
echo "backups/mytechpulse_20261007T000000Z.dump"
FAKE
  cat >"$BACKUP/ops/verify_backup.sh" <<'FAKE'
#!/usr/bin/env bash
echo "verify-backup $1" >>"$FAKE_CALLS"
[ "${FAKE_VERIFY_FAIL:-}" != 1 ] || exit 1
FAKE
  chmod +x "$BACKUP/ops/backup_db.sh" "$BACKUP/ops/verify_backup.sh"

  ENVF="$FX_TMP/production.env"
  printf 'POSTGRES_PASSWORD=%s\nAPI_DOMAIN=api.example.test\nAPP_ENV=production\n' "$SECRET_PASSWORD" >"$ENVF"
  chmod 600 "$ENVF"

  export MTP_RELEASES_ROOT="$FX_ROOT" MTP_ENV_FILE="$ENVF" MTP_BACKUP_DIR="$BACKUP"
  export MTP_DEPLOY_LOCAL_URL=http://local.test MTP_DEPLOY_BASE_URL=http://base.test MTP_DEPLOY_WAIT_SECONDS=1

  export FAKE_CALLS="$FX_TMP/calls.log" FAKE_STATE="$FX_TMP/state" FAKE_PREV_DIR="$PREV_DIR" FAKE_PREV_IMAGE="$PREV_IMAGE"
  export FAKE_NEW_IMAGE="$FX_IMAGE"
  mkdir -p "$FAKE_STATE"
  : >"$FAKE_CALLS"
  # 入れ替える前に動いているのは、直前の箱
  printf '%s' "$PREV_IMAGE" >"$FAKE_STATE/running"
  : >"$FX_TMP/all.log"
  unset FAKE_NO_GO FAKE_BACKUP_FAIL FAKE_VERIFY_FAIL FAKE_PULL_FAIL FAKE_MIGRATE_FAIL FAKE_SWAP_FAIL FAKE_NEW_UNHEALTHY \
    FAKE_PUBLIC_FAIL FAKE_ALL_UNHEALTHY FAKE_ROLLBACK_START_FAIL
  setup_fakes
}

setup_fakes() {
  mkdir -p "$FX_TMP/bin"
  cat >"$FX_TMP/bin/docker" <<'FAKE'
#!/usr/bin/env bash
args="$*"
printf 'docker [image=%s] %s\n' "${GO_API_IMAGE:-}" "$args" >>"$FAKE_CALLS"
case "$args" in
  "ps -q --filter "*"service=api-go"*) [ "${FAKE_NO_GO:-}" = 1 ] || echo cid-go ;;
  "inspect --format {{.Config.Image}} cid-go") echo "$FAKE_PREV_IMAGE" ;;
  "inspect --format {{index .Config.Labels \"com.docker.compose.project.working_dir\"}} cid-go") echo "$FAKE_PREV_DIR" ;;
  *"--profile go-preview pull api-go"*) [ "${FAKE_PULL_FAIL:-}" != 1 ] || exit 1 ;;
  *"run --rm migrate-go"*) [ "${FAKE_MIGRATE_FAIL:-}" != 1 ] || exit 1 ;;
  *"--profile go-preview up -d --no-deps api-go"*)
    if [ "${FAKE_SWAP_FAIL:-}" = 1 ] && [ "${GO_API_IMAGE:-}" = "$FAKE_NEW_IMAGE" ]; then exit 1; fi
    if [ "${FAKE_ROLLBACK_START_FAIL:-}" = 1 ] && [ "${GO_API_IMAGE:-}" = "$FAKE_PREV_IMAGE" ]; then exit 1; fi
    printf '%s' "${GO_API_IMAGE:-}" >"$FAKE_STATE/running" ;;
esac
exit 0
FAKE
  cat >"$FX_TMP/bin/curl" <<'FAKE'
#!/usr/bin/env bash
url="${*: -1}"
running="$(cat "$FAKE_STATE/running")"
code=000
case "$url" in
  http://local.test/* | http://base.test/*)
    code=200
    if [ "$running" = "$FAKE_NEW_IMAGE" ]; then
      [ "${FAKE_NEW_UNHEALTHY:-}" != 1 ] || code=503
      case "$url" in http://base.test/*) [ "${FAKE_PUBLIC_FAIL:-}" != 1 ] || code=502 ;; esac
    fi
    [ "${FAKE_ALL_UNHEALTHY:-}" != 1 ] || code=503 ;;
esac
echo "curl $url -> $code (running=${running:7:12})" >>"$FAKE_CALLS"
printf '%s' "$code"
exit 0
FAKE
  chmod +x "$FX_TMP/bin/docker" "$FX_TMP/bin/curl"
  export PATH="$FX_TMP/bin:$PATH"
}

deploy() {
  set +e
  bash "$BOOT/ops/deploy_go.sh" "$FX_ART" "${1:-$MANIFEST_SHA}" >"$FX_TMP/out" 2>"$FX_TMP/err"
  STATUS=$?
  set -e
  cat "$FX_TMP/out" "$FX_TMP/err" >>"$FX_TMP/all.log"
}

expect() {
  # expect 終了コード [Manifest SHA256]
  deploy "${2:-}"
  [ "$STATUS" -eq "$1" ] || {
    cat "$FX_TMP/out" "$FX_TMP/err" "$FAKE_CALLS" >&2
    fail "終了コード $STATUS（$1 を期待）"
  }
}
has() { grep -qE -- "$1" "$FAKE_CALLS"; }
expect_has() { has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼び出しが無い: $1"; }; }
expect_lacks() { ! has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼ばれてはいけない: $1"; }; }
out_has() { grep -qE -- "$1" "$FX_TMP/out" "$FX_TMP/err"; }
first_line() { grep -nE -m1 -- "$1" "$FAKE_CALLS" | cut -d: -f1 || true; }
expect_order() {
  local previous=0 current pattern
  for pattern in "$@"; do
    current="$(first_line "$pattern")"
    [ -n "$current" ] || { cat "$FAKE_CALLS" >&2; fail "呼び出しが無い: $pattern"; }
    [ "$current" -gt "$previous" ] || { cat "$FAKE_CALLS" >&2; fail "順序が違う: $pattern"; }
    previous="$current"
  done
}
running_now() { cat "$FAKE_STATE/running"; }

# 何も入れ替えていない（起動し直しもしていない）
nothing_swapped() {
  expect_lacks 'up -d'
  [ "$(running_now)" = "$PREV_IMAGE" ] || fail "動いている箱が変わった"
}

# 直前の箱へ戻っている
rolled_back() {
  [ "$(running_now)" = "$PREV_IMAGE" ] || fail "直前の箱へ戻っていない"
  # 最後の起動は、直前の箱で、直前のreleaseの運用ファイルを使っている
  local last
  last="$(grep -E 'up -d --no-deps api-go' "$FAKE_CALLS" | tail -1)"
  case "$last" in
    *"image=$PREV_IMAGE"*"-f $PREV_DIR/docker-compose.yml"*) ;;
    *) fail "戻しの起動が、直前の箱・運用ファイルでない: $last" ;;
  esac
  out_has '直前の箱へ戻しました' || fail "戻した表示が無い"
}

# ---- 成功 ----

case_deploy_succeeds_in_order() {
  expect 0
  out_has 'deploy: ok' || fail "完了の表示が無い"
  expect_order 'ps -q --filter' 'pull api-go' 'backup cwd=legacy project=mytechpulse' 'verify-backup backups/' \
    "image=${FX_IMAGE}] .*run --rm migrate-go" "image=${FX_IMAGE}] .*up -d --no-deps api-go" 'curl http://local.test/health/ready -> 200' \
    'curl http://base.test/health/ready -> 200'
  [ "$(running_now)" = "$FX_IMAGE" ] || fail "新しい箱が動いていない"
  out_has "新しい箱: $FX_IMAGE" || fail "新しい箱が表示されていない"
  out_has "直前の箱（戻し先）: $PREV_IMAGE" || fail "戻し先が表示されていない"
  local release_dir
  release_dir="$(ls -d "$FX_ROOT"/${FX_COMMIT}-* | head -1)"
  [ -f "$release_dir/previous-release.json" ] || fail "切り戻し用の記録が無い"
  grep -q "\"api_image\": \"$PREV_IMAGE\"" "$release_dir/previous-release.json" || fail "切り戻し用の記録が、直前の箱でない"
  grep -q "\"ops_release_dir\": \"$PREV_DIR\"" "$release_dir/previous-release.json" || fail "切り戻し用の記録が、直前の運用ファイルでない"
}

case_deploy_uses_only_the_new_release_files() {
  expect 0
  local release_dir
  release_dir="$(ls -d "$FX_ROOT"/${FX_COMMIT}-* | head -1)"
  # 入れ替えは、検証済みの新しいreleaseのcomposeで行う（checkoutのcomposeではない）
  expect_has "up -d --no-deps api-go"
  grep -E 'up -d --no-deps api-go' "$FAKE_CALLS" | head -1 | grep -q -- "-f $release_dir/docker-compose.yml" || fail "新しいreleaseのcomposeを使っていない"
  grep -E 'up -d --no-deps api-go' "$FAKE_CALLS" | head -1 | grep -q -- "--env-file $ENVF" || fail "設定ファイルを渡していない"
}

case_deploy_changes_only_the_go_api() {
  expect 0
  expect_lacks ' up -d .*(db|caddy)'
  expect_lacks '--no-deps (db|caddy|api)$'
  expect_lacks ' stop '
  expect_lacks ' down'
  expect_lacks 'pg_restore|dropdb|DROP |volume '
}

case_deploy_never_prints_secrets() {
  expect 0
  if grep -qF -- "$SECRET_PASSWORD" "$FX_TMP/all.log" "$FAKE_CALLS"; then
    fail "出力か呼び出しに機密値が出ている"
  fi
}

# ---- 入れ替え前に止まる（何も入れ替えない） ----

case_pull_failure_stops_before_anything() {
  FAKE_PULL_FAIL=1 expect 1
  out_has 'release .*failed' || fail "失敗した工程が表示されていない"
  expect_lacks 'migrate-go'
  expect_lacks 'backup cwd'
  nothing_swapped
}

case_backup_failure_stops_before_migration() {
  FAKE_BACKUP_FAIL=1 expect 1
  out_has 'backup .*failed' || fail "失敗した工程が表示されていない"
  expect_lacks 'migrate-go'
  nothing_swapped
}

case_backup_verification_failure_stops_before_migration() {
  FAKE_VERIFY_FAIL=1 expect 1
  out_has 'バックアップの検証に失敗' || fail "理由が表示されていない"
  expect_lacks 'migrate-go'
  nothing_swapped
}

case_migration_failure_keeps_the_running_version() {
  FAKE_MIGRATE_FAIL=1 expect 1
  out_has 'migrate .*failed' || fail "失敗した工程が表示されていない"
  out_has '入れ替えは行わない' || fail "入れ替えないことが表示されていない"
  nothing_swapped
}

case_no_running_go_version_stops_before_anything() {
  FAKE_NO_GO=1 expect 1
  out_has 'current .*failed' || fail "失敗した工程が表示されていない"
  expect_lacks 'pull api-go'
  expect_lacks 'migrate-go'
  nothing_swapped
}

case_running_version_must_be_pinned_by_digest() {
  FAKE_PREV_IMAGE=ghcr.io/h4aruki/mytechpulse-api-go:latest expect 1
  out_has 'digest指定の箱ではない' || fail "理由が表示されていない"
  expect_lacks 'migrate-go'
  nothing_swapped
}

# ---- 入れ替えの失敗（直前の箱へ戻す） ----

case_new_image_that_cannot_start_is_rolled_back() {
  FAKE_SWAP_FAIL=1 expect 1
  out_has 'swap .*failed' || fail "失敗した工程が表示されていない"
  rolled_back
}

case_new_image_that_is_not_ready_is_rolled_back() {
  FAKE_NEW_UNHEALTHY=1 expect 1
  out_has 'swap .*failed' || fail "失敗した工程が表示されていない"
  rolled_back
}

case_failure_through_the_public_entry_is_rolled_back() {
  FAKE_PUBLIC_FAIL=1 expect 1
  out_has 'public-check .*failed' || fail "失敗した工程が表示されていない"
  rolled_back
}

case_rollback_also_checks_the_public_entry() {
  FAKE_NEW_UNHEALTHY=1 expect 1
  # 戻した後に、直前の箱で、公開側の疎通まで確認している
  local last_up checks
  last_up="$(grep -nE 'up -d --no-deps api-go' "$FAKE_CALLS" | tail -1 | cut -d: -f1)"
  checks="$(tail -n +"$last_up" "$FAKE_CALLS" | grep -c 'curl http://base.test/health/ready -> 200')"
  [ "$checks" -ge 1 ] || fail "戻した後に公開側の疎通を確認していない"
}

case_rollback_failure_is_reported_loudly() {
  FAKE_NEW_UNHEALTHY=1 FAKE_ROLLBACK_START_FAIL=1 expect 4
  out_has '直前の箱へ戻すことにも失敗' || fail "戻せなかったことが表示されていない"
}

case_rollback_that_never_becomes_healthy_is_reported() {
  FAKE_ALL_UNHEALTHY=1 expect 4
  out_has '直前の箱へ戻すことにも失敗' || fail "戻せなかったことが表示されていない"
}

case_failed_deploy_never_rolls_back_the_database() {
  FAKE_NEW_UNHEALTHY=1 expect 1
  expect_lacks 'pg_restore|dropdb|DROP |volume |goose.* down|migrate.* down'
  # 移行は1回だけ（戻すときに、移行を戻す操作はしない）
  [ "$(grep -c 'run --rm migrate-go' "$FAKE_CALLS")" -eq 1 ] || fail "移行が複数回実行された"
}

# ---- 入力の拒否 ----

case_wrong_manifest_sha_is_rejected() {
  expect 2 "$(printf 'c%.0s' $(seq 1 64))"
  out_has '一致しません' || fail "理由が表示されていない"
  expect_lacks 'docker'
}

case_bad_arguments_are_rejected() {
  set +e
  bash "$BOOT/ops/deploy_go.sh" "$FX_ART" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "引数が足りないのに拒否されない"
  bash "$BOOT/ops/deploy_go.sh" "$FX_ART" "短い" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "不正なSHA256が拒否されない"
  bash "$BOOT/ops/deploy_go.sh" "$FX_TMP/nothing" "$MANIFEST_SHA" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "存在しないdirectoryが拒否されない"
  MTP_ENV_FILE="$FX_TMP/nothing.env" bash "$BOOT/ops/deploy_go.sh" "$FX_ART" "$MANIFEST_SHA" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "設定ファイルが無いのに拒否されない"
  MTP_BACKUP_DIR="$FX_TMP/nothing" bash "$BOOT/ops/deploy_go.sh" "$FX_ART" "$MANIFEST_SHA" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "バックアップのscriptが無いのに拒否されない"
  set -e
  expect_lacks 'docker'
}

case_script_from_a_different_archive_is_rejected() {
  # 展開した場所のscriptを書き換えると、照合したarchiveと違うので拒否する
  echo '# 改ざん' >>"$BOOT/ops/deploy_release.sh"
  expect 2
  out_has '中身と違います' || fail "理由が表示されていない"
  expect_lacks 'docker'
}

case_concurrent_deploy_is_rejected() {
  if command -v flock >/dev/null 2>&1; then
    # 別のプロセスがロックを持っている状態を作る
    (exec 9>"$FX_ROOT/.deploy.lock"; flock 9; sleep 20) &
    local holder=$!
    sleep 1
    expect 3
    kill "$holder" 2>/dev/null || true
  else
    mkdir "$FX_ROOT/.deploy.lock.d"
    expect 3
  fi
  out_has '別のデプロイが実行中' || fail "理由が表示されていない"
  expect_lacks 'docker'
}

case_lock_is_released_after_a_run() {
  expect 0
  # 続けて、もう一度実行できる（ロックが残っていない）
  [ ! -e "$FX_ROOT/.deploy.lock.d" ] || fail "ロックのdirectoryが残っている"
}

# ---- 静的な確認 ----

case_script_never_deletes_data_or_other_services() {
  local script="$repo_root/ops/deploy_go.sh"
  if grep -nE 'rm -rf|rm -r |volume (rm|prune)|system prune|image prune|dropdb|DROP |pg_restore|compose.* (down|stop|rm|restart) ' "$script" |
    grep -vE '^[0-9]+:\s*#' | grep -vF 'rm -rf -- "$work"'; then
    fail "削除・サービスの停止につながる操作がある"
  fi
  if grep -nE 'set -x|xtrace' "$script" | grep -v 'set +x'; then
    fail "shell traceを有効にしている"
  fi
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
  # 特定の1項目だけを実行したいとき: ONLY=case_名 bash ops/deploy_go_test.sh
  [ -z "${ONLY:-}" ] || [ "$case_name" = "$ONLY" ] || continue
  run_case "$case_name" "$case_name"
done

printf 'OK: %s deploy cases passed\n' "$passed"
