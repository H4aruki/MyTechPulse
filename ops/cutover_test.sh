#!/usr/bin/env bash
# ops/cutover.sh を、偽のdocker・curlと偽の運用scriptで確かめる（#127）。
# 本物のdocker・本番・ネットワークには触れない。設定ファイルの値はすべて合成のダミー。
# 使い方: bash ops/cutover_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

run_case() {
  local name="$1" status
  shift
  set +e
  (
    set -e
    setup
    trap 'rm -rf "$TMP"' EXIT
    "$@"
  )
  status=$?
  set -e
  [ "$status" -eq 0 ] || fail "$name"
  passed=$((passed + 1))
  printf 'ok: %s\n' "$name"
}

SECRET_PASSWORD='S3cretDbPassword-synthetic-0001'
SECRET_TOKEN='QiitaTokenSynthetic-0002'
IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:$(printf 'a%.0s' $(seq 1 64))"

setup() {
  TMP="$(mktemp -d)"
  RELEASE="$TMP/release"
  LEGACY="$TMP/legacy"
  mkdir -p "$RELEASE/ops/lib" "$RELEASE/ops/caddy" "$RELEASE/ops/sql" "$LEGACY/ops" "$TMP/bin" "$TMP/fake"

  # release directory（deploy_release.sh が準備したものに相当）
  printf 'MTP_RELEASE_DIR=%s\nGO_API_IMAGE=%s\n' "$RELEASE" "$IMAGE" >"$RELEASE/release.env"
  printf 'services:\n  caddy:\n    volumes:\n      - ./${MTP_CADDYFILE:-Caddyfile}:/x\n  migrate-go: {}\n' >"$RELEASE/docker-compose.yml"
  cp "$repo_root/ops/cutover.sh" "$RELEASE/ops/cutover.sh"
  cp "$repo_root/ops/lib/previous_release_record.sh" "$RELEASE/ops/lib/previous_release_record.sh"
  : >"$RELEASE/ops/caddy/Caddyfile.maintenance"
  : >"$RELEASE/ops/caddy/Caddyfile.go"
  : >"$RELEASE/ops/sql/rehearsal_cleanup.sql"
  write_bundle_fakes

  # 切り戻し先（旧版の運用ファイル）
  echo 'services: {}' >"$LEGACY/docker-compose.yml"
  : >"$LEGACY/Caddyfile"
  cat >"$LEGACY/ops/backup_db.sh" <<'FAKE'
#!/usr/bin/env bash
echo "legacy-backup cwd=${PWD##*/} project=${COMPOSE_PROJECT_NAME:-}" >>"$FAKE_CALLS"
[ "${FAKE_BACKUP_FAIL:-}" != 1 ] || exit 1
echo "backups/mytechpulse_20261007T000000Z.dump"
FAKE
  cat >"$LEGACY/ops/verify_backup.sh" <<'FAKE'
#!/usr/bin/env bash
echo "legacy-verify $1" >>"$FAKE_CALLS"
[ "${FAKE_VERIFY_FAIL:-}" != 1 ] || exit 1
FAKE
  cat >"$RELEASE/previous-release.json" <<JSON
{
  "manifest_sha256": "legacy-python-no-manifest",
  "api_image": "mytechpulse-api",
  "frontend_deployment_id": "deploy-0001",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "legacy-no-hash",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "legacy-no-hash",
  "ops_release_dir": "${LEGACY}"
}
JSON

  # 本番の設定ファイル（合成）
  ENVF="$TMP/production.env"
  write_env_file
  chmod 600 "$ENVF"

  setup_fakes
  export FAKE_CALLS="$TMP/calls.log" FAKE_STATE="$TMP/fake"
  : >"$FAKE_CALLS"
  printf 'db api caddy' >"$FAKE_STATE/services"
  printf 'python' >"$FAKE_STATE/caddy_config"
  printf '5,3,12' >"$FAKE_STATE/counts"
  : >"$TMP/all.log"

  export MTP_RELEASE_DIR="$RELEASE" MTP_ENV_FILE="$ENVF"
  export MTP_CUTOVER_BASE_URL=http://base.test MTP_CUTOVER_GO_URL=http://go.test MTP_CUTOVER_PY_URL=http://py.test
  export MTP_CUTOVER_WAIT_SECONDS=1 MTP_CUTOVER_ORIGIN=https://front.test
  unset MTP_CUTOVER_CONFIRM_FRONTEND MTP_COMPOSE_PROJECT MTP_COMPOSE_EXTRA_FILES
  unset FAKE_BACKUP_FAIL FAKE_VERIFY_FAIL FAKE_FAIL FAKE_NO_IMAGE FAKE_DBPASS_FAIL FAKE_CADDY_STUCK FAKE_COMPARE_EXIT \
    FAKE_SNAPSHOT_FAIL FAKE_SMOKE_FAIL FAKE_LEFTOVER FAKE_COUNTS_SECOND FAKE_GO_UNHEALTHY FAKE_PY_FAIL FAKE_PUBLIC_FAIL FAKE_CONFIG_FAIL
}

write_env_file() {
  cat >"$ENVF" <<EOF
POSTGRES_PASSWORD=${SECRET_PASSWORD}
API_DOMAIN=api.example.test
APP_ENV=production
QIITA_ACCESS_TOKEN=${SECRET_TOKEN}
CORS_ALLOWED_ORIGINS=https://front.test,https://www.front.test
SWAGGER_ENABLED=false
EOF
}

write_bundle_fakes() {
  cat >"$RELEASE/ops/snapshot_migration_state.sh" <<'FAKE'
#!/usr/bin/env bash
name="$(basename "$MTP_SNAPSHOT_OUTPUT" .json)"
echo "snapshot out=$name db=$MTP_SNAPSHOT_DB args=$MTP_SNAPSHOT_COMPOSE_ARGS" >>"$FAKE_CALLS"
[ "${FAKE_SNAPSHOT_FAIL:-}" != "$name" ] || { echo 'snapshot: failed (E_DB)' >&2; exit 1; }
[ -e "$MTP_SNAPSHOT_NONCE_FILE" ] || printf 'fake-nonce-value' >"$MTP_SNAPSHOT_NONCE_FILE"
printf '{}\n' >"$MTP_SNAPSHOT_OUTPUT"
echo 'snapshot: ok'
FAKE
  cat >"$RELEASE/ops/compare_migration_state.sh" <<'FAKE'
#!/usr/bin/env bash
echo "compare $(basename "$1") $(basename "$2")" >>"$FAKE_CALLS"
status="${FAKE_COMPARE_EXIT:-0}"
case "$status" in
  0) echo '{"matches":true,"mismatched_tables":0}' ;;
  1) echo '{"matches":false,"mismatched_tables":1}' ;;
esac
exit "$status"
FAKE
  cat >"$RELEASE/ops/rehearsal_smoke.sh" <<'FAKE'
#!/usr/bin/env bash
echo "smoke base=$MTP_REHEARSAL_BASE_URL origin=$MTP_REHEARSAL_ORIGIN user=$MTP_REHEARSAL_SMOKE_USERNAME" >>"$FAKE_CALLS"
if [ "${FAKE_SMOKE_FAIL:-}" = 1 ]; then
  echo 'smoke: failed (me: 状態番号 500（200 を期待）)' >&2
  exit 1
fi
echo 'smoke: ok'
FAKE
}

setup_fakes() {
  cat >"$TMP/bin/docker" <<'FAKE'
#!/usr/bin/env bash
args="$*"
printf 'docker [cwd=%s caddyfile=%s image=%s project=%s] %s\n' "${PWD##*/}" "${MTP_CADDYFILE:-}" "${GO_API_IMAGE:-}" "${COMPOSE_PROJECT_NAME:-}" "$args" >>"$FAKE_CALLS"
services() { cat "$FAKE_STATE/services"; }
add_service() { printf '%s %s' "$(services)" "$1" >"$FAKE_STATE/services"; }
remove_service() {
  # 同じファイルを読みながら書かないよう、先に変数へ受ける
  local remaining
  remaining="$(services | tr ' ' '\n' | grep -vx "$1" | tr '\n' ' ' || true)"
  printf '%s' "$remaining" >"$FAKE_STATE/services"
}
if [ -n "${FAKE_FAIL:-}" ] && [[ "$args" == *"$FAKE_FAIL"* ]]; then
  exit 1
fi
case "$args" in
  "image inspect"*) [ "${FAKE_NO_IMAGE:-}" != 1 ] || exit 1 ;;
  "inspect cid-caddy"*)
    case "$(cat "$FAKE_STATE/caddy_config")" in
      maintenance) echo '/srv/release/ops/caddy/Caddyfile.maintenance' ;;
      go) echo '/srv/release/ops/caddy/Caddyfile.go' ;;
      python) echo '/srv/legacy/Caddyfile' ;;
    esac ;;
  *"ps -q caddy"*) echo cid-caddy ;;
  *"ps --status running --services"*) services | tr ' ' '\n' | grep -v '^$' ;;
  *" config"*) [ "${FAKE_CONFIG_FAIL:-}" != 1 ] || exit 1 ;;
  *"up -d --no-deps caddy"*)
    if [ "${FAKE_CADDY_STUCK:-}" != 1 ]; then
      case "${MTP_CADDYFILE:-}" in
        ops/caddy/Caddyfile.maintenance) printf 'maintenance' >"$FAKE_STATE/caddy_config" ;;
        ops/caddy/Caddyfile.go) printf 'go' >"$FAKE_STATE/caddy_config" ;;
        *) printf 'python' >"$FAKE_STATE/caddy_config" ;;
      esac
    fi ;;
  *"up -d --no-deps api-go"*) add_service api-go ;;
  *"up -d --no-build --no-deps api"*) add_service api ;;
  *" stop api-go"*) remove_service api-go ;;
  *" stop api"*) remove_service api ;;
  *"run --rm migrate-go"*) ;;
  *"exec -T -e POSTGRES_PASSWORD db sh -c"*) [ "${FAKE_DBPASS_FAIL:-}" != 1 ] || exit 1 ;;
  *'max("tag_ID")'*) echo 7 ;;
  *"SELECT (SELECT count(*)"*)
    count_file="$FAKE_STATE/counts_calls"
    n="$(cat "$count_file" 2>/dev/null || echo 0)"
    echo $((n + 1)) >"$count_file"
    if [ "$n" -ge 1 ] && [ -n "${FAKE_COUNTS_SECOND:-}" ]; then echo "$FAKE_COUNTS_SECOND"; else cat "$FAKE_STATE/counts"; fi ;;
  *"WHERE user_name ="*) echo "${FAKE_LEFTOVER:-0}" ;;
  *"smoke_user="*) ;;
esac
exit 0
FAKE
  cat >"$TMP/bin/curl" <<'FAKE'
#!/usr/bin/env bash
url="${*: -1}"
code=000
case "$url" in
  http://base.test/*)
    case "$(cat "$FAKE_STATE/caddy_config")" in
      maintenance) code=503 ;;
      go) code=200; [ "${FAKE_PUBLIC_FAIL:-}" != 1 ] || code=502 ;;
      python) code=200 ;;
    esac ;;
  http://go.test/*)
    if grep -qw api-go "$FAKE_STATE/services"; then code=200; [ "${FAKE_GO_UNHEALTHY:-}" != 1 ] || code=503; fi ;;
  http://py.test/*)
    if grep -qw api "$FAKE_STATE/services"; then code=200; [ "${FAKE_PY_FAIL:-}" != 1 ] || code=503; fi ;;
esac
echo "curl $url -> $code" >>"$FAKE_CALLS"
printf '%s' "$code"
exit 0
FAKE
  chmod +x "$TMP/bin/docker" "$TMP/bin/curl"
  export PATH="$TMP/bin:$PATH"
}

# 段階を実行し、終了コードと出力を控える
st() {
  set +e
  bash "$RELEASE/ops/cutover.sh" "$1" >"$TMP/out" 2>"$TMP/err"
  STATUS=$?
  set -e
  cat "$TMP/out" "$TMP/err" >>"$TMP/all.log"
}

expect() {
  # expect 終了コード 段階
  st "$2"
  [ "$STATUS" -eq "$1" ] || {
    cat "$TMP/out" "$TMP/err" "$FAKE_CALLS" >&2
    fail "$2: 終了コード $STATUS（$1 を期待）"
  }
}
has() { grep -qE -- "$1" "$FAKE_CALLS"; }
expect_has() { has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼び出しが無い: $1"; }; }
expect_lacks() { ! has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼ばれてはいけない: $1"; }; }
out_has() { grep -qE -- "$1" "$TMP/out" "$TMP/err"; }
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
caddy_now() { cat "$FAKE_STATE/caddy_config"; }

# ここまでの段階を成功させる
advance_to() {
  local target="$1" s
  for s in preflight maintenance-on backup snapshot-before migrate compare go-start; do
    expect 0 "$s"
    [ "$s" != "$target" ] || return 0
  done
  export MTP_CUTOVER_CONFIRM_FRONTEND=yes
  for s in switch smoke smoke-cleanup; do
    expect 0 "$s"
    [ "$s" != "$target" ] || return 0
  done
}

# ---- 全段階 ----

case_full_cutover() {
  advance_to smoke-cleanup
  expect 0 finish
  out_has '^cutover: ok' || fail "完了の表示が無い"
  expect_order 'image inspect' 'caddyfile=ops/caddy/Caddyfile.maintenance .* up -d --no-deps caddy' ' stop api' \
    'legacy-backup cwd=legacy project=mytechpulse' 'legacy-verify backups/' \
    'snapshot out=before' 'run --rm migrate-go' 'snapshot out=after ' 'compare before.json after.json' \
    'up -d --no-deps api-go' 'caddyfile=ops/caddy/Caddyfile.go .* up -d --no-deps caddy' \
    'smoke base=http://base.test origin=https://front.test user=rehearsal-smoke-' 'smoke_user=rehearsal-smoke-'
  [ "$(caddy_now)" = go ] || fail "最後のCaddyの向け先がGo版でない"
  # 今のmaintenance・go切り替えは、リリースのcomposeを使い、他のサービスは起動していない
  expect_lacks 'up -d .*--no-deps .* db'
  # 設定ファイルは --env-file で渡し、中身は出さない
  expect_has '--env-file .*production.env'
  # 合成利用者名と片付け
  local smoke_user cleanup_user
  smoke_user="$(grep -Eo 'smoke base=.* user=rehearsal-smoke-[0-9a-f]{12}' "$FAKE_CALLS" | sed 's/.*user=//')"
  cleanup_user="$(grep -Eo 'smoke_user=rehearsal-smoke-[0-9a-f]{12}' "$FAKE_CALLS" | sed 's/smoke_user=//')"
  [ -n "$smoke_user" ] && [ "$smoke_user" = "$cleanup_user" ] || fail "smokeと片付けの利用者名が違う"
  expect_has 'tag_max=7'
  # 記録用の一時ファイルは消え、バックアップの名前は残る
  [ ! -e "$RELEASE/cutover-state/nonce" ] && [ ! -e "$RELEASE/cutover-state/before.json" ] || fail "finishの後にnonce・snapshotが残っている"
  out_has 'backup file kept: mytechpulse_20261007T000000Z.dump' || fail "バックアップの名前が出ていない"
  # 停止時間が出る
  out_has '^cutover: stop-time [0-9]+s' || cat "$TMP/all.log" | grep -q 'stop-time' || fail "停止時間が出ていない"
}

case_output_never_contains_secrets() {
  advance_to smoke-cleanup
  expect 0 finish
  local value
  for value in "$SECRET_PASSWORD" "$SECRET_TOKEN" 'fake-nonce-value'; do
    if grep -qF -- "$value" "$TMP/all.log" "$FAKE_CALLS"; then
      fail "出力か呼び出しに機密値が出ている: $value"
    fi
  done
}

case_each_stage_prints_name_and_seconds() {
  expect 0 preflight
  grep -qE '^cutover: preflight [0-9]+s ok$' "$TMP/out" || fail "preflightの行が決まった形でない"
  expect 0 maintenance-on
  grep -qE '^cutover: maintenance-on [0-9]+s ok$' "$TMP/out" || fail "maintenance-onの行が決まった形でない"
}

# ---- 段階の順番 ----

case_stages_cannot_skip_ahead() {
  local s
  for s in maintenance-on backup snapshot-before migrate compare go-start switch smoke finish; do
    expect 2 "$s"
  done
  expect_lacks 'run --rm migrate-go'
  expect_lacks 'up -d --no-deps'
  expect_lacks ' stop '
}

case_migrate_needs_backup_and_snapshot() {
  expect 0 preflight
  expect 0 maintenance-on
  expect 2 migrate
  expect 2 snapshot-before
  expect 0 backup
  expect 2 migrate
  expect_lacks 'run --rm migrate-go'
}

case_completed_stage_cannot_repeat() {
  expect 0 preflight
  expect 0 maintenance-on
  expect 2 maintenance-on
  expect 0 backup
  expect 2 backup
}

case_switch_needs_frontend_confirmation() {
  advance_to go-start
  expect 2 switch
  [ "$(caddy_now)" = maintenance ] || fail "確認が無いのにCaddyが切り替わった"
  MTP_CUTOVER_CONFIRM_FRONTEND=no expect 2 switch
  [ "$(caddy_now)" = maintenance ] || fail "yes以外でCaddyが切り替わった"
  MTP_CUTOVER_CONFIRM_FRONTEND=yes expect 0 switch
  [ "$(caddy_now)" = go ] || fail "Go版へ切り替わっていない"
}

case_must_run_from_release_bundle() {
  set +e
  MTP_RELEASE_DIR="$RELEASE" bash "$repo_root/ops/cutover.sh" preflight >"$TMP/out" 2>"$TMP/err"
  STATUS=$?
  set -e
  [ "$STATUS" -eq 2 ] || fail "checkoutのscriptを実行できてしまう"
  expect_lacks 'docker'
}

case_invalid_inputs_are_rejected() {
  set +e
  bash "$RELEASE/ops/cutover.sh" >"$TMP/out" 2>"$TMP/err"; [ $? -eq 2 ] || fail "段階なしを拒否しない"
  bash "$RELEASE/ops/cutover.sh" unknown >"$TMP/out" 2>"$TMP/err"; [ $? -eq 2 ] || fail "不明な段階を拒否しない"
  MTP_ENV_FILE="$TMP/none.env" bash "$RELEASE/ops/cutover.sh" preflight >"$TMP/out" 2>"$TMP/err"; [ $? -eq 2 ] || fail "設定ファイルなしを拒否しない"
  MTP_RELEASE_DIR="$TMP/none" bash "$RELEASE/ops/cutover.sh" preflight >"$TMP/out" 2>"$TMP/err"; [ $? -eq 2 ] || fail "release directoryなしを拒否しない"
  set -e
  [ ! -s "$FAKE_CALLS" ] || fail "拒否したのにdockerを呼んだ"
}

# ---- preflight ----

preflight_fails() {
  # preflight_fails 理由の一部
  expect 1 preflight
  grep -q "$1" "$TMP/err" || { cat "$TMP/err" >&2; fail "失敗の理由が違う（$1 を期待）"; }
  expect_lacks 'up -d'
  expect_lacks ' stop '
  [ ! -e "$RELEASE/cutover-state/done.preflight" ] || fail "失敗したのに完了の記録がある"
}

case_preflight_passes_and_changes_nothing() {
  expect 0 preflight
  expect_lacks 'up -d'
  expect_lacks ' stop '
  expect_lacks 'run --rm'
  [ "$(caddy_now)" = python ] || fail "preflightでCaddyが変わった"
}

case_preflight_missing_env_keys() {
  local key
  for key in POSTGRES_PASSWORD API_DOMAIN QIITA_ACCESS_TOKEN CORS_ALLOWED_ORIGINS; do
    write_env_file
    sed -i "/^${key}=/d" "$ENVF"
    preflight_fails "$key"
  done
  write_env_file
  sed -i 's/^QIITA_ACCESS_TOKEN=.*/QIITA_ACCESS_TOKEN=/' "$ENVF"
  preflight_fails QIITA_ACCESS_TOKEN
}

case_preflight_requires_production_settings() {
  write_env_file
  sed -i 's/^APP_ENV=.*/APP_ENV=local/' "$ENVF"
  preflight_fails APP_ENV
  write_env_file
  sed -i 's/^SWAGGER_ENABLED=.*/SWAGGER_ENABLED=true/' "$ENVF"
  preflight_fails SWAGGER_ENABLED
}

case_preflight_requires_private_env_file() {
  local probe mode
  probe="$TMP/mode-probe"
  : >"$probe"
  chmod 600 "$probe"
  mode="$(stat -c '%a' "$probe")"
  if [ "$mode" != 600 ]; then
    printf '（この環境ではファイルの権限を確かめられないため省略）\n'
    return 0
  fi
  chmod 644 "$ENVF"
  preflight_fails '権限'
}

case_preflight_checks_runtime_state() {
  FAKE_NO_IMAGE=1 preflight_fails '箱'
  unset FAKE_NO_IMAGE
  FAKE_DBPASS_FAIL=1 preflight_fails 'DBパスワード'
  unset FAKE_DBPASS_FAIL
  FAKE_CONFIG_FAIL=1 preflight_fails 'compose'
  unset FAKE_CONFIG_FAIL
  printf 'db api caddy api-go' >"$FAKE_STATE/services"
  preflight_fails '既に動いている'
  printf 'db caddy' >"$FAKE_STATE/services"
  preflight_fails 'api が動いていない'
  printf 'db api caddy' >"$FAKE_STATE/services"
  printf 'go' >"$FAKE_STATE/caddy_config"
  preflight_fails 'Python版でない'
}

case_preflight_checks_files() {
  rm -f -- "$RELEASE/ops/caddy/Caddyfile.go"
  preflight_fails 'Caddyfile.go'
  : >"$RELEASE/ops/caddy/Caddyfile.go"
  rm -f -- "$LEGACY/ops/backup_db.sh"
  preflight_fails 'backup_db.sh'
}

# ---- 各段階の失敗 ----

case_maintenance_failure_does_not_stop_python() {
  expect 0 preflight
  FAKE_CADDY_STUCK=1 expect 1 maintenance-on
  expect_lacks ' stop api'
  [ ! -e "$RELEASE/cutover-state/done.maintenance-on" ] || fail "失敗したのに完了の記録がある"
}

case_maintenance_blocks_before_stopping_python() {
  expect 0 preflight
  expect 0 maintenance-on
  [ "$(caddy_now)" = maintenance ] || fail "メンテナンスになっていない"
  grep -qw api "$FAKE_STATE/services" && fail "Python版が止まっていない"
  expect_order 'caddyfile=ops/caddy/Caddyfile.maintenance' ' stop api'
}

case_backup_failures_stop() {
  advance_to maintenance-on
  FAKE_BACKUP_FAIL=1 expect 1 backup
  FAKE_VERIFY_FAIL=1 expect 1 backup
  expect_lacks 'snapshot out='
  expect 2 snapshot-before
}

case_snapshot_failure_stops_before_migration() {
  advance_to backup
  FAKE_SNAPSHOT_FAIL=before expect 1 snapshot-before
  expect 2 migrate
  expect_lacks 'run --rm migrate-go'
}

case_migration_failure_stops() {
  advance_to snapshot-before
  FAKE_FAIL='run --rm migrate-go' expect 1 migrate
  expect 2 compare
  expect_lacks 'out=after'
}

case_compare_mismatch_stops_before_go() {
  advance_to migrate
  FAKE_COMPARE_EXIT=1 expect 1 compare
  out_has 'rollback' || fail "rollbackを促す表示が無い"
  expect 2 go-start
  expect_lacks 'up -d --no-deps api-go'
}

case_compare_unable_stops() {
  advance_to migrate
  FAKE_COMPARE_EXIT=2 expect 1 compare
  expect_lacks 'up -d --no-deps api-go'
}

case_count_difference_stops() {
  advance_to migrate
  FAKE_COUNTS_SECOND='5,3,13' expect 1 compare
  expect 2 go-start
}

case_go_unhealthy_stops_before_switch() {
  advance_to compare
  FAKE_GO_UNHEALTHY=1 expect 1 go-start
  export MTP_CUTOVER_CONFIRM_FRONTEND=yes
  expect 2 switch
  [ "$(caddy_now)" = maintenance ] || fail "Go版が不健全なのにCaddyが切り替わった"
}

case_switch_failure_is_reported() {
  advance_to go-start
  export MTP_CUTOVER_CONFIRM_FRONTEND=yes
  FAKE_PUBLIC_FAIL=1 expect 1 switch
  out_has 'rollback' || fail "rollbackを促す表示が無い"
}

case_smoke_requires_origin_in_cors_list() {
  advance_to switch
  MTP_CUTOVER_ORIGIN='' expect 2 smoke
  MTP_CUTOVER_ORIGIN=https://other.test expect 2 smoke
  MTP_CUTOVER_ORIGIN=http://front.test expect 2 smoke
  expect_lacks 'smoke base='
}

case_smoke_failure_allows_cleanup_and_blocks_finish() {
  advance_to switch
  FAKE_SMOKE_FAIL=1 expect 1 smoke
  expect 2 finish
  expect 0 smoke-cleanup
  expect 2 finish
}

case_cleanup_requires_a_smoke_attempt() {
  advance_to switch
  expect 2 smoke-cleanup
  expect_lacks 'smoke_user='
}

case_cleanup_failure_is_reported() {
  advance_to smoke
  FAKE_LEFTOVER=1 expect 1 smoke-cleanup
  expect 2 finish
}

# ---- 切り戻し ----

rollback_order() {
  expect_order 'caddyfile=ops/caddy/Caddyfile.maintenance' ' stop api-go' \
    'cwd=legacy .* up -d --no-build --no-deps api' 'curl http://py.test/' 'cwd=legacy .* up -d --no-deps caddy'
}

case_rollback_after_maintenance() {
  advance_to maintenance-on
  expect 0 rollback
  rollback_order
  [ "$(caddy_now)" = python ] || fail "Python版へ戻っていない"
  grep -qw api "$FAKE_STATE/services" || fail "Python版APIが動いていない"
  out_has 'deploy-0001' || fail "画面を戻すための識別子が表示されていない"
  out_has 'Cloudflare Pages' || fail "画面を戻す案内が無い"
  # 状態は消さず、名前を変えて残す。次の試行は新しい状態で始められる
  ls -d "$RELEASE"/cutover-state.rolledback-* >/dev/null 2>&1 || fail "状態が残っていない"
  [ ! -e "$RELEASE/cutover-state/done.maintenance-on" ] || fail "切り戻し後も完了の記録が残っている"
}

case_rollback_after_switch() {
  advance_to smoke
  expect 0 rollback
  [ "$(caddy_now)" = python ] || fail "Python版へ戻っていない"
  grep -qw api-go "$FAKE_STATE/services" && fail "Go版が止まっていない"
  # 切り戻しの後は、最初の段階からやり直せる
  expect 0 preflight
}

case_rollback_works_without_any_stage() {
  expect 0 rollback
  [ "$(caddy_now)" = python ] || fail "Python版へ戻っていない"
}

case_rollback_reports_partial_failure_and_continues() {
  advance_to switch
  FAKE_PY_FAIL=1 expect 1 rollback
  out_has 'python-health failed' || fail "失敗した工程が表示されていない"
  # 失敗した後も、残りの工程は続ける（Caddyを戻す）
  expect_has 'cwd=legacy .* up -d --no-deps caddy'
}

case_rollback_never_touches_the_database() {
  advance_to switch
  expect 0 rollback
  expect_lacks 'pg_restore|dropdb|DROP |down'
  expect_lacks 'volume '
}

# ---- 状態表示 ----

case_status_shows_state_without_changing() {
  expect 0 status
  grep -q 'caddy=python' "$TMP/out" || fail "Caddyの向け先が表示されていない"
  grep -q 'services=' "$TMP/out" || fail "動いているサービスが表示されていない"
  expect_lacks 'up -d'
  expect_lacks ' stop '
  advance_to maintenance-on
  expect 0 status
  grep -q 'caddy=maintenance' "$TMP/out" || fail "メンテナンスが表示されていない"
}

# ---- 静的な確認 ----

case_script_never_deletes_data() {
  if grep -nE 'docker compose.* down|compose.* down |volume (rm|prune)|system prune|image prune|dropdb|DROP DATABASE|pg_restore' \
    "$repo_root/ops/cutover.sh" | grep -vE '^[0-9]+:\s*#'; then
    fail "削除・復元につながる操作がある"
  fi
  if grep -nE 'rm -rf|rm -r ' "$repo_root/ops/cutover.sh" | grep -vE '^[0-9]+:\s*#'; then
    fail "再帰的な削除がある"
  fi
  if grep -nE 'set -x|xtrace' "$repo_root/ops/cutover.sh" | grep -v 'set +x'; then
    fail "shell traceを有効にしている"
  fi
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
  # 特定の1項目だけを実行したいとき: ONLY=case_名 bash ops/cutover_test.sh
  [ -z "${ONLY:-}" ] || [ "$case_name" = "$ONLY" ] || continue
  run_case "$case_name" "$case_name"
done

printf 'OK: %s cutover cases passed\n' "$passed"
