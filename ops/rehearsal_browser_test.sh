#!/usr/bin/env bash
# ops/rehearsal_browser.sh を、偽のdockerとcurlで確かめる。本物のdocker・ネットワークは使わない。
# 使い方: bash ops/rehearsal_browser_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/ops/rehearsal_browser.sh"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:$(printf 'a%.0s' $(seq 1 64))"
DB_NAME="mtp_rehearsal_20261006025030_df819f"

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

setup() {
  TMP="$(mktemp -d)"
  RELEASE="$TMP/release"
  mkdir -p "$RELEASE/frontend-site" "$TMP/bin"
  echo 'services: {}' >"$RELEASE/docker-compose.rehearsal.yml"
  printf 'MTP_RELEASE_DIR=%s\nGO_API_IMAGE=%s\n' "$RELEASE" "$IMAGE" >"$RELEASE/release.env"
  export FAKE_CALLS="$TMP/calls.log"
  : >"$FAKE_CALLS"

  cat >"$TMP/bin/docker" <<'FAKE'
#!/usr/bin/env bash
printf 'docker [env=%s swagger=%s site=%s origin=%s db=%s image=%s] %s\n' "${MTP_REHEARSAL_APP_ENV:-}" \
  "${MTP_REHEARSAL_SWAGGER_ENABLED:-}" "${MTP_REHEARSAL_SITE:-}" "${MTP_REHEARSAL_ORIGIN:-}" \
  "${MTP_REHEARSAL_DB_NAME:-}" "${MTP_REHEARSAL_IMAGE:-}" "$*" >>"$FAKE_CALLS"
case "$*" in
  *pg_database*) [ "${FAKE_NO_DB:-}" = 1 ] || echo 1 ;;
esac
exit 0
FAKE
  cat >"$TMP/bin/curl" <<'FAKE'
#!/usr/bin/env bash
echo "curl $*" >>"$FAKE_CALLS"
case "$*" in
  *'%{http_code}'*) printf '%s' "${FAKE_DOCS_STATUS:-200}"; exit 0 ;;
esac
if [ -n "${FAKE_CURL_FAIL:-}" ] && [[ "$*" == *"$FAKE_CURL_FAIL"* ]]; then
  exit 22
fi
exit 0
FAKE
  chmod +x "$TMP/bin/docker" "$TMP/bin/curl"
  export PATH="$TMP/bin:$PATH"
  export MTP_REHEARSAL_RELEASE_DIR="$RELEASE" MTP_REHEARSAL_DB_NAME="$DB_NAME"
  export MTP_REHEARSAL_DB_PASSWORD='synthetic-rehearsal-only' MTP_REHEARSAL_WAIT_SECONDS=1
  unset FAKE_NO_DB FAKE_DOCS_STATUS FAKE_CURL_FAIL
}

run() {
  set +e
  bash "$script" "$@" >"$TMP/out" 2>"$TMP/err"
  STATUS=$?
  set -e
}

expect_status() {
  [ "$STATUS" -eq "$1" ] || {
    cat "$TMP/out" "$TMP/err" "$FAKE_CALLS" >&2
    fail "終了コード $STATUS（$1 を期待）"
  }
}
has() { grep -qE -- "$1" "$FAKE_CALLS"; }
expect_has() { has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼び出しが無い: $1"; }; }
expect_lacks() { ! has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼ばれてはいけない: $1"; }; }

case_up_test_mode() {
  export FAKE_DOCS_STATUS=200
  run up test
  expect_status 0
  grep -q 'up (test) ok' "$TMP/out" || fail "成功の行が無い"
  grep -q 'swagger 200' "$TMP/out" || fail "Swaggerの結果が無い"
  expect_has "env=test swagger=true site=api.mytechpulse.net origin=https://api.mytechpulse.net db=$DB_NAME image=${IMAGE//\//\\/}"
  # DBを確認してからAPIとCaddyを起動する
  local db_line serve_line
  db_line="$(grep -nE 'up -d --wait db' "$FAKE_CALLS" | head -1 | cut -d: -f1)"
  serve_line="$(grep -nE 'serve up -d --wait api caddy' "$FAKE_CALLS" | head -1 | cut -d: -f1)"
  [ -n "$db_line" ] && [ -n "$serve_line" ] && [ "$db_line" -lt "$serve_line" ] || fail "起動の順序が違う"
  # 本番と同じホスト名で、隔離Caddy（18443）へ届くこと
  expect_has 'curl .*--resolve api.mytechpulse.net:18443:127.0.0.1 https://api.mytechpulse.net:18443/health/ready'
  expect_lacks ' down( |$)'
}

case_up_production_mode() {
  export FAKE_DOCS_STATUS=404
  run up production
  expect_status 0
  grep -q 'swagger 404' "$TMP/out" || fail "Swaggerが404の結果が無い"
  expect_has "env=production swagger=false site=api.mytechpulse.net"
}

case_production_with_visible_swagger_fails() {
  export FAKE_DOCS_STATUS=200
  run up production
  expect_status 1
  grep -q 'failed' "$TMP/err" || fail "失敗の行が無い"
}

case_test_without_swagger_fails() {
  export FAKE_DOCS_STATUS=404
  run up test
  expect_status 1
}

case_missing_database_fails_before_serving() {
  export FAKE_NO_DB=1
  run up test
  expect_status 1
  expect_lacks 'serve up'
}

case_unhealthy_fails() {
  export FAKE_CURL_FAIL=health/ready
  run up test
  expect_status 1
}

case_invalid_inputs_are_rejected_before_docker() {
  run
  expect_status 2
  run up
  expect_status 2
  run up staging
  expect_status 2
  run up test extra
  expect_status 2
  MTP_REHEARSAL_DB_NAME='bad name;drop' run up test
  expect_status 2
  MTP_REHEARSAL_DB_NAME='' run up test
  expect_status 2
  MTP_REHEARSAL_DB_PASSWORD='' run up test
  expect_status 2
  MTP_REHEARSAL_RELEASE_DIR="$TMP/none" run up test
  expect_status 2
  # tagやdigestでないimageは受け付けない
  printf 'GO_API_IMAGE=ghcr.io/h4aruki/mytechpulse-api-go:latest\n' >"$RELEASE/release.env"
  run up test
  expect_status 2
  rm -f -- "$RELEASE/release.env"
  run up test
  expect_status 2
  [ ! -s "$FAKE_CALLS" ] || { cat "$FAKE_CALLS" >&2; fail "拒否したのにdockerを呼んだ"; }
}

case_stop_only_stops() {
  run stop
  expect_status 0
  expect_has 'stop$'
  expect_lacks ' down( |$)'
  expect_lacks 'volume '
}

case_script_never_deletes_data() {
  if grep -nE 'down|volume rm|prune|dropdb|rm -rf' "$script" | grep -vE '^[0-9]+:\s*#'; then
    fail "削除につながる操作がある"
  fi
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
  run_case "$case_name" "$case_name"
done

printf 'OK: %s rehearsal browser cases passed\n' "$passed"
