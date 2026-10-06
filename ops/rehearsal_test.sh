#!/usr/bin/env bash
# ops/rehearsal.sh を、偽のdocker・curl・gpgと偽の運用scriptで確かめる。
# 本物のdocker・ネットワーク・本番由来のdumpは使わない（dumpは合成のダミー）。
# 使い方: bash ops/rehearsal_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"
rehearsal="$repo_root/ops/rehearsal.sh"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

# 1件ごとに、新しい一時directoryと環境で実行する
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

# ---- 部品 ----

SYNTHETIC_DECRYPTED='SYNTHETIC-DECRYPTED-DUMP'

setup() {
  fx_init
  add_bundle_files
  setup_fakes
  setup_previous_record
  setup_dump gpg
  export RH_TMPDIR="$FX_TMP/rehearsal-tmp"
  mkdir -p "$RH_TMPDIR"
  export FAKE_CALLS="$FX_TMP/calls.log"
  export FAKE_STATE="$FX_TMP"
  : >"$FAKE_CALLS"
  export MTP_REHEARSAL_DB_PASSWORD='synthetic-rehearsal-only'
  export MTP_REHEARSAL_WAIT_SECONDS=1
  unset MTP_REHEARSAL_IMAGE MTP_REHEARSAL_SYNTHETIC FAKE_FAIL FAKE_CURL_FAIL FAKE_GPG_FAIL \
    FAKE_SNAPSHOT_FAIL FAKE_COMPARE_EXITS FAKE_SMOKE_FAIL FAKE_SMOKE_SLEEP MTP_REHEARSAL_WINDOW_LIMIT_SECONDS
}

# 検証済みの運用一式（opsのarchive）へ、rehearsalが使うscriptの偽物を入れて、archiveとmanifestを作り直す
add_bundle_files() {
  local src="$FX_TMP/src/ops"
  mkdir -p "$src/ops/sql"
  echo 'services: {}' >"$src/docker-compose.rehearsal.yml"
  echo '-- cleanup' >"$src/ops/sql/rehearsal_cleanup.sql"
  echo '-- verify' >"$src/ops/sql/verify_restored_db.sql"

  cat >"$src/ops/snapshot_migration_state.sh" <<'FAKE'
#!/usr/bin/env bash
name="$(basename "$MTP_SNAPSHOT_OUTPUT" .json)"
echo "snapshot script=${BASH_SOURCE[0]} out=$name db=$MTP_SNAPSHOT_DB" >>"$FAKE_CALLS"
if [ "${FAKE_SNAPSHOT_FAIL:-}" = "$name" ]; then
  echo 'snapshot: failed (E_DB)' >&2
  exit 1
fi
[ -e "$MTP_SNAPSHOT_NONCE_FILE" ] || printf 'fake-nonce' >"$MTP_SNAPSHOT_NONCE_FILE"
printf '{}\n' >"$MTP_SNAPSHOT_OUTPUT"
echo 'snapshot: ok'
FAKE

  cat >"$src/ops/compare_migration_state.sh" <<'FAKE'
#!/usr/bin/env bash
count_file="$FAKE_STATE/compare_count"
count="$(cat "$count_file" 2>/dev/null || echo 0)"
echo $((count + 1)) >"$count_file"
read -r -a exits <<<"${FAKE_COMPARE_EXITS:-0 1 0}"
status="${exits[$count]:-0}"
echo "compare script=${BASH_SOURCE[0]} $(basename "$1") $(basename "$2") ${3:-} status=$status" >>"$FAKE_CALLS"
if [ "$status" -eq 0 ]; then
  echo '{"matches":true,"mismatched_tables":0}'
elif [ "$status" -eq 1 ]; then
  echo '{"matches":false,"mismatched_tables":1}'
fi
exit "$status"
FAKE

  cat >"$src/ops/rehearsal_smoke.sh" <<'FAKE'
#!/usr/bin/env bash
echo "smoke script=${BASH_SOURCE[0]} user=${MTP_REHEARSAL_SMOKE_USERNAME:-}" >>"$FAKE_CALLS"
[ -z "${FAKE_SMOKE_SLEEP:-}" ] || sleep "$FAKE_SMOKE_SLEEP"
if [ "${FAKE_SMOKE_FAIL:-}" = 1 ]; then
  echo 'smoke: failed (me: 状態番号 500（200 を期待）)' >&2
  exit 1
fi
echo 'smoke: ok'
FAKE

  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src" docker-compose.yml Caddyfile ops docker-compose.rehearsal.yml
  fx_write_manifest
}

# 呼ばれたコマンドを記録し、環境変数で失敗させられる偽物をPATHの先頭へ置く
setup_fakes() {
  FX_BIN="$FX_TMP/bin"
  mkdir -p "$FX_BIN"

  cat >"$FX_BIN/docker" <<'FAKE'
#!/usr/bin/env bash
args="$*"
printf 'docker [image=%s legacy=%s db=%s] %s\n' "${MTP_REHEARSAL_IMAGE:-}" "${MTP_REHEARSAL_LEGACY_IMAGE:-}" \
  "${MTP_REHEARSAL_DB_NAME:-}" "$args" >>"$FAKE_CALLS"
# 標準入力を読むのは、dumpをファイルから流し込む呼び出しだけ（他は入力が閉じていないため読まない）
case "$args" in
  *'pg_restore -U'*)
    echo "restore-stdin=$(head -c 24)" >>"$FAKE_CALLS"
    ;;
esac
if [ -n "${FAKE_FAIL:-}" ] && [[ "$args" == *"$FAKE_FAIL"* ]]; then
  exit 1
fi
case "$args" in
  *'max("tag_ID")'*) echo 7 ;;
esac
exit 0
FAKE

  cat >"$FX_BIN/curl" <<'FAKE'
#!/usr/bin/env bash
echo "curl $*" >>"$FAKE_CALLS"
if [ -n "${FAKE_CURL_FAIL:-}" ] && [[ "$*" == *"$FAKE_CURL_FAIL"* ]]; then
  exit 22
fi
exit 0
FAKE

  cat >"$FX_BIN/gpg" <<'FAKE'
#!/usr/bin/env bash
echo "gpg $*" >>"$FAKE_CALLS"
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift ;;
  esac
  shift
done
[ "${FAKE_GPG_FAIL:-}" != 1 ] || exit 2
# 復号結果は100byte以上の合成の内容
for _ in 1 2 3 4 5 6 7 8; do printf 'SYNTHETIC-DECRYPTED-DUMP'; done >"$output"
exit 0
FAKE
  chmod +x "$FX_BIN/docker" "$FX_BIN/curl" "$FX_BIN/gpg"
  export PATH="$FX_BIN:$PATH"
}

setup_previous_record() {
  FX_OLD_DIR="$FX_TMP/old-release"
  mkdir -p "$FX_OLD_DIR"
  echo 'services: {}' >"$FX_OLD_DIR/docker-compose.yml"
  FX_PREV="$FX_TMP/previous-release.json"
  cat >"$FX_PREV" <<JSON
{
  "manifest_sha256": "$(printf 'd%.0s' $(seq 1 64))",
  "api_image": "mytechpulse-api-python-previous",
  "frontend_deployment_id": "deploy-0001",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "$(printf 'f%.0s' $(seq 1 64))",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "$(printf '1%.0s' $(seq 1 64))",
  "ops_release_dir": "${FX_OLD_DIR}"
}
JSON
  export MTP_PREVIOUS_RELEASE_RECORD="$FX_PREV"
}

# 合成のdumpとchecksumを作る。gpg=暗号化dump風（.gpg） / plain=平文dump風
setup_dump() {
  local kind="$1" dir="$FX_TMP/dumps"
  mkdir -p "$dir"
  case "$kind" in
    gpg) FX_DUMP="$dir/rehearsal.dump.gpg" ;;
    plain) FX_DUMP="$dir/rehearsal.dump" ;;
  esac
  for _ in $(seq 1 12); do printf 'SYNTHETIC-ENCRYPTED-OR-PLAIN-BYTES'; done >"$FX_DUMP"
  (cd "$dir" && sha256sum "$(basename "$FX_DUMP")" >"$(basename "$FX_DUMP").sha256")
  export MTP_REHEARSAL_DUMP="$FX_DUMP"
}

run_rehearsal() {
  set +e
  TMPDIR="$RH_TMPDIR" bash "$rehearsal" "$FX_ART" >"$FX_TMP/out" 2>"$FX_TMP/err"
  RH_STATUS=$?
  set -e
}

calls() { cat "$FAKE_CALLS"; }
calls_has() { grep -qE -- "$1" "$FAKE_CALLS"; }
expect_calls_has() { calls_has "$1" || { calls >&2; fail "呼び出しが無い: $1"; }; }
expect_calls_lacks() { ! calls_has "$1" || { calls >&2; fail "呼ばれてはいけない: $1"; }; }
first_line() { grep -nE -m1 -- "$1" "$FAKE_CALLS" | cut -d: -f1; }
last_line() { grep -nE -- "$1" "$FAKE_CALLS" | tail -1 | cut -d: -f1; }

# 引数のpatternが、記録の中で最初に現れる順に並んでいること
expect_order() {
  local previous=0 current pattern
  for pattern in "$@"; do
    current="$(first_line "$pattern")"
    [ -n "$current" ] || { calls >&2; fail "呼び出しが無い: $pattern"; }
    [ "$current" -gt "$previous" ] || { calls >&2; fail "順序が違う: $pattern"; }
    previous="$current"
  done
}

expect_status() {
  [ "$RH_STATUS" -eq "$1" ] || {
    cat "$FX_TMP/out" "$FX_TMP/err" >&2
    fail "終了コード $RH_STATUS（$1 を期待）"
  }
}

# 一時directory（復号したdump・snapshot・nonce）が残っていないこと
expect_no_leftover() {
  [ -z "$(ls -A "$RH_TMPDIR")" ] || fail "一時fileが残っている: $(ls -A "$RH_TMPDIR")"
}

# 起動前に拒否した場合は、dockerを何も起動していない
expect_rejected_before_start() {
  run_rehearsal
  expect_status 2
  expect_calls_lacks 'up -d'
  expect_calls_lacks 'pg_restore'
  expect_calls_lacks 'run --rm migrate'
  expect_no_leftover
}

all_steps=(inputs release-verify previous-release image-pull decrypt db-start backup-verify restore
  snapshot-before migrate snapshot-after compare serve smoke expected-diff cleanup-compare rollback switch-back)

# ---- 全工程が通る ----

case_full_flow_with_encrypted_dump() {
  run_rehearsal
  expect_status 0
  # 工程は決められた順に、名前・秒数・okだけで出る
  local previous=0 step line
  for step in "${all_steps[@]}"; do
    line="$(grep -nE "^rehearsal: ${step} [0-9]+s ok$" "$FX_TMP/out" | cut -d: -f1)"
    [ -n "$line" ] || { cat "$FX_TMP/out" >&2; fail "工程 $step が ok で出ていない"; }
    [ "$line" -gt "$previous" ] || fail "工程 $step の順序が違う"
    previous="$line"
  done
  grep -qE '^rehearsal: stop-equivalent [0-9]+s \(limit 1800s\), rollback [0-9]+s, total [0-9]+s$' "$FX_TMP/out" ||
    fail "所要時間の行が無い"
  [ "$(tail -1 "$FX_TMP/out")" = 'rehearsal: ok' ] || fail "最後の行が rehearsal: ok でない"
  # 復元先のDB名は、後のブラウザ確認で使うため最後に表示する。実際に使った名前と同じであること
  local shown_db
  shown_db="$(grep -Eo '^rehearsal: database mtp_rehearsal_[0-9]{14}_[0-9a-f]{6}$' "$FX_TMP/out" | sed 's/rehearsal: database //')"
  [ -n "$shown_db" ] || fail "復元先のDB名が表示されていない"
  expect_calls_has "db=${shown_db}\] "

  expect_order 'up -d --wait db' 'pg_restore --list' 'pg_restore -U' \
    'snapshot script=.* out=before ' 'run --rm migrate' 'snapshot script=.* out=after ' \
    'compare script=.* before.json after.json  status=0' 'up -d --wait api caddy' 'smoke script' \
    'snapshot script=.* out=after-write ' 'compare script=.* before.json after-write.json  status=1' \
    'smoke_user=rehearsal-smoke-[0-9a-f]{12}' 'snapshot script=.* out=after-cleanup ' \
    'compare script=.* before.json after-cleanup.json --allow-sequence-advance status=0' \
    'stop api caddy' 'up -d --wait api-legacy' 'stop api-legacy'
  # 旧releaseへ戻した後、Go版を同じ組合せで起動し直している
  [ "$(last_line 'up -d --wait api caddy')" -gt "$(first_line 'stop api-legacy')" ] || fail "再切替が旧API停止の後でない"

  # 復元に渡したのは、復号した内容（暗号化fileそのものではない）
  expect_calls_has "restore-stdin=$SYNTHETIC_DECRYPTED"
  expect_calls_has 'gpg --quiet --decrypt --output '
  # 実行が終わったら、止めるだけ。volumeやDBは消さない
  expect_calls_has 'stop$'
  expect_calls_lacks ' down( |$)'
  expect_calls_lacks 'volume '
  expect_no_leftover
}

# API imageはmanifestの完全digest、切り戻し先は直前のreleaseの記録、復元先は新しいDB
case_images_and_database_come_from_inputs() {
  run_rehearsal
  expect_status 0
  expect_calls_has "docker \[image=${FX_IMAGE//\//\\/} legacy=mytechpulse-api-python-previous db=mtp_rehearsal_[0-9]{14}_[0-9a-f]{6}\] "
  # 検証済みの運用一式のscriptを使い、checkoutのscriptは使わない
  local release_dir="$FX_ROOT/${FX_COMMIT}-${FX_RUN_ID}-${FX_ATTEMPT}"
  expect_calls_has "snapshot script=${release_dir}/ops/snapshot_migration_state.sh"
  expect_calls_has "compare script=${release_dir}/ops/compare_migration_state.sh"
  expect_calls_has "smoke script=${release_dir}/ops/rehearsal_smoke.sh"
  # 合成利用者の名前は、smokeと後始末で同じ
  local smoke_user cleanup_user
  smoke_user="$(grep -Eo 'smoke script=.* user=rehearsal-smoke-[0-9a-f]{12}' "$FAKE_CALLS" | sed 's/.*user=//')"
  cleanup_user="$(grep -Eo 'smoke_user=rehearsal-smoke-[0-9a-f]{12}' "$FAKE_CALLS" | sed 's/smoke_user=//')"
  [ -n "$smoke_user" ] && [ "$smoke_user" = "$cleanup_user" ] || fail "smokeと後始末の合成利用者名が違う"
  expect_calls_has 'tag_max=7'
}

# 出力には、工程名・秒数・成否と固定の文だけを出す
case_output_has_no_sensitive_values() {
  run_rehearsal
  expect_status 0
  local value
  for value in "$MTP_REHEARSAL_DB_PASSWORD" 'rehearsal-smoke-' "$SYNTHETIC_DECRYPTED" 'SYNTHETIC-ENCRYPTED' \
    "$FX_DUMP" 'fake-nonce'; do
    if grep -qF -- "$value" "$FX_TMP/out" "$FX_TMP/err"; then
      fail "出力に機密値が出ている: $value"
    fi
  done
  # 工程の行は、決まった形だけ
  local unexpected
  unexpected="$(grep -vE '^rehearsal: |^snapshot: ok$|^smoke: |^\{"matches":' "$FX_TMP/out" || true)"
  [ -z "$unexpected" ] || fail "想定外の出力がある: $unexpected"
}

case_synthetic_plain_dump_is_accepted_only_when_declared() {
  setup_dump plain
  # 宣言が無ければ、平文のdumpは受け付けない
  expect_rejected_before_start
  : >"$FAKE_CALLS"
  export MTP_REHEARSAL_SYNTHETIC=1
  run_rehearsal
  expect_status 0
  expect_calls_lacks '^gpg '
  expect_calls_has 'pg_restore -U'
  expect_no_leftover
}

# ---- 起動前に失敗する ----

case_missing_dump_is_rejected() {
  export MTP_REHEARSAL_DUMP="$FX_TMP/dumps/none.dump.gpg"
  expect_rejected_before_start
}

case_missing_checksum_is_rejected() {
  rm -f -- "$FX_DUMP.sha256"
  expect_rejected_before_start
}

case_mismatched_checksum_is_rejected() {
  printf 'tampered' >>"$FX_DUMP"
  expect_rejected_before_start
  grep -q 'checksum' "$FX_TMP/err" || fail "checksum不一致の理由が出ていない"
}

case_explicit_image_is_rejected() {
  export MTP_REHEARSAL_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:$(printf 'b%.0s' $(seq 1 64))"
  expect_rejected_before_start
}

case_wrong_manifest_hash_is_rejected() {
  export MTP_MANIFEST_SHA256="$(printf '0%.0s' $(seq 1 64))"
  expect_rejected_before_start
}

# manifestを変えずにAPIのdigestだけ差し替えようとしても、manifestのhashが合わない
case_api_only_swap_is_rejected() {
  sed -i "s/@sha256:${FX_DIGEST}/@sha256:$(printf 'c%.0s' $(seq 1 64))/" "$MTP_RELEASE_MANIFEST"
  expect_rejected_before_start
}

case_tampered_frontend_archive_is_rejected() {
  printf 'tampered' >>"$FX_ART/frontend-${FX_COMMIT}.tar.gz"
  expect_rejected_before_start
}

case_missing_ops_archive_is_rejected() {
  rm -f -- "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  expect_rejected_before_start
}

case_wrong_run_id_is_rejected() {
  export MTP_RELEASE_RUN_ID=999
  expect_rejected_before_start
}

case_missing_previous_record_is_rejected() {
  rm -f -- "$FX_PREV"
  expect_rejected_before_start
}

case_missing_previous_image_is_rejected() {
  export FAKE_FAIL='image inspect'
  expect_rejected_before_start
}

case_bundle_without_cleanup_sql_is_rejected() {
  local src="$FX_TMP/src/ops"
  rm -f -- "$src/ops/sql/rehearsal_cleanup.sql"
  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src" docker-compose.yml Caddyfile ops docker-compose.rehearsal.yml
  fx_write_manifest
  expect_rejected_before_start
}

# ---- 途中で失敗したら、後続へ進まない ----

case_decrypt_failure_stops_before_start() {
  export FAKE_GPG_FAIL=1
  run_rehearsal
  expect_status 1
  grep -qE '^rehearsal: decrypt [0-9]+s failed$' "$FX_TMP/err" || fail "decryptの失敗が出ていない"
  expect_calls_lacks 'up -d'
  expect_calls_lacks 'pg_restore'
  expect_no_leftover
}

# 失敗させる呼び出し / 失敗する工程 / 実行されてはいけない呼び出し
expect_stops_at() {
  local step="$1" forbidden="$2"
  run_rehearsal
  expect_status 1
  grep -qE "^rehearsal: ${step} [0-9]+s failed$" "$FX_TMP/err" || { cat "$FX_TMP/out" "$FX_TMP/err" >&2; fail "$step の失敗が出ていない"; }
  expect_calls_lacks "$forbidden"
  # 失敗しても、止めるだけ。一時fileは消える
  expect_calls_has 'stop$'
  expect_calls_lacks ' down( |$)'
  expect_calls_lacks 'volume '
  expect_no_leftover
}

case_backup_verify_failure_stops() {
  export FAKE_FAIL='pg_restore --list'
  expect_stops_at backup-verify 'pg_restore -U'
}

case_restore_failure_stops() {
  export FAKE_FAIL='pg_restore -U'
  expect_stops_at restore 'snapshot script'
}

case_snapshot_before_failure_stops() {
  export FAKE_SNAPSHOT_FAIL=before
  expect_stops_at snapshot-before 'run --rm migrate'
}

case_migration_failure_stops() {
  export FAKE_FAIL='run --rm migrate'
  expect_stops_at migrate 'out=after '
}

case_comparison_mismatch_stops_before_serving() {
  export FAKE_COMPARE_EXITS='1'
  expect_stops_at compare 'up -d --wait api caddy'
}

case_comparison_unable_stops_before_serving() {
  export FAKE_COMPARE_EXITS='2'
  expect_stops_at compare 'up -d --wait api caddy'
}

case_serve_failure_stops_before_smoke() {
  export FAKE_CURL_FAIL='18001'
  expect_stops_at serve 'smoke script'
}

case_smoke_failure_stops_before_switching() {
  export FAKE_SMOKE_FAIL=1
  expect_stops_at smoke 'out=after-write'
  expect_calls_lacks 'api-legacy'
}

case_missing_expected_diff_stops() {
  export FAKE_COMPARE_EXITS='0 0'
  expect_stops_at expected-diff 'smoke_user='
}

case_cleanup_mismatch_stops_before_rollback() {
  export FAKE_COMPARE_EXITS='0 1 1'
  expect_stops_at cleanup-compare 'api-legacy'
}

case_cleanup_failure_stops_before_rollback() {
  export FAKE_FAIL='smoke_user='
  expect_stops_at cleanup-compare 'api-legacy'
}

case_rollback_failure_is_reported() {
  export FAKE_CURL_FAIL='18000'
  expect_stops_at rollback 'stop api-legacy'
}

# ---- 時間 ----

# 停止相当が上限を超えたら失敗にする。上限は1800秒を超えて広げられない
case_window_over_limit_fails() {
  export MTP_REHEARSAL_WINDOW_LIMIT_SECONDS=0 FAKE_SMOKE_SLEEP=1
  run_rehearsal
  expect_status 1
  grep -q '30分を超えた' "$FX_TMP/err" || fail "時間超過の理由が出ていない"
  [ "$(tail -1 "$FX_TMP/out")" != 'rehearsal: ok' ] || fail "時間超過なのに ok と出ている"
}

case_window_limit_cannot_be_extended() {
  export MTP_REHEARSAL_WINDOW_LIMIT_SECONDS=1801
  run_rehearsal
  expect_status 2
  expect_calls_lacks 'up -d'
}

# ---- 静的な確認 ----

case_script_never_deletes_data() {
  # volumeやDBを消す操作が、scriptに入っていない
  if grep -nE 'down|volume rm|prune|dropdb|DROP DATABASE|rm -rf' "$rehearsal" | grep -vE '^[0-9]+:\s*#|rm -rf -- "\$work"'; then
    fail "削除につながる操作がある"
  fi
  # 公開してよいのは、固定の文と工程名だけ
  if grep -nE 'set -x|xtrace' "$rehearsal" | grep -v 'set +x'; then
    fail "shell traceを有効にしている"
  fi
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
  run_case "$case_name" "$case_name"
done

printf 'OK: %s rehearsal cases passed\n' "$passed"
