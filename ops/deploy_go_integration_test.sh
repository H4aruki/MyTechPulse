#!/usr/bin/env bash
# ops/deploy_go.sh を、本物のDocker・PostgreSQL・Go版の箱で、本番と同じ構成を別の名前・別のportに作って通す（#125）。
# 本番・本物のドメイン・既存のproject/volumeには一切触れない。
#
# 通すもの
#   1. 新しい箱の入れ替えに失敗したとき（確認先に届かない状況を作る）、確認なしで直前の箱へ戻り、
#      戻った箱が応答すること、DBのデータが変わらないこと
#   2. 入れ替えに成功したとき、新しい箱が動き、応答すること、DBのデータが変わらないこと
# 通さないもの: 窓口（Caddy）・本物のドメインのHTTPS（公開側の疎通は、APIの待ち受けに直接つなぐ）
#
# 必要な環境変数
#   MTP_IT_OLD_IMAGE  いま動いているGo版の箱（ghcr.io/h4aruki/mytechpulse-api-go@sha256:... 。手元にあるもの）
#   MTP_IT_NEW_IMAGE  入れ替える先の箱（同上。OLDと別のdigest）
# 使い方: MTP_IT_OLD_IMAGE=... MTP_IT_NEW_IMAGE=... bash ops/deploy_go_integration_test.sh
# DBの保存先はtmpfs（保存しない）。終了時はcontainerを止めて消す（volumeは作らない）。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
old_image="${MTP_IT_OLD_IMAGE:-}"
new_image="${MTP_IT_NEW_IMAGE:-}"
[ -n "$old_image" ] && [ -n "$new_image" ] && [ "$old_image" != "$new_image" ] ||
  { echo "MTP_IT_OLD_IMAGE と MTP_IT_NEW_IMAGE（別のdigest）を指定してください" >&2; exit 2; }
for image in "$old_image" "$new_image"; do
  [[ "$image" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] || { echo "箱の指定の形式が正しくありません" >&2; exit 2; }
  docker image inspect "$image" >/dev/null 2>&1 || { echo "箱がありません: $image" >&2; exit 2; }
done

# shellcheck source=ops/tests/release_fixture.sh
source "$repo_root/ops/tests/release_fixture.sh"
export FX_API_IMAGE="$new_image"
fx_init

project="mtp-deploy-it"
work="$FX_TMP/it"
prev="$work/prev"
legacy="$work/legacy"
releases="$work/releases"
mkdir -p "$prev" "$legacy/ops" "$releases"
api_port=28011
api_url="http://127.0.0.1:${api_port}"

passed=0
fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}
ok() {
  passed=$((passed + 1))
  printf 'ok: %s\n' "$1"
}

password="it-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"
envf="$work/production.env"
cat >"$envf" <<EOF
POSTGRES_PASSWORD=${password}
API_DOMAIN=api.example.test
APP_ENV=test
QIITA_ACCESS_TOKEN=synthetic-qiita-token
CORS_ALLOWED_ORIGINS=http://localhost:5173
SWAGGER_ENABLED=true
EOF
chmod 600 "$envf"

# 本番との違いは、port・保存先・箱の指定だけ
override="$work/override.yml"
cat >"$override" <<YAML
services:
  db:
    ports: !override
      - "127.0.0.1:25433:5432"
    volumes: !override
      - type: tmpfs
        target: /var/lib/postgresql/data
  api-go:
    build: !reset null
    ports: !override
      - "127.0.0.1:${api_port}:8001"
  migrate-go:
    build: !reset null
YAML

cp "$repo_root/docker-compose.yml" "$prev/docker-compose.yml"
# バックアップのscriptがある運用ファイル（composeは、この directory の override と .env を自動で読む）
cp "$repo_root/docker-compose.yml" "$legacy/docker-compose.yml"
cp "$override" "$legacy/docker-compose.override.yml"
cp "$repo_root/ops/backup_db.sh" "$repo_root/ops/verify_backup.sh" "$legacy/ops/"
printf 'POSTGRES_PASSWORD=%s\n' "$password" >"$legacy/.env"

# 直前のreleaseの運用ファイルで動かす（本番では、いま動いているGo版のrelease directory）
dc_prev() { docker compose --env-file "$envf" -p "$project" -f "$prev/docker-compose.yml" -f "$override" "$@"; }

cleanup() {
  GO_API_IMAGE="$old_image" dc_prev --profile go-preview --profile go-migrate down >/dev/null 2>&1 || true
  rm -rf -- "$FX_TMP"
}
trap cleanup EXIT

http_code() { curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$1" || true; }
wait_ready() {
  local _
  for _ in $(seq 1 60); do
    [ "$(http_code "$api_url/health/ready")" = 200 ] && return 0
    sleep 1
  done
  return 1
}
running_image() {
  local container
  container="$(docker ps -q --filter "label=com.docker.compose.project=${project}" --filter "label=com.docker.compose.service=api-go" | head -1 | tr -d '\r')"
  docker inspect --format '{{.Config.Image}}' "$container" | tr -d '\r'
}
count_users() {
  dc_prev exec -T db psql -X -q -At -U postgres -d mytechpulse -c 'SELECT count(*) FROM "user"' | tr -d '\r'
}

# 今の本番を再現する: DB と、直前の箱のGo版
GO_API_IMAGE="$old_image" dc_prev --profile go-preview up -d --wait db >/dev/null 2>&1 || fail "DBを起動できない"
GO_API_IMAGE="$old_image" dc_prev --profile go-migrate run --rm migrate-go >/dev/null 2>&1 || fail "直前の箱の移行に失敗した"
GO_API_IMAGE="$old_image" dc_prev --profile go-preview up -d --no-deps api-go >/dev/null 2>&1 || fail "直前の箱を起動できない"
wait_ready || fail "直前の箱が準備完了にならない"
[ "$(running_image)" = "$old_image" ] || fail "動いている箱が、直前の箱でない"
# 既存の利用者を1人入れておく（入れ替えでデータが変わらないことの確認用）
dc_prev exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d mytechpulse \
  -c "INSERT INTO \"user\" (user_name, password) VALUES ('deploy-it-user', 'synthetic-hash')" >/dev/null 2>&1 || fail "既存の利用者を入れられない"
[ "$(count_users)" = 1 ] || fail "利用者の件数が想定と違う"
ok '今の本番を再現（DB・直前の箱のGo版）と、既存の利用者の登録'

# 実行回ごとに、新しいrelease directoryを使う（同じ実行回は再利用できない）
prepare_release() {
  # prepare_release 実行回のID : 同じ作りのopsのarchive（リポジトリの運用ファイル一式）とmanifestを作り、展開する
  local run_id="$1" src="$FX_TMP/src/ops-real"
  rm -rf "$src" "$work/boot-$run_id"
  mkdir -p "$src" "$work/boot-$run_id"
  (cd "$repo_root" && tar -cf - docker-compose.yml Caddyfile ops) | tar -xf - -C "$src"
  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src" docker-compose.yml Caddyfile ops
  FX_MANIFEST_RUN_ID="$run_id" fx_write_manifest
  MANIFEST_SHA="$MTP_MANIFEST_SHA256"
  tar -xzf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$work/boot-$run_id"
}

run_deploy() {
  # run_deploy 実行回のID [環境変数...] : 終了コードを DEPLOY_STATUS に、出力を DEPLOY_OUT に入れる
  local run_id="$1"
  shift
  prepare_release "$run_id"
  set +e
  DEPLOY_OUT="$(
    env MTP_RELEASES_ROOT="$releases" MTP_ENV_FILE="$envf" MTP_BACKUP_DIR="$legacy" MTP_COMPOSE_PROJECT="$project" \
      MTP_COMPOSE_EXTRA_FILES="$override" MTP_DEPLOY_LOCAL_URL="$api_url" MTP_DEPLOY_BASE_URL="$api_url" \
      MTP_DEPLOY_PREV_DIR="$prev" MTP_DEPLOY_WAIT_SECONDS=20 "$@" \
      bash "$work/boot-$run_id/ops/deploy_go.sh" "$FX_ART" "$MANIFEST_SHA" 2>&1
  )"
  DEPLOY_STATUS=$?
  set -e
}

# ---- 1. 入れ替えに失敗 → 直前の箱へ戻る ----
# 新しい箱の確認先だけを、使っていないportにする。新しい箱は起動しても「準備ができない」扱いになる
run_deploy 5001 MTP_DEPLOY_NEW_LOCAL_URL=http://127.0.0.1:28998
[ "$DEPLOY_STATUS" -eq 1 ] || fail "新しい箱が確認に通らないのに、終了コードが 1 でない: $DEPLOY_STATUS $DEPLOY_OUT"
case "$DEPLOY_OUT" in *"直前の箱へ戻しました"*) ;; *) fail "戻した表示が無い: $DEPLOY_OUT" ;; esac
wait_ready || fail "戻した後に、直前の箱が応答しない"
[ "$(running_image)" = "$old_image" ] || fail "戻した後に動いている箱が、直前の箱でない: $(running_image)"
[ "$(count_users)" = 1 ] || fail "戻しで利用者の件数が変わった"
ok '入れ替えに失敗したとき、確認なしで直前の箱へ戻り、応答し、DBのデータは変わらない'

# 直前の箱が動いたままなので、続けて入れ替えを試せる
# ---- 2. 入れ替えに成功 ----
run_deploy 5002
[ "$DEPLOY_STATUS" -eq 0 ] || fail "入れ替えに失敗した: $DEPLOY_STATUS $DEPLOY_OUT"
case "$DEPLOY_OUT" in *"deploy: ok"*) ;; *) fail "完了の表示が無い: $DEPLOY_OUT" ;; esac
wait_ready || fail "入れ替えた後に、新しい箱が応答しない"
[ "$(running_image)" = "$new_image" ] || fail "入れ替えた後に動いている箱が、新しい箱でない: $(running_image)"
case "$DEPLOY_OUT" in *"直前の箱（戻し先）: ${old_image}"*) ;; *) fail "戻し先が表示されていない" ;; esac
[ "$(count_users)" = 1 ] || fail "入れ替えで利用者の件数が変わった"
ls "$legacy"/backups/*.dump >/dev/null 2>&1 || fail "バックアップのファイルが無い"
ok '入れ替えに成功したとき、新しい箱が動いて応答し、DBのデータは変わらず、バックアップが取れている'

printf 'OK: %s deploy integration cases passed\n' "$passed"
