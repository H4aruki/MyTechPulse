#!/usr/bin/env bash
# 本番相当の移行と切り戻しを、隔離環境で最初から最後まで通して試す（#126）。
#
#   使い方: MTP_RELEASE_MANIFEST=... MTP_MANIFEST_SHA256=... \
#           MTP_RELEASE_RUN_ID=... MTP_RELEASE_RUN_ATTEMPT=... MTP_RELEASES_ROOT=/絶対path \
#           MTP_REHEARSAL_DUMP=/絶対path/xxx.dump.gpg MTP_REHEARSAL_DB_PASSWORD=... \
#           MTP_PREVIOUS_RELEASE_RECORD=/絶対path/previous-release.json \
#           bash ops/rehearsal.sh <同じrunからdownloadしたarchiveのdirectory>
#
# 入力（環境変数）
#   MTP_RELEASE_MANIFEST / MTP_MANIFEST_SHA256 / MTP_RELEASE_RUN_ID / MTP_RELEASE_RUN_ATTEMPT /
#   MTP_RELEASES_ROOT        verify_release.sh と同じ。API imageは、検証したmanifestの完全digestだけを使う
#                            （APIだけを差し替える MTP_REHEARSAL_IMAGE は受け付けない）
#   MTP_REHEARSAL_DUMP       復元するdump。本番由来は暗号化file（.gpg）だけ受け付ける。
#                            隣に <file>.sha256 が要る（sha256sum の出力形式）
#   MTP_REHEARSAL_SYNTHETIC  1のときだけ、暗号化していない合成dumpを受け付ける（本番由来には使わない）
#   MTP_REHEARSAL_DB_PASSWORD  隔離DBだけのpassword。本番のpasswordを使い回さない
#   MTP_PREVIOUS_RELEASE_RECORD  切り戻し先（直前のrelease）の記録。deploy_release.sh と同じ形式
#
# 流れ: 入力の確認 → manifest検証 → （暗号化dumpなら復号）→ 隔離DB起動 → 新しいDBへ復元
#   → 移行前snapshot → migration → 移行後snapshot → 内容比較 → Go版起動 → smoke
#   → 合成writeの差分確認 → 合成データの後始末と再比較 → 旧release（Python版API）へ戻す模擬 → Go版へ再切替
# 「停止相当」は、移行前snapshotからsmokeの終わりまで。30分（1800秒）を超えたら失敗にする。
#
# 公開する出力は、工程名・秒数・成功/失敗だけ。dumpの中身・利用者名・password・snapshot・
# nonceは出さない。工程の下で動くdockerやpg_restoreの出力は捨てる。
# 終了時はcontainerを止めるだけで（down -v はしない）、入力dump・復元したDB・volumeは残す。
# 消すのは、この実行で作った一時directory（復号したdump・snapshot・nonce）だけ。
# 終了コード: 0=すべて成功、1=どれかの工程が失敗、2=入力の拒否
set -euo pipefail
set +x
umask 077

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

reject() {
  echo "拒否: $*" >&2
  exit 2
}

[ "$#" -eq 1 ] && [ -n "$1" ] || reject "使い方: rehearsal.sh <archiveのdirectory>"
archive_dir="$1"

dump="${MTP_REHEARSAL_DUMP:-}"
db_password="${MTP_REHEARSAL_DB_PASSWORD:-}"
previous_record="${MTP_PREVIOUS_RELEASE_RECORD:-}"
releases_root="${MTP_RELEASES_ROOT:-}"

[ -z "${MTP_REHEARSAL_IMAGE:-}" ] ||
  reject "MTP_REHEARSAL_IMAGE は指定できません（API imageはmanifestの完全digestだけを使います）"
[ -n "$dump" ] || reject "MTP_REHEARSAL_DUMP が未設定です"
[ -n "$db_password" ] || reject "MTP_REHEARSAL_DB_PASSWORD が未設定です"
[[ "$db_password" =~ ^[A-Za-z0-9._~-]+$ ]] ||
  reject "MTP_REHEARSAL_DB_PASSWORD は英数字と . _ ~ - だけで指定してください"
[ -n "$previous_record" ] || reject "MTP_PREVIOUS_RELEASE_RECORD が未設定です（切り戻し先が決まりません）"
[ -n "$releases_root" ] || reject "MTP_RELEASES_ROOT が未設定です"

synthetic="${MTP_REHEARSAL_SYNTHETIC:-0}"
case "$synthetic" in
  0 | 1) ;;
  *) reject "MTP_REHEARSAL_SYNTHETIC は 0 か 1 で指定してください" ;;
esac

# 停止相当の上限は30分。試験のために短くすることはできるが、広げることはできない
window_limit_seconds="${MTP_REHEARSAL_WINDOW_LIMIT_SECONDS:-1800}"
[[ "$window_limit_seconds" =~ ^[0-9]+$ ]] && [ "$window_limit_seconds" -le 1800 ] ||
  reject "MTP_REHEARSAL_WINDOW_LIMIT_SECONDS は 1800 以下の数字で指定してください"
wait_seconds="${MTP_REHEARSAL_WAIT_SECONDS:-60}"
[[ "$wait_seconds" =~ ^[0-9]+$ ]] || reject "MTP_REHEARSAL_WAIT_SECONDS は数字で指定してください"

# この実行だけの一時directory。復号したdump・nonce・snapshotを置く。終了時に必ず消す
work="$(mktemp -d)"
chmod 700 -- "$work"
mkdir -p "$work/state"
compose_started=0
compose_file=""

cleanup() {
  local status=$?
  trap - EXIT
  if [ "$compose_started" = 1 ] && [ -n "$compose_file" ]; then
    # 止めるだけ。容器・volume・復元したDBは残す
    docker compose -p mytechpulse-rehearsal -f "$compose_file" --profile serve --profile legacy --profile migrate \
      stop >/dev/null 2>&1 || true
  fi
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

state_set() { printf '%s' "$2" >"$work/state/$1"; }
state_get() { cat "$work/state/$1"; }

# run_step NAME FUNCTION : 工程を別shellで実行し、名前・秒数・成否だけを出す。
# 失敗したら後続へ進まない。起動前の入力確認（inputs・release-verify・previous-release）の拒否だけは2、
# それ以外の失敗は1で終わる
run_step() {
  local name="$1" function_name="$2" started status
  started=$SECONDS
  set +e
  (
    set -e
    "$function_name"
  )
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    printf 'rehearsal: %s %ss ok\n' "$name" "$((SECONDS - started))"
  else
    printf 'rehearsal: %s %ss failed\n' "$name" "$((SECONDS - started))" >&2
    case "$name" in
      inputs | release-verify | previous-release) [ "$status" -eq 2 ] && exit 2 ;;
    esac
    exit 1
  fi
}

quiet() {
  "$@" >/dev/null 2>&1
}

dc() {
  docker compose -p mytechpulse-rehearsal -f "$(state_get compose_file)" "$@"
}

psql_db() {
  dc exec -T db psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "$(state_get db_name)" "$@"
}

wait_http() {
  # wait_http URL [curlの追加引数...]
  local url="$1" tries
  shift
  for ((tries = 0; tries <= wait_seconds; tries++)); do
    if curl -fsS -o /dev/null --max-time 3 "$@" "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# ---- 1. 入力の確認（起動前に失敗させる） ----
step_inputs() {
  [ -f "$dump" ] && [ ! -L "$dump" ] || reject "dumpのfileがありません"
  [ -f "$dump.sha256" ] && [ ! -L "$dump.sha256" ] || reject "dumpのchecksum（.sha256）がありません"
  if [ "$synthetic" != 1 ]; then
    case "$dump" in
      *.gpg) ;;
      *) reject "本番由来のdumpは暗号化file（.gpg）だけ受け付けます" ;;
    esac
  fi
  (cd "$(dirname "$dump")" && sha256sum -c "$(basename "$dump").sha256" >/dev/null 2>&1) ||
    reject "dumpのchecksumが一致しません"
  [[ "$releases_root" =~ ^/[A-Za-z0-9._/-]+$ ]] || reject "MTP_RELEASES_ROOT は絶対pathで指定してください"
}

# ---- 2. manifestとarchiveの検証、展開 ----
step_release() {
  local output line release_dir="" api_image=""
  output="$(bash "$here/verify_release.sh" "$archive_dir")" ||
    { local status=$?; [ "$status" -eq 2 ] && exit 2; exit 1; }
  while IFS= read -r line; do
    case "$line" in
      MTP_RELEASE_DIR=*) release_dir="${line#MTP_RELEASE_DIR=}" ;;
      MTP_FRONTEND_ARCHIVE=*) state_set frontend_archive "${line#MTP_FRONTEND_ARCHIVE=}" ;;
      GO_API_IMAGE=*) api_image="${line#GO_API_IMAGE=}" ;;
    esac
  done <<<"$output"
  [ -n "$release_dir" ] && [ -n "$api_image" ] || exit 1
  # 検証済みの運用一式だけを使う。checkoutの同名fileへは切り替えない
  local required
  for required in docker-compose.rehearsal.yml ops/snapshot_migration_state.sh ops/compare_migration_state.sh \
    ops/rehearsal_smoke.sh ops/sql/rehearsal_cleanup.sql ops/sql/verify_restored_db.sql; do
    [ -f "$release_dir/$required" ] || reject "検証済みの運用一式に $required がありません"
  done
  mkdir "$release_dir/frontend-site"
  tar -xzf "$(state_get frontend_archive)" -C "$release_dir/frontend-site" --no-same-owner --no-same-permissions
  state_set release_dir "$release_dir"
  state_set api_image "$api_image"
  state_set compose_file "$release_dir/docker-compose.rehearsal.yml"
  state_set frontend_dir "$release_dir/frontend-site"
}

# ---- 3. 切り戻し先の確認（旧API・旧ops・旧frontendの記録が揃っていること） ----
step_previous() {
  # shellcheck source=ops/lib/previous_release_record.sh
  source "$here/lib/previous_release_record.sh"
  parse_previous_release_record "$previous_record"
  [ -f "$PREV_OPS_DIR/docker-compose.yml" ] || reject "直前のreleaseの運用一式に docker-compose.yml がありません"
  quiet docker image inspect "$PREV_API_IMAGE" ||
    reject "直前のreleaseのAPI imageがこのPCにありません（切り戻しの模擬ができません）"
  state_set previous_image "$PREV_API_IMAGE"
}

# 以降のdocker composeが読む値。API imageはmanifest由来だけ
load_environment() {
  export MTP_REHEARSAL_IMAGE MTP_REHEARSAL_DB_PASSWORD MTP_REHEARSAL_FRONTEND_DIR MTP_REHEARSAL_DB_NAME
  export MTP_REHEARSAL_LEGACY_IMAGE
  MTP_REHEARSAL_IMAGE="$(state_get api_image)"
  MTP_REHEARSAL_DB_PASSWORD="$db_password"
  MTP_REHEARSAL_FRONTEND_DIR="$(state_get frontend_dir)"
  MTP_REHEARSAL_DB_NAME="$(state_get db_name)"
  MTP_REHEARSAL_LEGACY_IMAGE="$(state_get previous_image)"
}

step_pull() {
  load_environment
  quiet dc --profile serve --profile migrate pull api migrate
}

# ---- 4. 暗号化dumpの復号（復号した平文は、この実行の一時directoryだけに置く） ----
step_decrypt() {
  case "$dump" in
    *.gpg)
      mkdir -m 700 "$work/dump"
      # パスワードはgpgのプロンプトへ人間が入力する。引数・環境変数・fileには出さない
      gpg --quiet --decrypt --output "$work/dump/restore.dump" "$dump" || exit 1
      [ "$(wc -c <"$work/dump/restore.dump")" -ge 100 ] || exit 1
      state_set dump_file "$work/dump/restore.dump"
      ;;
    *)
      state_set dump_file "$dump"
      ;;
  esac
}

# ---- 5. 隔離DBの起動、dumpの検査、新しいDBへの復元 ----
step_db_start() {
  load_environment
  quiet dc up -d --wait db
}

step_backup_verify() {
  load_environment
  quiet dc exec -T db pg_restore --list <"$(state_get dump_file)"
}

step_restore() {
  load_environment
  local db
  db="$(state_get db_name)"
  # 既にあるDBへは復元しない（前の実行の結果を上書きしない）
  [ -z "$(dc exec -T db psql -X -q -At -U postgres -d postgres -c "SELECT 1 FROM pg_database WHERE datname = '$db'" 2>/dev/null)" ] || exit 1
  quiet dc exec -T db createdb -U postgres "$db"
  quiet dc exec -T db pg_restore -U postgres -d "$db" --exit-on-error <"$(state_get dump_file)"
  quiet dc exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$db" <"$(state_get release_dir)/ops/sql/verify_restored_db.sql"
}

# snapshot・比較は、検証済みの運用一式のscriptを使う
run_snapshot() {
  # run_snapshot NAME
  load_environment
  MTP_SNAPSHOT_DB="$(state_get db_name)" \
    MTP_SNAPSHOT_NONCE_FILE="$work/nonce" \
    MTP_SNAPSHOT_OUTPUT="$work/$1.json" \
    MTP_SNAPSHOT_COMPOSE_ARGS="-p mytechpulse-rehearsal -f $(state_get compose_file)" \
    bash "$(state_get release_dir)/ops/snapshot_migration_state.sh"
}

# run_compare BEFORE AFTER [FLAG] : 終了コードをそのまま返す（0=一致 1=不一致 2=検査不能）
run_compare() {
  load_environment
  MTP_SNAPSHOT_DB="$(state_get db_name)" \
    MTP_SNAPSHOT_COMPOSE_ARGS="-p mytechpulse-rehearsal -f $(state_get compose_file)" \
    bash "$(state_get release_dir)/ops/compare_migration_state.sh" "$work/$1.json" "$work/$2.json" ${3:+"$3"}
}

step_snapshot_before() { run_snapshot before; }

step_migrate() {
  load_environment
  quiet dc --profile migrate run --rm migrate
}

step_snapshot_after() { run_snapshot after; }

# 一般利用者の書き込みを止めたまま（まだAPIを起動していない）、移行前後の内容を比べる
step_compare() { run_compare before after; }

# ---- 6. Go版の起動とsmoke ----
step_serve() {
  load_environment
  # 合成writeの前にtagの最大IDを控える（後始末で、smokeが増やしたtagだけを消すため）
  state_set tag_max "$(psql_db -c 'SELECT coalesce(max("tag_ID"), 0) FROM tag')"
  quiet dc --profile serve up -d --wait api caddy
  wait_http http://127.0.0.1:18001/health/ready
  # 隔離Caddy経由でも、frontendとAPI中継が使えること
  wait_http https://localhost:18443/health/ready -k --resolve localhost:18443:127.0.0.1
  wait_http https://localhost:18443/ -k --resolve localhost:18443:127.0.0.1
}

step_smoke() {
  load_environment
  MTP_REHEARSAL_SMOKE_USERNAME="$(state_get smoke_user)" \
    bash "$(state_get release_dir)/ops/rehearsal_smoke.sh"
}

# ---- 7. 合成writeによる差分と、後始末後の一致 ----
step_expected_diff() {
  run_snapshot after-write
  local status=0
  run_compare before after-write >/dev/null 2>&1 || status=$?
  # smokeが書いたのだから、差分が出るのが正しい。一致していたら、書き込みが検査に映っていない
  [ "$status" -eq 1 ] || exit 1
}

step_cleanup() {
  load_environment
  quiet dc exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$(state_get db_name)" \
    -v "smoke_user=$(state_get smoke_user)" -v "tag_max=$(state_get tag_max)" \
    <"$(state_get release_dir)/ops/sql/rehearsal_cleanup.sql"
  run_snapshot after-cleanup
  # 採番だけは進んでよい（戻ってはいけない）。3表の内容は一致していること
  run_compare before after-cleanup --allow-sequence-advance
}

# ---- 8. 旧releaseへ戻す模擬と、Go版への再切替 ----
step_rollback() {
  load_environment
  quiet dc --profile serve stop api caddy
  quiet dc --profile legacy up -d --wait api-legacy
  wait_http http://127.0.0.1:18000/
}

step_switch_back() {
  load_environment
  quiet dc --profile legacy stop api-legacy
  quiet dc --profile serve up -d --wait api caddy
  wait_http http://127.0.0.1:18001/health/ready
  wait_http https://localhost:18443/health/ready -k --resolve localhost:18443:127.0.0.1
}

# 利用者名と復元先DB名は、この実行だけの合成の名前
state_set smoke_user "rehearsal-smoke-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
state_set db_name "mtp_rehearsal_$(date -u +%Y%m%d%H%M%S)_$(head -c 3 /dev/urandom | od -An -tx1 | tr -d ' \n')"

total_started=$SECONDS
run_step inputs step_inputs
run_step release-verify step_release
run_step previous-release step_previous
# 以降のdocker composeと、終了時のstopが読む値は、ここで確定させる
compose_file="$(state_get compose_file)"
load_environment
run_step image-pull step_pull
run_step decrypt step_decrypt
compose_started=1
run_step db-start step_db_start
run_step backup-verify step_backup_verify
run_step restore step_restore

window_started=$SECONDS
run_step snapshot-before step_snapshot_before
run_step migrate step_migrate
run_step snapshot-after step_snapshot_after
run_step compare step_compare
run_step serve step_serve
run_step smoke step_smoke
window_seconds=$((SECONDS - window_started))

run_step expected-diff step_expected_diff
run_step cleanup-compare step_cleanup

rollback_started=$SECONDS
run_step rollback step_rollback
run_step switch-back step_switch_back
rollback_seconds=$((SECONDS - rollback_started))

printf 'rehearsal: stop-equivalent %ss (limit %ss), rollback %ss, total %ss\n' \
  "$window_seconds" "$window_limit_seconds" "$rollback_seconds" "$((SECONDS - total_started))"
if [ "$window_seconds" -gt "$window_limit_seconds" ]; then
  echo "rehearsal: failed (停止相当の工程が30分を超えた)" >&2
  exit 1
fi
printf 'rehearsal: ok\n'
