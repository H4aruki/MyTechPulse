#!/usr/bin/env bash
# ops/verify_release.sh の拒否条件と正常系を確かめる。
# 使い方: bash ops/verify_release_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"
verify="$repo_root/ops/verify_release.sh"

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
    "$@"
  )
  status=$?
  set -e
  [ "$status" -eq 0 ] || fail "$name"
  passed=$((passed + 1))
  printf 'ok: %s
' "$name"
}

# verify_release.sh を実行して終了コードと標準エラーを控える
verify_status() {
  set +e
  bash "$verify" "$FX_ART" >"$FX_TMP/out" 2>"$FX_TMP/err"
  VERIFY_STATUS=$?
  set -e
}

expect_reject() {
  local needle="$1"
  verify_status
  [ "$VERIFY_STATUS" -eq 2 ] || { cat "$FX_TMP/err" >&2; echo "終了コード $VERIFY_STATUS（2を期待）" >&2; return 1; }
  grep -q -- "$needle" "$FX_TMP/err" || { cat "$FX_TMP/err" >&2; echo "拒否理由に '$needle' が無い" >&2; return 1; }
  # 拒否したときは、release directoryを1つも作らない
  [ -z "$(ls -A "$FX_ROOT")" ] || { echo "拒否したのにrelease directoryが作られた" >&2; return 1; }
}

case_normal() {
  verify_status
  [ "$VERIFY_STATUS" -eq 0 ] || { cat "$FX_TMP/err" >&2; return 1; }
  local dir="$FX_ROOT/${FX_COMMIT}-${FX_RUN_ID}-${FX_ATTEMPT}"
  [ -f "$dir/docker-compose.yml" ] || { echo "composeが展開されていない" >&2; return 1; }
  [ -f "$dir/Caddyfile" ] || return 1
  [ -f "$dir/ops/backup_db.sh" ] || return 1
  [ -f "$dir/frontend-${FX_COMMIT}.tar.gz" ] || { echo "frontend archiveが配置されていない" >&2; return 1; }
  [ "$(fx_sha256 "$dir/frontend-${FX_COMMIT}.tar.gz")" = "$(fx_sha256 "$FX_ART/frontend-${FX_COMMIT}.tar.gz")" ] || return 1
  grep -qx "MTP_RELEASE_DIR=$dir" "$FX_TMP/out" || { cat "$FX_TMP/out" >&2; return 1; }
  grep -qx "GO_API_IMAGE=$FX_IMAGE" "$FX_TMP/out" || return 1
  grep -qx "MTP_FRONTEND_ARCHIVE=$dir/frontend-${FX_COMMIT}.tar.gz" "$FX_TMP/out" || return 1
  grep -qx "GO_API_IMAGE=$FX_IMAGE" "$dir/release.env" || return 1
}

case_manifest_unset() {
  unset MTP_RELEASE_MANIFEST
  expect_reject "MTP_RELEASE_MANIFEST"
}

case_manifest_sha_unset() {
  unset MTP_MANIFEST_SHA256
  expect_reject "MTP_MANIFEST_SHA256"
}

case_manifest_hash_mismatch() {
  export MTP_MANIFEST_SHA256="$(printf 'b%.0s' $(seq 1 64))"
  expect_reject "SHA256"
}

case_manifest_edited_after_hash() {
  # APIのimageだけ差し替えたmanifest。保存済みのSHA256とは合わない
  sed -i "s/${FX_DIGEST}/$(printf 'c%.0s' $(seq 1 64))/" "$MTP_RELEASE_MANIFEST"
  expect_reject "SHA256"
}

case_run_id_mismatch() {
  export MTP_RELEASE_RUN_ID="9999"
  expect_reject "run_id"
}

case_run_attempt_mismatch() {
  export MTP_RELEASE_RUN_ATTEMPT="2"
  expect_reject "run_attempt"
}

case_tag_only() {
  FX_API_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go:latest" fx_write_manifest
  expect_reject "api_image"
}

case_commit_tag_only() {
  FX_API_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go:${FX_COMMIT}" fx_write_manifest
  expect_reject "api_image"
}

case_other_repository() {
  FX_API_IMAGE="ghcr.io/someone/mytechpulse-api-go@sha256:${FX_DIGEST}" fx_write_manifest
  expect_reject "api_image"
}

case_short_digest() {
  FX_API_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:${FX_DIGEST:0:63}" fx_write_manifest
  expect_reject "api_image"
}

case_frontend_hash_mismatch() {
  printf 'x' >> "$FX_ART/frontend-${FX_COMMIT}.tar.gz"
  expect_reject "frontend のarchiveのsha256"
}

case_ops_hash_mismatch() {
  printf 'x' >> "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  expect_reject "ops のarchiveのsha256"
}

case_artifact_missing() {
  rm "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  expect_reject "ops-"
}

case_manifest_commit_vs_file_name() {
  # manifest内のcommitとarchive名がずれている
  FX_MANIFEST_COMMIT="fedcba9876543210fedcba9876543210fedcba98" fx_write_manifest
  expect_reject "frontend"
}

case_manifest_not_canonical() {
  # 項目を足したmanifest（hashは合わせてある）
  sed -i 's/"schema_version": 1,/"schema_version": 1,\n  "extra": "x",/' "$MTP_RELEASE_MANIFEST"
  fx_export_inputs
  expect_reject "manifest"
}

case_dangerous_archive() {
  local kind="$1" needle="$2"
  fx_craft_tar "$kind" "$FX_ART/ops-${FX_COMMIT}.tar.gz"
  fx_write_manifest
  expect_reject "$needle"
  [ ! -e "$FX_TMP/mtp-escape.txt" ] || return 1
}

case_dangerous_frontend_archive() {
  fx_craft_tar symlink "$FX_ART/frontend-${FX_COMMIT}.tar.gz"
  fx_write_manifest
  expect_reject "frontend"
}

case_existing_release_dir() {
  local dir="$FX_ROOT/${FX_COMMIT}-${FX_RUN_ID}-${FX_ATTEMPT}"
  mkdir -p "$dir"
  echo "keep" > "$dir/marker"
  verify_status
  [ "$VERIFY_STATUS" -eq 2 ] || return 1
  grep -q "既に存在" "$FX_TMP/err" || { cat "$FX_TMP/err" >&2; return 1; }
  [ "$(cat "$dir/marker")" = "keep" ] || { echo "既存directoryが書き換えられた" >&2; return 1; }
  [ "$(ls -A "$dir")" = "marker" ] || { echo "既存directoryへ書き込まれた" >&2; return 1; }
}

case_relative_root() {
  export MTP_RELEASES_ROOT="relative/releases"
  expect_reject "MTP_RELEASES_ROOT"
}

run_case "正常: 検証後にopsを展開しfrontend archiveを配置する" case_normal
run_case "manifest未設定を拒否" case_manifest_unset
run_case "manifestのSHA256未設定を拒否" case_manifest_sha_unset
run_case "manifestのhash不一致を拒否" case_manifest_hash_mismatch
run_case "APIだけ差し替えたmanifestを拒否" case_manifest_edited_after_hash
run_case "run ID不一致を拒否" case_run_id_mismatch
run_case "run attempt不一致を拒否" case_run_attempt_mismatch
run_case "tagだけのimageを拒否" case_tag_only
run_case "commit SHA tagだけのimageを拒否" case_commit_tag_only
run_case "別repositoryのimageを拒否" case_other_repository
run_case "短いdigestを拒否" case_short_digest
run_case "frontend archiveのhash不一致を拒否" case_frontend_hash_mismatch
run_case "ops archiveのhash不一致を拒否" case_ops_hash_mismatch
run_case "archiveの欠落を拒否" case_artifact_missing
run_case "commitとarchive名のずれを拒否" case_manifest_commit_vs_file_name
run_case "決められた形式でないmanifestを拒否" case_manifest_not_canonical
run_case "絶対pathを含むops archiveを拒否" case_dangerous_archive absolute "絶対path"
run_case "..を含むops archiveを拒否" case_dangerous_archive dotdot ".. を含む"
run_case "symlinkを含むops archiveを拒否" case_dangerous_archive symlink "symlink"
run_case "hardlinkを含むops archiveを拒否" case_dangerous_archive hardlink "hardlink"
run_case "危険なfrontend archiveを拒否" case_dangerous_frontend_archive
run_case "既存release directoryへの上書きを拒否" case_existing_release_dir
run_case "相対pathのrelease置き場を拒否" case_relative_root

printf '合格: %s件\n' "$passed"
