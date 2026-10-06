#!/usr/bin/env bash
# ops/cutover.sh を、本物のDocker・Caddy・PostgreSQL・Go版の箱で、本番と同じ構成を別の名前・別のportで
# 使い捨てに作って通す（#127）。本番・本物のドメイン・既存のproject/volumeには一切触れない。
#
# 通すもの: preflight → maintenance-on → backup → snapshot-before → migrate → compare → go-start → switch
#           → status → rollback → status、および、メンテナンス中にPython版へ書き込めないこと
# 通さないもの: smoke（本番のホスト名のHTTPSとCookieが要る。#126 の rehearsal.sh と手順書で確認する）
#
# 必要な環境変数
#   MTP_IT_GO_IMAGE      Go版の箱（ghcr.io/h4aruki/mytechpulse-api-go@sha256:... 。事前に docker pull しておく）
#   MTP_IT_LEGACY_IMAGE  旧版（Python版）の箱（例: mytechpulse-api:latest）
# 使い方: MTP_IT_GO_IMAGE=... MTP_IT_LEGACY_IMAGE=... bash ops/cutover_integration_test.sh
# DBとCaddyの保存先はtmpfs（保存しない）。終了時は container を止めて消す（volumeは作らないので、消すvolumeも無い）。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_image="${MTP_IT_GO_IMAGE:-}"
legacy_image="${MTP_IT_LEGACY_IMAGE:-}"
[ -n "$go_image" ] && [ -n "$legacy_image" ] || { echo "MTP_IT_GO_IMAGE と MTP_IT_LEGACY_IMAGE を指定してください" >&2; exit 2; }
docker image inspect "$go_image" >/dev/null 2>&1 || { echo "Go版の箱がありません: $go_image" >&2; exit 2; }
docker image inspect "$legacy_image" >/dev/null 2>&1 || { echo "旧版の箱がありません: $legacy_image" >&2; exit 2; }

project="mtp-cutover-it"
work="$(mktemp -d)"
chmod 700 -- "$work"
legacy="$work/legacy"
release="$work/release"
mkdir -p "$legacy/ops" "$release"
base_port=28080
base_url="http://127.0.0.1:${base_port}"

passed=0
fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}
ok() {
    passed=$((passed + 1))
    printf 'ok: %s\n' "$1"
}

cleanup() {
    docker compose -p "$project" -f "$legacy/docker-compose.yml" -f "$work/override.yml" \
        --profile prod --profile go-preview --profile go-migrate down >/dev/null 2>&1 || true
    rm -rf -- "$work"
}
trap cleanup EXIT

password="it-$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')"

# 旧版の運用ファイル一式（切り戻し先）
cp "$repo_root/docker-compose.yml" "$legacy/docker-compose.yml"
cp "$repo_root/Caddyfile" "$legacy/Caddyfile"
cp "$repo_root/ops/backup_db.sh" "$repo_root/ops/verify_backup.sh" "$legacy/ops/"
printf 'POSTGRES_PASSWORD=%s\nAPI_DOMAIN=:80\n' "$password" >"$legacy/.env"

# 本番との違いは、port・保存先・箱の指定だけ（Docker Desktopと開発用のDBと衝突しないようにする）
cat >"$work/override.yml" <<YAML
services:
  db:
    ports: !override
      - "127.0.0.1:25432:5432"
    volumes: !override
      - type: tmpfs
        target: /var/lib/postgresql/data
  api:
    build: !reset null
    image: ${legacy_image}
    ports: !override
      - "127.0.0.1:28000:8000"
    environment:
      QIITA_ACCESS_TOKEN: synthetic-token
      SECRET_KEY: synthetic-secret-key-for-integration
  api-go:
    build: !reset null
    ports: !override
      - "127.0.0.1:28001:8001"
  caddy:
    environment:
      API_DOMAIN: ":80"
    ports: !override
      - "127.0.0.1:${base_port}:80"
    volumes: !override
      - ./\${MTP_CADDYFILE:-Caddyfile}:/etc/caddy/Caddyfile:ro
      - type: tmpfs
        target: /data
      - type: tmpfs
        target: /config
YAML

# 切り替え先のrelease directory（検証済みの配布物を展開したものに相当）
cp "$repo_root/docker-compose.yml" "$release/docker-compose.yml"
cp "$repo_root/Caddyfile" "$release/Caddyfile"
mkdir -p "$release/ops"
cp -r "$repo_root/ops/lib" "$repo_root/ops/caddy" "$repo_root/ops/sql" "$release/ops/"
cp "$repo_root/ops/cutover.sh" "$repo_root/ops/snapshot_migration_state.sh" "$repo_root/ops/compare_migration_state.sh" \
    "$repo_root/ops/rehearsal_smoke.sh" "$release/ops/"
printf 'MTP_RELEASE_DIR=%s\nGO_API_IMAGE=%s\n' "$release" "$go_image" >"$release/release.env"
cat >"$release/previous-release.json" <<JSON
{
  "manifest_sha256": "legacy-python-no-manifest",
  "api_image": "${legacy_image}",
  "frontend_deployment_id": "deploy-it-0001",
  "frontend_artifact_name": "frontend-previous",
  "frontend_sha256": "legacy-no-hash",
  "ops_artifact_name": "ops-previous",
  "ops_sha256": "legacy-no-hash",
  "ops_release_dir": "${legacy}"
}
JSON

# Go版の設定ファイル（合成）
envf="$work/production.env"
cat >"$envf" <<EOF
POSTGRES_PASSWORD=${password}
API_DOMAIN=api.example.test
APP_ENV=production
QIITA_ACCESS_TOKEN=synthetic-qiita-token
CORS_ALLOWED_ORIGINS=https://front.test
SWAGGER_ENABLED=false
EOF
chmod 600 "$envf"

export MTP_RELEASE_DIR="$release" MTP_ENV_FILE="$envf" MTP_COMPOSE_PROJECT="$project"
export MTP_COMPOSE_EXTRA_FILES="$work/override.yml"
export MTP_CUTOVER_BASE_URL="$base_url" MTP_CUTOVER_GO_URL="http://127.0.0.1:28001" MTP_CUTOVER_PY_URL="http://127.0.0.1:28000"
export MTP_CUTOVER_WAIT_SECONDS=90

# 今のPython版の本番を再現する（db・Python版API・Caddy）。旧版のcomposeを、その directory から起動する
legacy_dc() { (cd "$legacy" && docker compose -p "$project" -f docker-compose.yml -f "$work/override.yml" "$@"); }
legacy_dc --profile prod up -d --wait db api caddy >/dev/null 2>&1 || fail "Python版の本番を再現できない"
for _ in $(seq 1 60); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:28000/" || true)" = 200 ] && break
    sleep 1
done
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$base_url/" || true)" = 200 ] || fail "Caddy経由でPython版に届かない"

# 既存の利用者を1人、旧版のAPIで登録しておく（件数と、旧版が作ったパスワードの形式の確認用）
register="$(printf '{"newusername":"cutover-existing","newpassword":"Cutover-Existing-1","favoritetags":["go","react"]}' |
    curl -fsS --max-time 20 -H 'Content-Type: application/json' --data-binary @- "http://127.0.0.1:28000/auth/create_user")" ||
    fail "既存の利用者を登録できない"
case "$register" in *'"access_token":"'*) ;; *) fail "既存の利用者の登録に失敗した" ;; esac
ok 'Python版の本番を再現（Python版API・Caddy・DB）と既存の利用者の登録'

cutover() { bash "$release/ops/cutover.sh" "$1"; }

out="$(cutover preflight 2>&1)" || fail "preflight が失敗した: $out"
ok 'preflight が通る（本物のcompose・箱・DBパスワードの確認）'

out="$(cutover status 2>&1)"
case "$out" in *"caddy=python"*) ;; *) fail "statusがPython版向けを示さない: $out" ;; esac

cutover maintenance-on >/dev/null 2>&1 || fail "maintenance-on が失敗した"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$base_url/")" = 503 ] || fail "メンテナンス中に503にならない"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$base_url/auth/create_user")" = 503 ] || fail "メンテナンス中に書き込みが通る"
out="$(legacy_dc ps --status running --services 2>/dev/null | tr -d '\r' | sort | tr '\n' ' ')"
case " $out" in *" api "*) fail "Python版APIが止まっていない" ;; esac
case "$(cutover status 2>&1)" in *"caddy=maintenance"*) ;; *) fail "statusがメンテナンスを示さない" ;; esac
ok 'maintenance-on: 503になり、書き込みは通らず、Python版APIが止まる'

out="$(cutover backup 2>&1)" || fail "backup が失敗した: $out"
case "$out" in *"counts(user,tag,recommend) 1,2,2"*) ;; *) fail "件数が想定と違う: $out" ;; esac
ls "$legacy/backups/"*.dump >/dev/null 2>&1 || fail "バックアップのファイルが無い"
ok 'backup: 本物のpg_dumpで取得・検証でき、件数（利用者1・タグ2・興味度2）が出る'

cutover snapshot-before >/dev/null 2>&1 || fail "snapshot-before が失敗した"
out="$(cutover migrate 2>&1)" || fail "migrate が失敗した: $out"
out="$(cutover compare 2>&1)" || fail "compare が失敗した: $out"
case "$out" in *"counts(user,tag,recommend) 1,2,2"*) ;; *) fail "移行後の件数が違う: $out" ;; esac
ok 'migrate・compare: 本物のGo版の箱で移行でき、移行前後が一致する'

out="$(cutover go-start 2>&1)" || fail "go-start が失敗した: $out"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:28001/health/ready")" = 200 ] || fail "Go版が準備完了にならない"
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$base_url/")" = 503 ] || fail "switch前にメンテナンスでなくなった"
ok 'go-start: Go版が起動して準備完了。公開はまだメンテナンスのまま'

set +e
cutover switch >/dev/null 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "画面の公開の確認なしで switch できてしまう"
out="$(MTP_CUTOVER_CONFIRM_FRONTEND=yes cutover switch 2>&1)" || fail "switch が失敗した: $out"
case "$out" in *"stop-time"*) ;; *) fail "停止時間が出ない: $out" ;; esac
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$base_url/health/ready")" = 200 ] || fail "Caddy経由でGo版に届かない"
case "$(cutover status 2>&1)" in *"caddy=go"*) ;; *) fail "statusがGo版向けを示さない" ;; esac
ok 'switch: 画面の公開の確認が要り、Caddy経由でGo版に届く'

# Go版に旧版が作ったパスワードで入れる（Caddy経由）
login_status="$(printf '{"username":"cutover-existing","password":"Cutover-Existing-1"}' |
    curl -s -o /dev/null -w '%{http_code}' --max-time 10 -X POST "$base_url/api/v1/auth/login" -H 'Content-Type: application/json' \
        -H 'Origin: https://front.test' -H 'X-MTP-CSRF: 1' --data-binary @-)"
[ "$login_status" = 200 ] || fail "旧版が作った利用者でGo版にログインできない: $login_status"
ok 'Caddy経由で、旧版が作った利用者がGo版にログインできる'

out="$(cutover rollback 2>&1)" || fail "rollback が失敗した: $out"
case "$out" in *"deploy-it-0001"*) ;; *) fail "画面を戻す識別子が出ない: $out" ;; esac
[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$base_url/")" = 200 ] || fail "切り戻し後にPython版へ届かない"
body="$(curl -s --max-time 5 "$base_url/")"
case "$body" in *Hello*) ;; *) fail "切り戻し後がPython版の応答でない: $body" ;; esac
out="$(cutover status 2>&1)"
case "$out" in *"caddy=python"*) ;; *) fail "切り戻し後にstatusがPython版向けでない: $out" ;; esac
case "$out" in *" api-go "*) fail "切り戻し後もGo版が動いている: $out" ;; esac
ok 'rollback: Python版へ戻り、Go版は止まる'

# データは巻き戻らない（Go版が追加した表は残り、利用者は残る）
users="$(docker compose -p "$project" -f "$legacy/docker-compose.yml" -f "$work/override.yml" exec -T db \
    psql -X -q -At -U postgres -d mytechpulse -c 'SELECT count(*) FROM "user"' | tr -d '\r')"
[ "$users" = 1 ] || fail "切り戻しで利用者が変わった: $users"
tables="$(docker compose -p "$project" -f "$legacy/docker-compose.yml" -f "$work/override.yml" exec -T db \
    psql -X -q -At -U postgres -d mytechpulse -c "SELECT count(*) FROM pg_tables WHERE tablename = 'auth_session'" | tr -d '\r')"
[ "$tables" = 1 ] || fail "追加した表が残っていない（DBを巻き戻した）"
ok '切り戻しでDBは巻き戻らない（利用者は残り、追加した表も残る）'

# 切り戻しの後は、最初からやり直せる
out="$(cutover preflight 2>&1)" || fail "切り戻しの後に preflight をやり直せない: $out"
ok '切り戻しの後は、最初からやり直せる'

printf 'OK: %s cutover integration cases passed\n' "$passed"
