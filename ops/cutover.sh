#!/usr/bin/env bash
# 本番をPython版からGo版へ切り替える（#127）。本番サーバー上で、人間が1段階ずつ実行する。
#
#   使い方: MTP_RELEASE_DIR=<deploy_release.sh が準備したrelease directory> \
#           MTP_ENV_FILE=<本番の設定ファイル（0600・Git管理の外）> \
#           bash "$MTP_RELEASE_DIR/ops/cutover.sh" <段階>
#   このscriptは、検証済みのrelease directoryの中のもの（$MTP_RELEASE_DIR/ops/cutover.sh）だけを使う。
#
# 段階（この順番に1つずつ実行する。前の段階が成功していないと次は実行できない）
#   preflight        読み取りだけの事前確認。何も変えない（何度でも実行できる）
#   maintenance-on   Caddyをメンテナンス応答（503）にし、Python版APIを止める。ここから停止時間が始まる
#   backup           最終バックアップを取り、チェックサムと読み取りを確認。件数を控える
#   snapshot-before  移行前の状態を記録する（内容は表に出さない）
#   migrate          Go版のDBマイグレーションを1回実行する（追加のみ。既存の3表には触れない）
#   compare          移行後の状態を記録し、移行前と比べる。一致しなければ止まる
#   go-start         Go版APIを起動し、稼働確認する（まだ公開はしない）
#                    → この後、新しい画面をCloudflare Pagesへ公開する（手動）
#   switch           CaddyをGo版へ向ける。ここで停止が終わる。MTP_CUTOVER_CONFIRM_FRONTEND=yes が要る
#   smoke            本番のホスト名で、登録→ログイン→記事一覧→クリック→ログアウトを確認する
#   smoke-cleanup    smokeが作った合成利用者と関連行だけを消す
#   finish           記録用の一時ファイル（nonce・snapshot）を消し、完了を表示する
#   rollback         いつでも実行できる。Python版へ戻す（DBは巻き戻さない）
#   status           今の状態（Caddyの向け先、動いているサービス）を表示する。何も変えない
#
# 入力（すべて任意。ふつうは何も指定しない）
#   MTP_RELEASE_DIR           release directory。指定する場合は、このscriptの場所と同じであること（既定: このscriptの場所）
#   MTP_ENV_FILE              Go版の設定ファイル（既定: ~/mytechpulse-production.env）。このscriptは項目の有無と、
#                             一部の値の確認だけをし、中身は出さない
#                             必須項目: POSTGRES_PASSWORD, API_DOMAIN, APP_ENV=production, QIITA_ACCESS_TOKEN,
#                             CORS_ALLOWED_ORIGINS, SWAGGER_ENABLED=false
#   MTP_CUTOVER_ORIGIN        smokeで使う、本番の画面のオリジン。CORS_ALLOWED_ORIGINS に含まれること
#                             （既定: CORS_ALLOWED_ORIGINS の最初のもの）
#   MTP_CUTOVER_CONFIRM_FRONTEND  switchで必須。新しい画面を公開してから yes を指定する
#                             （part2は、未指定ならその場で「yes」の入力を求める）
#   MTP_COMPOSE_PROJECT       既定 mytechpulse（本番のproject名）
#   試験用: MTP_COMPOSE_EXTRA_FILES, MTP_CUTOVER_BASE_URL, MTP_CUTOVER_GO_URL, MTP_CUTOVER_PY_URL, MTP_CUTOVER_WAIT_SECONDS
#
# 出力は、段階の名前・秒数・成功/失敗と、固定の文、件数だけ。設定ファイルの値・パスワード・バックアップの中身は出さない。
# 一度完了した段階は再実行できない。失敗した段階は、原因を直して再実行できる。
# 終了コード: 0=成功、1=段階が失敗、2=入力の拒否（順番違い・設定不備）
set -euo pipefail
set +x
umask 077

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

reject() {
  echo "拒否: $*" >&2
  exit 2
}

[ "$#" -eq 1 ] || reject "使い方: cutover.sh <part1 | part2 | 段階>"
stage="$1"
case "$stage" in
  part1 | part2 | preflight | status | maintenance-on | backup | snapshot-before | migrate | compare | go-start | switch | smoke | smoke-cleanup | finish | rollback) ;;
  *) reject "不明な段階です: $stage" ;;
esac

release_dir="${MTP_RELEASE_DIR:-$(dirname "$here")}"
env_file="${MTP_ENV_FILE:-$HOME/mytechpulse-production.env}"
project="${MTP_COMPOSE_PROJECT:-mytechpulse}"
wait_seconds="${MTP_CUTOVER_WAIT_SECONDS:-60}"

release_dir="${release_dir%/}"
[[ "$release_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -d "$release_dir" ] || reject "MTP_RELEASE_DIR（絶対path）のdirectoryがありません"
[ "$here" = "$release_dir/ops" ] || reject "検証済みのrelease directoryの ops/cutover.sh から実行してください（checkoutの同名fileは使いません）"
[[ "$env_file" =~ ^/[A-Za-z0-9._/-]+$ ]] && [ -f "$env_file" ] && [ ! -L "$env_file" ] || reject "MTP_ENV_FILE（絶対path）のファイルがありません"
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || reject "MTP_COMPOSE_PROJECT の形式が正しくありません"
[[ "$wait_seconds" =~ ^[0-9]+$ ]] || reject "MTP_CUTOVER_WAIT_SECONDS は数字で指定してください"

# ---- release directory の入力 ----
[ -f "$release_dir/release.env" ] || reject "release directory に release.env がありません"
GO_API_IMAGE=""
while IFS= read -r line; do
  case "$line" in
    GO_API_IMAGE=*) GO_API_IMAGE="${line#GO_API_IMAGE=}" ;;
  esac
done <"$release_dir/release.env"
[[ "$GO_API_IMAGE" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] ||
  reject "release.env にmanifest由来の完全digestがありません"
export GO_API_IMAGE

# shellcheck source=ops/lib/previous_release_record.sh
source "$here/lib/previous_release_record.sh"
parse_previous_release_record "$release_dir/previous-release.json"
legacy_dir="$PREV_OPS_DIR"

# 試験用: composeへ追加で重ねるファイル（既定は空。本番では使わない）
extra_args=()
if [ -n "${MTP_COMPOSE_EXTRA_FILES:-}" ]; then
  for extra_file in $MTP_COMPOSE_EXTRA_FILES; do
    extra_args+=(-f "$extra_file")
  done
fi

state_dir="$release_dir/cutover-state"

# 設定ファイルの値（API_DOMAINなど）を読む。値は出力へ出さない
env_value() {
  local line
  line="$(grep -m1 "^$1=" "$env_file" || true)"
  printf '%s' "${line#*=}"
}

dc() {
  docker compose --env-file "$env_file" -p "$project" -f "$release_dir/docker-compose.yml" ${extra_args[@]+"${extra_args[@]}"} "$@"
}

# 旧版（切り戻し先）の運用ファイルで動かす。旧版のcomposeは、その directory の設定ファイルを自動で読む
legacy_dc() {
  if [ "${#extra_args[@]}" -gt 0 ]; then
    # 試験用に重ねるファイルがあるときは、旧版のcomposeも明示して重ねる
    (cd "$legacy_dir" && COMPOSE_PROJECT_NAME="$project" docker compose -f "$legacy_dir/docker-compose.yml" "${extra_args[@]}" "$@")
  else
    (cd "$legacy_dir" && COMPOSE_PROJECT_NAME="$project" docker compose "$@")
  fi
}

api_domain="$(env_value API_DOMAIN)"
base_url="${MTP_CUTOVER_BASE_URL:-https://${api_domain}}"
go_url="${MTP_CUTOVER_GO_URL:-http://127.0.0.1:8001}"
py_url="${MTP_CUTOVER_PY_URL:-http://127.0.0.1:8000}"

# ---- 状態（段階の完了の記録） ----
prepare_state() {
  if [ ! -d "$state_dir" ]; then
    mkdir -m 700 "$state_dir"
  fi
}
done_marker() { printf '%s/done.%s' "$state_dir" "$1"; }
is_done() { [ -f "$(done_marker "$1")" ]; }
mark_done() { : >"$(done_marker "$1")"; }
state_set() { printf '%s' "$2" >"$state_dir/$1"; }
state_get() { cat "$state_dir/$1"; }

require_done() {
  is_done "$1" || reject "先に「$1」を成功させてください（段階の順番を守ります）"
}
require_not_done() {
  ! is_done "$1" || reject "「$1」は既に完了しています（再実行できません）"
}

fail_stage() {
  echo "cutover: $stage failed ($1)" >&2
  exit 1
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

db_counts() {
  dc exec -T db psql -X -q -At -U postgres -d mytechpulse \
    -c "SELECT (SELECT count(*) FROM \"user\") || ',' || (SELECT count(*) FROM tag) || ',' || (SELECT count(*) FROM recommend)" |
    tr -d '\r'
}

caddy_config_label() {
  local container source
  container="$(dc --profile prod ps -q caddy 2>/dev/null | head -1 || true)"
  [ -n "$container" ] || { echo "none"; return 0; }
  source="$(docker inspect "$container" \
    --format '{{range .Mounts}}{{if eq .Destination "/etc/caddy/Caddyfile"}}{{.Source}}{{end}}{{end}}' 2>/dev/null | tr '\\' '/')"
  case "$source" in
    */Caddyfile.maintenance) echo "maintenance" ;;
    */Caddyfile.go) echo "go" ;;
    */Caddyfile) echo "python" ;;
    *) echo "unknown" ;;
  esac
}

running_services() {
  dc --profile prod --profile go-preview ps --status running --services 2>/dev/null | tr -d '\r' | sort | tr '\n' ' '
}

set_caddy() {
  # set_caddy maintenance|go : リリースのcomposeで、使う設定を選んでCaddyだけ作り直す（他のサービスは起動しない）
  case "$1" in
    maintenance) MTP_CADDYFILE=ops/caddy/Caddyfile.maintenance dc --profile prod up -d --no-deps caddy >/dev/null 2>&1 ;;
    go) MTP_CADDYFILE=ops/caddy/Caddyfile.go dc --profile prod up -d --no-deps caddy >/dev/null 2>&1 ;;
  esac
}

# ---- 段階 ----

stage_preflight() {
  local key value
  [ -f "$release_dir/docker-compose.yml" ] && grep -q 'MTP_CADDYFILE' "$release_dir/docker-compose.yml" ||
    fail_stage "composeが設定の切り替えに対応していない"
  grep -q 'migrate-go' "$release_dir/docker-compose.yml" || fail_stage "composeにGo版のmigrationが無い"
  for key in ops/caddy/Caddyfile.maintenance ops/caddy/Caddyfile.go ops/snapshot_migration_state.sh \
    ops/compare_migration_state.sh ops/rehearsal_smoke.sh ops/sql/rehearsal_cleanup.sql; do
    [ -f "$release_dir/$key" ] || fail_stage "release directory に $key が無い"
  done

  # 切り戻し先（旧版）が揃っている
  for key in docker-compose.yml Caddyfile ops/backup_db.sh ops/verify_backup.sh; do
    [ -f "$legacy_dir/$key" ] || fail_stage "切り戻し先の運用ファイル（$key）が無い"
  done

  # 設定ファイル: 項目の有無と、必要な値だけを確認する（値は出さない）
  for key in POSTGRES_PASSWORD API_DOMAIN QIITA_ACCESS_TOKEN CORS_ALLOWED_ORIGINS; do
    [ -n "$(env_value "$key")" ] || fail_stage "設定ファイルに $key が無い、または空"
  done
  [ "$(env_value APP_ENV)" = "production" ] || fail_stage "設定ファイルの APP_ENV が production でない"
  [ "$(env_value SWAGGER_ENABLED)" = "false" ] || fail_stage "設定ファイルの SWAGGER_ENABLED が false でない"
  [[ "$api_domain" =~ ^[A-Za-z0-9.-]+$ ]] || fail_stage "API_DOMAIN の形式が正しくない"
  if modes_enforceable; then
    [ "$(stat -c '%a' "$env_file")" = "600" ] || fail_stage "設定ファイルの権限が 600 でない"
  fi

  # composeが展開できる（中身は出さない）
  dc --profile prod --profile go-preview --profile go-migrate config >/dev/null 2>&1 || fail_stage "composeを展開できない"

  # Go版の箱がこのサーバーにある（deploy_release.sh がpull済み）
  docker image inspect "$GO_API_IMAGE" >/dev/null 2>&1 || fail_stage "Go版の箱（image）がサーバーに無い"

  # 今はPython版が動いている（db・api・caddy）。Go版はまだ動いていない
  local running
  running="$(running_services)"
  for key in db api caddy; do
    case " $running" in
      *" $key "*) ;;
      *) fail_stage "$key が動いていない" ;;
    esac
  done
  case " $running" in
    *" api-go "*) fail_stage "Go版が既に動いている" ;;
  esac
  [ "$(caddy_config_label)" = "python" ] || fail_stage "Caddyの向け先がPython版でない"

  # 設定ファイルのパスワードが、いま動いているDBのパスワードと一致する（Go版はこの値でDBへ繋ぐ）。値は出さない。
  # 127.0.0.1 はDBの設定で認証なしで通るため使わず、サービス名（db）で繋いで、パスワードを実際に使わせる
  POSTGRES_PASSWORD="$(env_value POSTGRES_PASSWORD)" dc exec -T -e POSTGRES_PASSWORD db sh -c     'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -At -h db -U postgres -d mytechpulse -c "SELECT 1"' >/dev/null 2>&1 ||
    fail_stage "設定ファイルのDBパスワードでDBへ繋がらない"

  # 空き容量（バックアップのため）。1GB以上
  local free_kb
  free_kb="$(df -Pk "$release_dir" | awk 'NR==2 {print $4}')"
  [ "${free_kb:-0}" -ge 1048576 ] || fail_stage "空き容量が1GB未満"

  state_set preflight_at "$(date +%s)"
  mark_done preflight
}

modes_enforceable() {
  local probe result
  probe="$(mktemp "$state_dir/mode-probe.XXXXXX")" || return 1
  chmod 600 -- "$probe"
  result="$(stat -c '%a' -- "$probe")"
  rm -f -- "$probe"
  [ "$result" = "600" ]
}

stage_status() {
  echo "cutover: caddy=$(caddy_config_label) services=$(running_services)"
}

stage_maintenance_on() {
  set_caddy maintenance || fail_stage "Caddyをメンテナンスにできない"
  wait_status "$base_url/" 503 || fail_stage "メンテナンス応答（503）にならない"
  # Python版を止める（書き込みの元を断つ）。止めるだけで、containerやデータは消さない
  dc stop api >/dev/null 2>&1 || fail_stage "Python版APIを止められない"
  state_set maintenance_at "$(date +%s)"
}

stage_backup() {
  local dump
  dump="$(cd "$legacy_dir" && COMPOSE_PROJECT_NAME="$project" ./ops/backup_db.sh | tail -1)" || fail_stage "バックアップを取れない"
  [ -n "$dump" ] || fail_stage "バックアップのpathが分からない"
  (cd "$legacy_dir" && COMPOSE_PROJECT_NAME="$project" ./ops/verify_backup.sh "$dump" >/dev/null 2>&1) ||
    fail_stage "バックアップの検証に失敗した"
  state_set backup_file "$(basename "$dump")"
  local counts
  counts="$(db_counts)" || fail_stage "件数を取れない"
  state_set counts_before "$counts"
  echo "cutover: backup $(basename "$dump")"
  echo "cutover: counts(user,tag,recommend) $(state_get counts_before)"
}

run_snapshot() {
  # run_snapshot NAME
  MTP_SNAPSHOT_DB=mytechpulse \
    MTP_SNAPSHOT_NONCE_FILE="$state_dir/nonce" \
    MTP_SNAPSHOT_OUTPUT="$state_dir/$1.json" \
    MTP_SNAPSHOT_COMPOSE_ARGS="--env-file $env_file -p $project -f $release_dir/docker-compose.yml" \
    bash "$release_dir/ops/snapshot_migration_state.sh"
}

stage_snapshot_before() {
  run_snapshot before || fail_stage "移行前の記録に失敗した"
}

stage_migrate() {
  dc --profile go-migrate run --rm migrate-go >/dev/null 2>&1 || fail_stage "migrationに失敗した"
}

stage_compare() {
  run_snapshot after || fail_stage "移行後の記録に失敗した"
  local status=0
  MTP_SNAPSHOT_DB=mytechpulse \
    MTP_SNAPSHOT_COMPOSE_ARGS="--env-file $env_file -p $project -f $release_dir/docker-compose.yml" \
    bash "$release_dir/ops/compare_migration_state.sh" "$state_dir/before.json" "$state_dir/after.json" || status=$?
  [ "$status" -eq 0 ] || fail_stage "移行前後の内容が一致しない、または比較できない。rollback を検討してください"
  local after
  after="$(db_counts)" || fail_stage "件数を取れない"
  [ "$after" = "$(state_get counts_before)" ] || fail_stage "移行前後で件数が違う。rollback を検討してください"
  echo "cutover: counts(user,tag,recommend) $after"
}

stage_go_start() {
  dc --profile go-preview up -d --no-deps api-go >/dev/null 2>&1 || fail_stage "Go版を起動できない"
  wait_status "$go_url/health/live" 200 || fail_stage "Go版が起動しない（live）"
  wait_status "$go_url/health/ready" 200 || fail_stage "Go版の準備ができない（ready）"
}

stage_switch() {
  [ "${MTP_CUTOVER_CONFIRM_FRONTEND:-}" = "yes" ] ||
    reject "新しい画面を公開してから、MTP_CUTOVER_CONFIRM_FRONTEND=yes を付けて実行してください"
  set_caddy go || fail_stage "CaddyをGo版へ向けられない"
  wait_status "$base_url/health/ready" 200 || fail_stage "本番のホスト名でGo版に届かない。rollback を検討してください"
  local started elapsed
  started="$(state_get maintenance_at)"
  elapsed=$(($(date +%s) - started))
  echo "cutover: stop-time ${elapsed}s (limit 1800s)"
  if [ "$elapsed" -gt 1800 ]; then
    echo "cutover: warning 停止時間が30分を超えた" >&2
  fi
}

stage_smoke() {
  # 指定が無いときだけ、設定ファイルの最初のオリジンを使う（空で指定したときは拒否する）
  local origin
  if [ -z "${MTP_CUTOVER_ORIGIN+x}" ]; then
    origin="$(env_value CORS_ALLOWED_ORIGINS)"
    origin="${origin%%,*}"
  else
    origin="$MTP_CUTOVER_ORIGIN"
  fi
  [[ "$origin" =~ ^https://[A-Za-z0-9.-]+$ ]] || reject "画面のオリジン（https://画面のホスト名）が正しくありません"
  case ",$(env_value CORS_ALLOWED_ORIGINS)," in
    *",$origin,"*) ;;
    *) reject "MTP_CUTOVER_ORIGIN が CORS_ALLOWED_ORIGINS に含まれていません" ;;
  esac
  # 片付けの対象を絞るため、書き込む前に、合成利用者名とtagの最大IDを控える
  local user tag_max
  user="rehearsal-smoke-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  tag_max="$(dc exec -T db psql -X -q -At -U postgres -d mytechpulse -c 'SELECT coalesce(max("tag_ID"), 0) FROM tag' | tr -d '\r')"
  state_set smoke_user "$user"
  state_set tag_max "$tag_max"
  MTP_REHEARSAL_BASE_URL="$base_url" MTP_REHEARSAL_ORIGIN="$origin" MTP_REHEARSAL_SMOKE_USERNAME="$user" \
    bash "$release_dir/ops/rehearsal_smoke.sh" || fail_stage "動作確認に失敗した。rollback を検討してください（先に smoke-cleanup も実行できます）"
}

stage_smoke_cleanup() {
  [ -f "$state_dir/smoke_user" ] || reject "smoke を実行していません（片付ける対象がありません）"
  local user
  user="$(state_get smoke_user)"
  dc exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d mytechpulse \
    -v "smoke_user=$user" -v "tag_max=$(state_get tag_max)" <"$release_dir/ops/sql/rehearsal_cleanup.sql" >/dev/null 2>&1 ||
    fail_stage "片付けに失敗した（合成利用者がいない、または条件に合わない）"
  local remaining
  remaining="$(dc exec -T db psql -X -q -At -U postgres -d mytechpulse \
    -c "SELECT count(*) FROM \"user\" WHERE user_name = '$user'" | tr -d '\r')"
  [ "$remaining" = "0" ] || fail_stage "合成利用者が残っている"
}

stage_finish() {
  # この実行で作った、記録用の一時ファイル（nonce・snapshot）だけを消す。バックアップは消さない
  rm -f -- "$state_dir/nonce" "$state_dir/before.json" "$state_dir/after.json"
  echo "cutover: backup file kept: $(state_get backup_file)"
  echo "cutover: ok（監視期間へ。記録を #127 へ）"
}

stage_rollback() {
  local failed=0
  step() {
    # step NAME COMMAND...
    local name="$1"
    shift
    if "$@" >/dev/null 2>&1; then
      echo "cutover: rollback $name ok"
    else
      echo "cutover: rollback $name failed" >&2
      failed=1
    fi
  }
  step "caddy-maintenance" set_caddy maintenance
  step "stop-go" dc --profile go-preview stop api-go
  step "start-python" legacy_dc up -d --no-build --no-deps api
  step "python-health" wait_status "$py_url/" 200
  step "caddy-python" legacy_dc --profile prod up -d --no-deps caddy
  step "public-health" wait_status "$base_url/" 200
  # 状態は消さず、名前を変えて残す（次の試行を、新しい状態で始められる）
  if [ -d "$state_dir" ]; then
    mv -- "$state_dir" "$state_dir.rolledback-$(date +%Y%m%d%H%M%S)" || failed=1
  fi
  if [ "$failed" -ne 0 ]; then
    echo "cutover: rollback 一部が失敗しました。status で状態を確認し、手順書の切り戻しを続けてください" >&2
    exit 1
  fi
  echo "cutover: rollback ok（データベースは巻き戻していません）"
  if [ "$PREV_FRONTEND_DEPLOYMENT_ID" = "see-cloudflare-pages-deployments" ]; then
    # cutover_prepare.sh が作る記録には、画面の公開の識別子が入らない（Cloudflare側で控えておく）
    echo "cutover: 画面を公開した後の場合は、自分のPCで画面も戻してください: gh workflow run cutover-frontend.yml -f action=rollback"
    echo "cutover: （うまくいかなければ、Cloudflare Pagesの管理画面のDeploymentsで、切り替え前の公開へ戻します）"
  else
    echo "cutover: 画面（Cloudflare Pages）を、直前の公開（ID: ${PREV_FRONTEND_DEPLOYMENT_ID}）へ戻してください"
  fi
}

run_stage() {
  local function_name="$1" started status
  started=$SECONDS
  set +e
  (
    set -e
    "$function_name"
  )
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    printf 'cutover: %s %ss ok\n' "$stage" "$((SECONDS - started))"
  else
    printf 'cutover: %s %ss failed\n' "$stage" "$((SECONDS - started))" >&2
    exit "$status"
  fi
}

# ---- まとめて実行する（part1・part2）。中で、同じscriptの段階を1つずつ呼ぶ ----

self="$here/cutover.sh"

auto_rollback() {
  # auto_rollback 失敗した段階 : 確認なしで切り戻す。DBは巻き戻さない
  echo "cutover: $1 が失敗したため、自動で切り戻します" >&2
  bash "$self" rollback || exit 1
}

confirm_frontend() {
  [ "${MTP_CUTOVER_CONFIRM_FRONTEND:-}" = "yes" ] && return 0
  [ -t 0 ] || reject "新しい画面を公開してから、MTP_CUTOVER_CONFIRM_FRONTEND=yes を付けて実行してください"
  local answer=""
  printf '新しい画面（Cloudflare Pages）を公開しましたか？ 公開済みなら yes と入力してください: '
  read -r answer || true
  [ "$answer" = "yes" ] || reject "新しい画面を公開してから、もう一度実行してください"
  export MTP_CUTOVER_CONFIRM_FRONTEND=yes
}

part1() {
  [ -n "${TMUX:-}${STY:-}" ] ||
    echo "cutover: 注意 tmux（または screen）の中で実行すると、接続が切れても作業が止まりません" >&2
  trap 'echo "cutover: 中断しました。状態は cutover.sh status、戻すときは cutover.sh rollback" >&2; exit 130' INT TERM
  # 事前確認は毎回やり直す。ここで失敗したときは、まだ何も変えていないので、切り戻さない
  bash "$self" preflight || exit $?
  local s status
  for s in maintenance-on backup snapshot-before migrate compare go-start; do
    status=0
    bash "$self" "$s" || status=$?
    if [ "$status" -eq 1 ]; then
      auto_rollback "$s"
      exit 1
    elif [ "$status" -ne 0 ]; then
      # 入力の拒否（2）は、その段階が何も実行していない。自動では戻さず、状態の確認を求める
      echo "cutover: $s を実行できませんでした。status で状態を確認してください" >&2
      exit "$status"
    fi
  done
  echo "cutover: part1 ok（Go版は起動済み・まだ公開していません）"
  echo "cutover: 次: 新しい画面をCloudflare Pagesへ公開する → cutover.sh part2"
}

part2() {
  require_done go-start
  confirm_frontend
  trap 'echo "cutover: 中断しました。状態は cutover.sh status、戻すときは cutover.sh rollback" >&2; exit 130' INT TERM
  local s status
  for s in switch smoke; do
    status=0
    bash "$self" "$s" || status=$?
    if [ "$status" -eq 1 ]; then
      # 動作確認が作った合成利用者は、切り戻す前に片付ける（失敗しても切り戻しは続ける）
      if [ "$s" = "smoke" ]; then
        bash "$self" smoke-cleanup || echo "cutover: 合成利用者の片付けに失敗しました。切り戻しは続けます" >&2
      fi
      auto_rollback "$s"
      exit 1
    elif [ "$status" -ne 0 ]; then
      echo "cutover: $s を実行できませんでした。status で状態を確認してください" >&2
      exit "$status"
    fi
  done
  # 片付けに失敗しても、公開はGo版のまま。原因を調べるために止まる（切り戻さない）
  bash "$self" smoke-cleanup || {
    echo "cutover: 合成利用者の片付けに失敗しました。Go版は公開中です。原因を確認してください" >&2
    exit 1
  }
  bash "$self" finish
  echo "cutover: 最後に、ブラウザで、既存の利用者のログインと記事の表示、Qiitaの記事が出ることを確認してください"
}

prepare_state
case "$stage" in
  part1)
    part1
    ;;
  part2)
    part2
    ;;
  preflight)
    run_stage stage_preflight
    ;;
  status)
    stage_status
    ;;
  maintenance-on)
    require_done preflight
    require_not_done maintenance-on
    run_stage stage_maintenance_on
    mark_done maintenance-on
    ;;
  backup)
    require_done maintenance-on
    require_not_done backup
    run_stage stage_backup
    mark_done backup
    ;;
  snapshot-before)
    require_done backup
    require_not_done snapshot-before
    run_stage stage_snapshot_before
    mark_done snapshot-before
    ;;
  migrate)
    require_done snapshot-before
    require_not_done migrate
    run_stage stage_migrate
    mark_done migrate
    ;;
  compare)
    require_done migrate
    require_not_done compare
    run_stage stage_compare
    mark_done compare
    ;;
  go-start)
    require_done compare
    require_not_done go-start
    run_stage stage_go_start
    mark_done go-start
    ;;
  switch)
    require_done go-start
    require_not_done switch
    run_stage stage_switch
    mark_done switch
    ;;
  smoke)
    require_done switch
    require_not_done smoke
    run_stage stage_smoke
    mark_done smoke
    ;;
  smoke-cleanup)
    require_done switch
    require_not_done smoke-cleanup
    run_stage stage_smoke_cleanup
    mark_done smoke-cleanup
    ;;
  finish)
    require_done smoke
    require_done smoke-cleanup
    require_not_done finish
    run_stage stage_finish
    mark_done finish
    ;;
  rollback)
    run_stage stage_rollback
    ;;
esac
