#!/usr/bin/env bash
# ops/cutover_prepare.sh を、偽のdockerと、本物の配布検証script（verify_release.sh・deploy_release.sh）で確かめる（#161）。
# 本物のdocker・本番・ネットワークには触れない。設定ファイルの値はすべて合成のダミー。
# 使い方: bash ops/cutover_prepare_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  # 直前の実行の出力を添える（原因の特定のため。値は合成のダミーだけ）
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
SECRET_TOKEN='QiitaTokenSynthetic-0002'
LEGACY_IMAGE_ID="sha256:$(printf 'b%.0s' $(seq 1 64))"

setup() {
  fx_init
  # 本番の配布と同じ作りのopsのarchive（リポジトリの運用ファイル一式）を作る
  local src="$FX_TMP/src/ops-real"
  mkdir -p "$src"
  (cd "$repo_root" && tar -cf - docker-compose.yml Caddyfile ops docker-compose.rehearsal.yml) | tar -xf - -C "$src"
  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src" docker-compose.yml Caddyfile ops docker-compose.rehearsal.yml
  fx_write_manifest
  MANIFEST_SHA="$MTP_MANIFEST_SHA256"
  unset MTP_RELEASE_MANIFEST MTP_MANIFEST_SHA256 MTP_RELEASE_RUN_ID MTP_RELEASE_RUN_ATTEMPT MTP_RELEASES_ROOT

  # opsのarchiveを、オーナーと同じように展開する
  BOOT="$FX_TMP/boot"
  mkdir -p "$BOOT"
  tar -xzf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$BOOT"

  # いま動いているPython版の運用ファイルと設定（合成）
  LEGACY="$FX_TMP/legacy"
  mkdir -p "$LEGACY/backend" "$LEGACY/ops"
  echo 'services: {}' >"$LEGACY/docker-compose.yml"
  : >"$LEGACY/Caddyfile"
  : >"$LEGACY/ops/backup_db.sh"
  : >"$LEGACY/ops/verify_backup.sh"
  printf 'POSTGRES_PASSWORD=%s\nAPI_DOMAIN=api.example.test\n' "$SECRET_PASSWORD" >"$LEGACY/.env"
  printf 'QIITA_ACCESS_TOKEN=%s\nSECRET_KEY=unused-synthetic\n' "$SECRET_TOKEN" >"$LEGACY/backend/.env"

  export HOME="$FX_TMP/home"
  mkdir -p "$HOME"
  export MTP_RELEASES_ROOT="$FX_ROOT"
  ENVF="$HOME/mytechpulse-production.env"

  export FAKE_CALLS="$FX_TMP/calls.log"
  : >"$FAKE_CALLS"
  unset FAKE_CONTAINERD FAKE_CONTAINER_CREATED FAKE_IMAGE_CREATED FAKE_IMAGE_EXTRA FAKE_IMAGE_ENV_BAD FAKE_HISTORY_BAD FAKE_AUDIT_FAIL FAKE_NAMES_BAD FAKE_NO_API FAKE_BAD_IMAGE_ID FAKE_PULL_FAIL FAKE_PREFLIGHT_FAIL MTP_ENV_FILE MTP_CORS_ALLOWED_ORIGINS
  setup_fakes
}

setup_fakes() {
  mkdir -p "$FX_TMP/bin"
  # 箱の中身（期待どおりのもの）を、tarにしておく。偽のdockerが export で返す
  local image_root="$FX_TMP/image-root" file
  mkdir -p "$image_root/etc/ssl/certs" "$image_root/dev"
  for file in api migrate etc/ssl/certs/ca-certificates.crt .dockerenv dev/console etc/hostname etc/hosts etc/mtab etc/resolv.conf; do
    : >"$image_root/$file"
  done
  # 本物の docker export と同じく、先頭に ./ を付けない一覧にする
  tar -cf "$FX_TMP/image.tar" -C "$image_root" .dockerenv api dev etc migrate
  # 想定外のファイルが入っている箱（FAKE_IMAGE_EXTRA=1 のときに返す）
  : >"$image_root/unexpected-secret.txt"
  tar -cf "$FX_TMP/image-extra.tar" -C "$image_root" .dockerenv api dev etc migrate unexpected-secret.txt
  rm -f "$image_root/unexpected-secret.txt"
  cat >"$FX_TMP/bin/docker" <<'FAKE'
#!/usr/bin/env bash
args="$*"
printf 'docker [cwd=%s project=%s] %s\n' "${PWD##*/}" "${COMPOSE_PROJECT_NAME:-}" "$args" >>"$FAKE_CALLS"
case "$args" in
  "compose ps -q api") [ "${FAKE_NO_API:-}" = 1 ] || echo cid-api ;;
  # コンテナが持つ識別子。containerdの保存方式では、これで箱を引けない（FAKE_CONTAINERD=1）
  "inspect --format {{.Image}} cid-api") if [ "${FAKE_CONTAINERD:-}" = 1 ]; then echo "$FAKE_CONTAINERD_ID"; else echo "$FAKE_LEGACY_IMAGE_ID"; fi ;;
  "inspect --format {{.Config.Image}} cid-api") echo mytechpulse-api ;;
  "inspect --format {{.Created}} cid-api") echo "${FAKE_CONTAINER_CREATED:-2026-10-05T01:17:05.111952982Z}" ;;
  "image inspect "*"--format {{.Id}}")
    # 識別子の指定が、引けない識別子のときは失敗する
    if [ "${FAKE_CONTAINERD:-}" = 1 ] && [ "$3" = "$FAKE_CONTAINERD_ID" ]; then echo "Error response from daemon: No such image" >&2; exit 1; fi
    if [ "${FAKE_BAD_IMAGE_ID:-}" = 1 ]; then echo broken; else echo "$FAKE_LEGACY_IMAGE_ID"; fi ;;
  "image inspect "*"--format {{.Created}}") echo "${FAKE_IMAGE_CREATED:-2026-10-05T10:16:56.135812664+09:00}" ;;
  "tag "*) ;;
  create*) echo cid-inspect ;;
  "export cid-inspect")
    if [ "${FAKE_IMAGE_EXTRA:-}" = 1 ]; then cat "$FAKE_IMAGE_TAR_EXTRA"; else cat "$FAKE_IMAGE_TAR"; fi ;;
  "rm cid-inspect") ;;
  "image inspect "*"{{json .Config.Env}}")
    if [ "${FAKE_IMAGE_ENV_BAD:-}" = 1 ]; then echo '["PATH=/usr/bin","API_TOKEN=synthetic"]'; else echo '["PATH=/usr/local/sbin:/usr/bin"]'; fi ;;
  "history --no-trunc"*)
    echo 'COPY api /api'
    [ "${FAKE_HISTORY_BAD:-}" != 1 ] || echo 'ENV SECRET_KEY=synthetic' ;;
  *"exec -T db psql"*"-c SELECT count(*)"*) echo "${FAKE_NAMES_BAD:-0}" ;;
  *"exec -T db psql"*)
    cat >/dev/null
    if [ "${FAKE_AUDIT_FAIL:-}" = 1 ]; then exit 1; fi
    echo 0 ;;
  *"--profile go-preview pull api-go"*) [ "${FAKE_PULL_FAIL:-}" != 1 ] || exit 1 ;;
  # cutover.sh preflight が見る、動いているサービスなど
  "image inspect"*) ;;
  *" config"*) ;;
  *"ps --status running --services"*) printf 'db\napi\ncaddy\n' ;;
  *"ps -q caddy"*) echo cid-caddy ;;
  "inspect cid-caddy"*) echo '/home/user/MyTechPulse/Caddyfile' ;;
  *"exec -T -e POSTGRES_PASSWORD db sh -c"*) [ "${FAKE_PREFLIGHT_FAIL:-}" != 1 ] || exit 1 ;;
esac
exit 0
FAKE
  chmod +x "$FX_TMP/bin/docker"
  export FAKE_CONTAINERD_ID="sha256:$(printf 'c%.0s' $(seq 1 64))"
  export FAKE_LEGACY_IMAGE_ID="$LEGACY_IMAGE_ID" FAKE_IMAGE_TAR="$FX_TMP/image.tar" FAKE_IMAGE_TAR_EXTRA="$FX_TMP/image-extra.tar"
  export PATH="$FX_TMP/bin:$PATH"
}

# 準備を実行し、終了コードと出力を控える
prepare() {
  set +e
  bash "$BOOT/ops/cutover_prepare.sh" "$FX_ART" "${1:-$MANIFEST_SHA}" "$LEGACY" >"$FX_TMP/out" 2>"$FX_TMP/err"
  STATUS=$?
  set -e
  cat "$FX_TMP/out" "$FX_TMP/err" >>"$FX_TMP/all.log"
}

expect() {
  # expect 終了コード [Manifest SHA256]
  prepare "${2:-}"
  [ "$STATUS" -eq "$1" ] || {
    cat "$FX_TMP/out" "$FX_TMP/err" "$FAKE_CALLS" >&2
    fail "終了コード $STATUS（$1 を期待）"
  }
}
has() { grep -qE -- "$1" "$FAKE_CALLS"; }
expect_has() { has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼び出しが無い: $1"; }; }
expect_lacks() { ! has "$1" || { cat "$FAKE_CALLS" >&2; fail "呼ばれてはいけない: $1"; }; }
out_has() { grep -qE -- "$1" "$FX_TMP/out" "$FX_TMP/err"; }

nothing_created() {
  [ ! -e "$FX_ROOT/previous-release.json" ] || fail "切り戻しの記録が作られている"
  [ ! -e "$ENVF" ] || fail "設定ファイルが作られている"
  expect_lacks 'pull api-go'
  expect_lacks '^docker .* tag '
}

# ---- 成功 ----

case_prepare_succeeds_and_prints_the_two_commands() {
  expect 0
  out_has 'prepare: ok' || fail "完了の表示が無い"
  local release_dir
  release_dir="$(ls -d "$FX_ROOT"/${FX_COMMIT}-* | head -1)"
  [ -f "$release_dir/ops/cutover.sh" ] || fail "release directoryに ops/cutover.sh が無い"
  out_has "bash ${release_dir}/ops/cutover.sh part1" || fail "part1のコマンドが表示されていない"
  out_has "bash ${release_dir}/ops/cutover.sh part2" || fail "part2のコマンドが表示されていない"
  # 工程は、この順番で行われる
  local order=(audit previous-record release image-inspect env-file preflight) previous=0 name line
  for name in "${order[@]}"; do
    line="$(grep -nE -m1 "^prepare: $name [0-9]+s ok" "$FX_TMP/out" | cut -d: -f1)"
    [ -n "$line" ] && [ "$line" -gt "$previous" ] || fail "工程の順番が違う、または成功していない: $name"
    previous="$line"
  done
}

case_previous_release_record_matches_the_running_python_version() {
  expect 0
  local record="$FX_ROOT/previous-release.json"
  [ -f "$record" ] || fail "切り戻しの記録が無い"
  # 書式は、deploy_release.sh と cutover.sh が読むものと同じ（読み込む部品で確かめる）
  local result
  result="$(
    reject() { echo "拒否: $*" >&2; exit 2; }
    source "$repo_root/ops/lib/previous_release_record.sh"
    parse_previous_release_record "$record"
    printf '%s %s' "$PREV_API_IMAGE" "$PREV_OPS_DIR"
  )"
  case "$result" in
    "mytechpulse-legacy-api:pre-go-"*" $LEGACY") ;;
    *) fail "切り戻しの記録の中身が違う: $result" ;;
  esac
  # Python版の箱に別名を付けている（消えないように）
  expect_has "docker .* tag $LEGACY_IMAGE_ID mytechpulse-legacy-api:pre-go-"
  # release directoryにも、同じ記録が残る
  local release_dir
  release_dir="$(ls -d "$FX_ROOT"/${FX_COMMIT}-* | head -1)"
  cmp -s "$record" "$release_dir/previous-release.json" || fail "release directoryの記録が違う"
}

case_env_file_is_created_from_the_legacy_settings() {
  expect 0
  [ -f "$ENVF" ] || fail "設定ファイルが作られていない"
  grep -qx "POSTGRES_PASSWORD=$SECRET_PASSWORD" "$ENVF" || fail "POSTGRES_PASSWORD が写されていない"
  grep -qx "QIITA_ACCESS_TOKEN=$SECRET_TOKEN" "$ENVF" || fail "QIITA_ACCESS_TOKEN が写されていない"
  grep -qx 'API_DOMAIN=api.example.test' "$ENVF" || fail "API_DOMAIN が写されていない"
  grep -qx 'APP_ENV=production' "$ENVF" || fail "APP_ENV が production でない"
  grep -qx 'SWAGGER_ENABLED=false' "$ENVF" || fail "SWAGGER_ENABLED が false でない"
  grep -qx 'CORS_ALLOWED_ORIGINS=https://mytechpulse.net,https://www.mytechpulse.net' "$ENVF" || fail "CORS の既定値が違う"
  [ "$(wc -l <"$ENVF")" -eq 6 ] || fail "設定ファイルの項目数が違う"
  if [ "$(stat -c '%a' "$ENVF")" != 600 ]; then
    # chmodの結果を読み戻せない環境では確かめられない
    probe="$FX_TMP/probe"; : >"$probe"; chmod 600 "$probe"
    [ "$(stat -c '%a' "$probe")" != 600 ] || fail "設定ファイルの権限が 600 でない"
  fi
}

case_cors_origins_can_be_chosen() {
  MTP_CORS_ALLOWED_ORIGINS=https://front.test expect 0
  grep -qx 'CORS_ALLOWED_ORIGINS=https://front.test' "$ENVF" || fail "指定したオリジンが使われていない"
}

case_existing_env_file_is_left_alone() {
  printf 'POSTGRES_PASSWORD=keep-me\n' >"$ENVF"
  chmod 600 "$ENVF"
  # 既存の設定は、そのまま使う（ここでは項目が足りないので、事前確認が止める）
  expect 1
  [ "$(cat "$ENVF")" = "POSTGRES_PASSWORD=keep-me" ] || fail "既存の設定ファイルが書き換えられた"
}

case_prepare_never_prints_secrets() {
  expect 0
  local value
  for value in "$SECRET_PASSWORD" "$SECRET_TOKEN"; do
    if grep -qF -- "$value" "$FX_TMP/all.log" "$FAKE_CALLS"; then
      fail "出力か呼び出しに機密値が出ている: $value"
    fi
  done
}

case_prepare_changes_no_running_service() {
  expect 0
  expect_lacks ' up '
  expect_lacks ' stop '
  expect_lacks ' down'
  expect_lacks 'exec .*(INSERT|UPDATE|DELETE|DROP|ALTER)'
  expect_lacks 'run --rm migrate'
}

# ---- 拒否・失敗（何も作らない） ----

case_wrong_manifest_sha_is_rejected() {
  expect 2 "$(printf 'c%.0s' $(seq 1 64))"
  out_has 'Summaryの値と一致しません' || fail "理由が表示されていない"
  nothing_created
}

case_bad_arguments_are_rejected() {
  set +e
  bash "$BOOT/ops/cutover_prepare.sh" "$FX_ART" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "引数が足りないのに拒否されない"
  bash "$BOOT/ops/cutover_prepare.sh" "$FX_ART" "短い" "$LEGACY" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "不正なSHA256が拒否されない"
  bash "$BOOT/ops/cutover_prepare.sh" "$FX_TMP/nothing" "$MANIFEST_SHA" "$LEGACY" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "存在しないdirectoryが拒否されない"
  bash "$BOOT/ops/cutover_prepare.sh" "$FX_ART" "$MANIFEST_SHA" "$FX_TMP/nothing" >/dev/null 2>&1
  [ "$?" -eq 2 ] || fail "旧版の場所が無いのに拒否されない"
  set -e
  nothing_created
}

case_script_from_a_different_archive_is_rejected() {
  # 展開した場所のscriptを書き換えると、照合したarchiveと違うので拒否する
  echo '# 改ざん' >>"$BOOT/ops/deploy_release.sh"
  expect 2
  out_has '中身と違います' || fail "理由が表示されていない"
  nothing_created
}

case_tag_collision_stops_before_any_change() {
  FAKE_AUDIT_FAIL=1 expect 1
  out_has 'audit .*failed' || fail "失敗した工程が表示されていない"
  nothing_created
}

case_bad_user_names_stop_before_any_change() {
  FAKE_NAMES_BAD=2 expect 1
  out_has 'audit .*failed' || fail "失敗した工程が表示されていない"
  nothing_created
}

case_missing_python_api_stops() {
  FAKE_NO_API=1 expect 1
  out_has 'previous-record .*failed' || fail "失敗した工程が表示されていない"
  [ ! -e "$FX_ROOT/previous-release.json" ] || fail "切り戻しの記録が作られている"
  expect_lacks 'pull api-go'
}

case_unreadable_image_id_stops() {
  FAKE_BAD_IMAGE_ID=1 expect 1
  [ ! -e "$FX_ROOT/previous-release.json" ] || fail "切り戻しの記録が作られている"
  expect_lacks '^docker .* tag '
}

# containerdの保存方式（本番）: コンテナが持つ識別子では箱を引けない。名前から引き、作成日時で裏づけを取る
case_containerd_store_finds_the_image_by_name() {
  FAKE_CONTAINERD=1 expect 0
  # 引けなかった識別子ではなく、名前から引いた箱に別名を付けている
  expect_has "docker .* tag $LEGACY_IMAGE_ID mytechpulse-legacy-api:pre-go-"
  expect_lacks "tag $FAKE_CONTAINERD_ID"
  out_has 'prepare: previous-record [0-9]+s ok' || fail "工程が成功していない"
}

case_containerd_store_rejects_an_image_rebuilt_after_the_container() {
  # 箱が、コンテナより後に作り直されている（動いている箱と同じ保証がない）。別名を付けずに止まる
  FAKE_CONTAINERD=1 FAKE_IMAGE_CREATED=2026-10-06T00:00:00+09:00 FAKE_CONTAINER_CREATED=2026-10-05T01:17:05Z expect 1
  out_has '後に作り直されている' || fail "理由が表示されていない"
  expect_lacks '^docker .* tag '
  [ ! -e "$FX_ROOT/previous-release.json" ] || fail "切り戻しの記録が作られている"
}

case_containerd_store_rejects_unreadable_timestamps() {
  FAKE_CONTAINERD=1 FAKE_CONTAINER_CREATED=not-a-date expect 1
  out_has '作成日時を確認できない' || fail "理由が表示されていない"
  expect_lacks '^docker .* tag '
}

case_classic_store_does_not_need_the_name_lookup() {
  expect 0
  # 従来の保存方式では、コンテナが持つ識別子で引けるので、名前や日時の確認は要らない
  expect_lacks '{{.Config.Image}}'
  expect_lacks '{{.Created}}'
}

case_pull_failure_stops_before_env_file() {
  FAKE_PULL_FAIL=1 expect 1
  out_has 'release .*failed' || fail "失敗した工程が表示されていない"
  [ ! -e "$ENVF" ] || fail "pullに失敗したのに設定ファイルを作った"
}

case_image_with_unexpected_files_is_rejected() {
  FAKE_IMAGE_EXTRA=1 expect 1
  out_has 'image-inspect .*failed' || fail "失敗した工程が表示されていない"
  [ ! -e "$ENVF" ] || fail "箱が不合格なのに設定ファイルを作った"
  # 確認用のコンテナは、失敗しても片付ける
  expect_has 'docker .* rm cid-inspect'
}

case_image_with_baked_in_environment_is_rejected() {
  FAKE_IMAGE_ENV_BAD=1 expect 1
  out_has 'image-inspect .*failed' || fail "失敗した工程が表示されていない"
  [ ! -e "$ENVF" ] || fail "箱が不合格なのに設定ファイルを作った"
}

case_image_with_secret_looking_history_is_rejected() {
  FAKE_HISTORY_BAD=1 expect 1
  out_has 'image-inspect .*failed' || fail "失敗した工程が表示されていない"
  [ ! -e "$ENVF" ] || fail "箱が不合格なのに設定ファイルを作った"
}

case_missing_legacy_setting_names_only_the_key() {
  printf 'API_DOMAIN=api.example.test\n' >"$LEGACY/.env"
  expect 1
  out_has 'POSTGRES_PASSWORD' || fail "足りない項目名が表示されていない"
  [ ! -e "$ENVF" ] || fail "不完全な設定ファイルが作られている"
  if grep -qF -- "$SECRET_TOKEN" "$FX_TMP/out" "$FX_TMP/err"; then fail "値が出ている"; fi
}

case_preflight_failure_is_reported() {
  FAKE_PREFLIGHT_FAIL=1 expect 1
  out_has 'preflight .*failed' || fail "失敗した工程が表示されていない"
  out_has 'prepare: ok' && fail "失敗したのに完了と表示した"
  true
}

# ---- 静的な確認 ----

case_script_never_deletes_data_or_changes_services() {
  local script="$repo_root/ops/cutover_prepare.sh"
  if grep -nE 'rm -rf|rm -r |volume (rm|prune)|system prune|image prune|dropdb|DROP |pg_restore|compose.* (up|down|stop|restart|rm) ' "$script" |
    grep -vE '^[0-9]+:\s*#' | grep -vF 'rm -rf -- "$work"'; then
    fail "削除・サービスの変更につながる操作がある"
  fi
  if grep -nE 'set -x|xtrace' "$script" | grep -v 'set +x'; then
    fail "shell traceを有効にしている"
  fi
}

for case_name in $(declare -F | awk '{print $3}' | grep '^case_'); do
  # 特定の1項目だけを実行したいとき: ONLY=case_名 bash ops/cutover_prepare_test.sh
  [ -z "${ONLY:-}" ] || [ "$case_name" = "$ONLY" ] || continue
  run_case "$case_name" "$case_name"
done

printf 'OK: %s cutover prepare cases passed\n' "$passed"
