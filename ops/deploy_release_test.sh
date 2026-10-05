#!/usr/bin/env bash
# ops/deploy_release.sh を、偽のdocker/git/curlで確かめる。
# 本物のdockerやネットワークは使わない。使い方: bash ops/deploy_release_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"
deploy="$repo_root/ops/deploy_release.sh"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

# 1件ごとに、新しい一時directoryと環境で実行する。
# `|| fail` の形で呼ぶとサブシェル内のset -eが無効になるため、終了コードは別に受け取る
run_case() {
  local name="$1" status
  shift
  set +e
  (
    set -e
    fx_init
    trap 'rm -rf "$FX_TMP"' EXIT
    setup_fakes
    setup_previous_record
    "$@"
  )
  status=$?
  set -e
  [ "$status" -eq 0 ] || fail "$name"
  passed=$((passed + 1))
  printf 'ok: %s\n' "$name"
}

# 呼ばれたコマンドを記録するだけの偽物を PATH の先頭へ置く。
# 反映scriptが呼んではいけないもの（git・curl・ssh・go・npm）も偽物にして、呼ばれたら分かるようにする
setup_fakes() {
  FX_BIN="$FX_TMP/bin"
  FX_CALLS="$FX_TMP/calls.log"
  mkdir -p "$FX_BIN"
  : > "$FX_CALLS"
  local tool
  for tool in docker git curl ssh scp rsync go npm; do
    cat > "$FX_BIN/$tool" <<FAKE
#!/usr/bin/env bash
echo "$tool \$*" >> "$FX_CALLS"
if [ "$tool" = docker ]; then
  echo "GO_API_IMAGE=\${GO_API_IMAGE:-}" >> "$FX_CALLS"
  [ "\${FAKE_DOCKER_FAIL:-}" = 1 ] && exit 1
fi
exit 0
FAKE
    chmod +x "$FX_BIN/$tool"
  done
  export PATH="$FX_BIN:$PATH"
}

# 直前に稼働していたreleaseの記録と、そのrelease directoryを用意する
setup_previous_record() {
  FX_OLD_DIR="$FX_TMP/old-release"
  mkdir -p "$FX_OLD_DIR"
  echo "old" > "$FX_OLD_DIR/marker"
  FX_PREV="$FX_ROOT/current-release.json"
  write_previous_record "$FX_PREV"
}

write_previous_record() {
  cat > "$1" <<JSON
{
  "manifest_sha256": "$(printf 'd%.0s' $(seq 1 64))",
  "api_image": "ghcr.io/h4aruki/mytechpulse-api-go@sha256:$(printf 'e%.0s' $(seq 1 64))",
  "frontend_deployment_id": "deploy-0001",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "$(printf 'f%.0s' $(seq 1 64))",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "$(printf '1%.0s' $(seq 1 64))",
  "ops_release_dir": "${FX_OLD_DIR}"
}
JSON
}

run_deploy() {
  set +e
  bash "$deploy" "$FX_ART" >"$FX_TMP/out" 2>"$FX_TMP/err"
  DEPLOY_STATUS=$?
  set -e
}

release_dir() {
  echo "$FX_ROOT/${FX_COMMIT}-${FX_RUN_ID}-${FX_ATTEMPT}"
}

# 拒否した場合は、外部コマンドを1つも呼ばず、release directoryも作らない
expect_reject_untouched() {
  local needle="$1"
  run_deploy
  [ "$DEPLOY_STATUS" -eq 2 ] || { cat "$FX_TMP/err" >&2; echo "終了コード $DEPLOY_STATUS（2を期待）" >&2; return 1; }
  grep -q -- "$needle" "$FX_TMP/err" || { cat "$FX_TMP/err" >&2; echo "拒否理由に '$needle' が無い" >&2; return 1; }
  [ ! -s "$FX_CALLS" ] || { cat "$FX_CALLS" >&2; echo "拒否したのに外部コマンドが呼ばれた" >&2; return 1; }
  [ ! -e "$(release_dir)" ] || { echo "拒否したのにrelease directoryが作られた" >&2; return 1; }
}

case_normal() {
  run_deploy
  [ "$DEPLOY_STATUS" -eq 0 ] || { cat "$FX_TMP/err" >&2; return 1; }
  local dir
  dir="$(release_dir)"
  # 呼ばれたのは「そのrelease内のcomposeでAPIのdigestをpullする」1回だけ
  local expected
  expected="docker compose -p mytechpulse -f ${dir}/docker-compose.yml --profile go-preview pull api-go
GO_API_IMAGE=${FX_IMAGE}"
  [ "$(cat "$FX_CALLS")" = "$expected" ] || { cat "$FX_CALLS" >&2; echo "呼ばれたコマンドが想定と違う" >&2; return 1; }
  # 展開済みの運用一式とfrontend archive
  [ -f "$dir/docker-compose.yml" ] || return 1
  [ -f "$dir/frontend-${FX_COMMIT}.tar.gz" ] || return 1
  # 切り戻し用の記録は、直前の記録そのまま
  cmp "$dir/previous-release.json" "$FX_PREV" || { echo "previous-release.json が直前の記録と違う" >&2; return 1; }
  # 直前のrelease directoryは触らない
  [ "$(cat "$FX_OLD_DIR/marker")" = "old" ] || return 1
  [ "$(ls -A "$FX_OLD_DIR")" = "marker" ] || return 1
  grep -qx "GO_API_IMAGE=$FX_IMAGE" "$dir/release.env" || { echo "release.env にdigest指定のimageが無い" >&2; return 1; }
}

case_no_forbidden_calls() {
  run_deploy
  [ "$DEPLOY_STATUS" -eq 0 ] || { cat "$FX_TMP/err" >&2; return 1; }
  local word
  for word in "^git " "^curl " "^ssh " "^scp " "^rsync " "^go " "^npm " " build" " prune" " up" " down" " run" " exec" " rm" " migrate" " restart" " stop" "caddy"; do
    if grep -q -- "$word" "$FX_CALLS"; then
      cat "$FX_CALLS" >&2
      echo "呼んではいけない操作が記録された: $word" >&2
      return 1
    fi
  done
}

case_script_has_no_forbidden_commands() {
  # コメントを除いた本文に、git更新・build・prune・削除・migration・公開操作が無い
  local body
  body="$(grep -v '^[[:space:]]*#' "$deploy" "$repo_root/ops/verify_release.sh")"
  local word
  for word in "git " "docker compose build" "docker build" "prune" "rm -" "rmdir" "curl" "ssh " "migrate" "reset --hard" "wrangler" "caddy" "docker compose up" "docker compose down"; do
    if printf '%s\n' "$body" | grep -q -- "$word"; then
      printf '%s\n' "$body" | grep -- "$word" >&2
      echo "反映scriptに含めてはいけない記述: $word" >&2
      return 1
    fi
  done
}

case_manifest_unset() {
  unset MTP_RELEASE_MANIFEST
  expect_reject_untouched "MTP_RELEASE_MANIFEST"
}

case_manifest_hash_mismatch() {
  export MTP_MANIFEST_SHA256="$(printf 'b%.0s' $(seq 1 64))"
  expect_reject_untouched "SHA256"
}

case_run_mismatch() {
  export MTP_RELEASE_RUN_ID="1"
  expect_reject_untouched "run_id"
}

case_attempt_mismatch() {
  export MTP_RELEASE_RUN_ATTEMPT="9"
  expect_reject_untouched "run_attempt"
}

case_tag_only() {
  FX_API_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go:latest" fx_write_manifest
  expect_reject_untouched "api_image"
}

case_other_repository() {
  FX_API_IMAGE="ghcr.io/other/mytechpulse-api-go@sha256:${FX_DIGEST}" fx_write_manifest
  expect_reject_untouched "api_image"
}

case_short_digest() {
  FX_API_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:${FX_DIGEST:0:10}" fx_write_manifest
  expect_reject_untouched "api_image"
}

case_frontend_tampered() {
  printf 'x' >> "$FX_ART/frontend-${FX_COMMIT}.tar.gz"
  expect_reject_untouched "frontend のarchiveのsha256"
}

case_ops_tampered() {
  printf 'x' >> "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  expect_reject_untouched "ops のarchiveのsha256"
}

case_dangerous_archive() {
  fx_craft_tar symlink "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  fx_write_manifest
  expect_reject_untouched "symlink"
}

case_existing_release_dir() {
  mkdir -p "$(release_dir)"
  echo keep > "$(release_dir)/marker"
  run_deploy
  [ "$DEPLOY_STATUS" -eq 2 ] || return 1
  [ ! -s "$FX_CALLS" ] || return 1
  [ "$(ls -A "$(release_dir)")" = "marker" ] || return 1
}

case_no_previous_record() {
  rm "$FX_PREV"
  expect_reject_untouched "直前のrelease"
}

case_previous_record_from_env() {
  # 初回（Python版が稼働中）は、保全済みの記録を環境変数で渡す
  local first="$FX_TMP/first-release.json"
  write_previous_record "$first"
  rm "$FX_PREV"
  export MTP_PREVIOUS_RELEASE_RECORD="$first"
  run_deploy
  [ "$DEPLOY_STATUS" -eq 0 ] || { cat "$FX_TMP/err" >&2; return 1; }
  cmp "$(release_dir)/previous-release.json" "$first" || return 1
}

case_previous_record_incomplete() {
  grep -v '"frontend_deployment_id"' "$FX_PREV" > "$FX_TMP/incomplete.json"
  mv "$FX_TMP/incomplete.json" "$FX_PREV"
  expect_reject_untouched "直前のrelease"
}

case_previous_record_dir_missing() {
  rm -r "$FX_OLD_DIR"
  expect_reject_untouched "直前のrelease"
}

case_pull_failure_keeps_old_release() {
  export FAKE_DOCKER_FAIL=1
  run_deploy
  [ "$DEPLOY_STATUS" -eq 1 ] || { cat "$FX_TMP/err" >&2; echo "終了コード $DEPLOY_STATUS（1を期待）" >&2; return 1; }
  grep -q "pull" "$FX_TMP/err" || return 1
  # 失敗しても、直前のrelease directoryと記録はそのまま
  [ "$(cat "$FX_OLD_DIR/marker")" = "old" ] || return 1
  [ -f "$FX_PREV" ] || return 1
  # 呼ばれたのはpullの1回だけ（git・build・prune・起動は無い）
  [ "$(grep -c '^docker ' "$FX_CALLS")" = 1 ] || { cat "$FX_CALLS" >&2; return 1; }
}

run_case "正常: 検証→展開→digest pull の順に、pullだけを呼ぶ" case_normal
run_case "git・curl・build・prune・migration・起動を呼ばない" case_no_forbidden_calls
run_case "scriptの本文に危険な操作が無い" case_script_has_no_forbidden_commands
run_case "manifest未設定を拒否" case_manifest_unset
run_case "manifest hash不一致を拒否" case_manifest_hash_mismatch
run_case "run ID不一致を拒否" case_run_mismatch
run_case "run attempt不一致を拒否" case_attempt_mismatch
run_case "tagだけのimageを拒否" case_tag_only
run_case "別repositoryのimageを拒否" case_other_repository
run_case "短いdigestを拒否" case_short_digest
run_case "frontend archive改変を拒否" case_frontend_tampered
run_case "ops archive改変を拒否" case_ops_tampered
run_case "危険なarchiveを拒否" case_dangerous_archive
run_case "既存release directoryへの上書きを拒否" case_existing_release_dir
run_case "直前のreleaseの記録が無ければ拒否" case_no_previous_record
run_case "初回は環境変数で渡した記録を使う" case_previous_record_from_env
run_case "項目が足りない直前の記録を拒否" case_previous_record_incomplete
run_case "直前のrelease directoryが無ければ拒否" case_previous_record_dir_missing
run_case "pull失敗時は終了し、旧releaseを保持する" case_pull_failure_keeps_old_release

printf '合格: %s件\n' "$passed"
