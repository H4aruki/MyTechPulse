#!/usr/bin/env bash
# ブラウザ確認のために、移行済みの隔離DBに対してGo版を、本番と同じホスト名（api.mytechpulse.net）で起動する（#126）。
# 画面（frontend）はAPIの本番ホスト名を埋め込んで作られているため、この名前で受け付ける必要がある。
# 通信は隔離環境のCaddyへ届く。PCのhostsファイルや443番portは変えない（下の起動手順を参照）。
#
#   使い方: bash ops/rehearsal_browser.sh up test|production
#           bash ops/rehearsal_browser.sh stop
#   入力（環境変数）
#     MTP_REHEARSAL_RELEASE_DIR  rehearsal.sh が使った検証済みのrelease directory（release.env と frontend-site がある所）
#     MTP_REHEARSAL_DB_NAME      rehearsal.sh が最後に表示した復元先のDB名
#     MTP_REHEARSAL_DB_PASSWORD  rehearsal.sh と同じ隔離DB用のpassword
#   up test        : APP_ENV=test。Swagger UI（/docs）が表示される
#   up production  : APP_ENV=production。Swagger UIは404になる。Cookieは本番と同じ __Host- 付きのSecure
#   stop           : containerを止めるだけ。DB・volumeは残す
#
# ブラウザは、使い捨ての専用profileで次のように起動する（Chromeの例）。
#   chrome --user-data-dir=<新しい空のfolder> --host-resolver-rules="MAP api.mytechpulse.net 127.0.0.1:18443" \
#          https://api.mytechpulse.net/app/
# 隔離環境の証明書は使い捨ての内部CAのため、警告が出たら、この専用profileでだけ続行する。
# 専用profileを使うのは、本物の本番サイトを見たブラウザのHSTS記録が影響しないようにするため。
# 終了コード: 0=成功、1=失敗、2=入力の拒否
set -euo pipefail
set +x
umask 077

reject() {
  echo "拒否: $*" >&2
  exit 2
}

action="${1:-}"
mode="${2:-}"
case "$action" in
  up)
    case "$mode" in
      test | production) ;;
      *) reject "使い方: rehearsal_browser.sh up test|production" ;;
    esac
    [ "$#" -eq 2 ] || reject "使い方: rehearsal_browser.sh up test|production"
    ;;
  stop)
    [ "$#" -eq 1 ] || reject "使い方: rehearsal_browser.sh stop"
    ;;
  *) reject "使い方: rehearsal_browser.sh up test|production / stop" ;;
esac

release_dir="${MTP_REHEARSAL_RELEASE_DIR:-}"
db_name="${MTP_REHEARSAL_DB_NAME:-}"
db_password="${MTP_REHEARSAL_DB_PASSWORD:-}"
[ -n "$release_dir" ] && [ -d "$release_dir" ] || reject "MTP_REHEARSAL_RELEASE_DIR のdirectoryがありません"
[[ "$db_name" =~ ^[a-z0-9_]{1,63}$ ]] || reject "MTP_REHEARSAL_DB_NAME は rehearsal.sh が表示した名前（小文字・数字・_）で指定してください"
[[ "$db_password" =~ ^[A-Za-z0-9._~-]+$ ]] || reject "MTP_REHEARSAL_DB_PASSWORD を英数字と . _ ~ - だけで指定してください"

compose_file="$release_dir/docker-compose.rehearsal.yml"
[ -f "$compose_file" ] || reject "release directory に docker-compose.rehearsal.yml がありません"
[ -d "$release_dir/frontend-site" ] || reject "release directory に frontend-site がありません（rehearsal.sh を先に実行してください）"

api_image=""
if [ -f "$release_dir/release.env" ]; then
  while IFS= read -r line; do
    case "$line" in
      GO_API_IMAGE=*) api_image="${line#GO_API_IMAGE=}" ;;
    esac
  done <"$release_dir/release.env"
fi
[[ "$api_image" =~ ^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$ ]] ||
  reject "release.env にmanifest由来の完全digestがありません"

export MTP_REHEARSAL_IMAGE="$api_image"
export MTP_REHEARSAL_DB_PASSWORD="$db_password"
export MTP_REHEARSAL_DB_NAME="$db_name"
export MTP_REHEARSAL_FRONTEND_DIR="$release_dir/frontend-site"
export MTP_REHEARSAL_SITE="api.mytechpulse.net"
export MTP_REHEARSAL_ORIGIN="https://api.mytechpulse.net"
case "$mode" in
  production)
    export MTP_REHEARSAL_APP_ENV=production MTP_REHEARSAL_SWAGGER_ENABLED=false
    ;;
  *)
    export MTP_REHEARSAL_APP_ENV=test MTP_REHEARSAL_SWAGGER_ENABLED=true
    ;;
esac

dc() {
  docker compose -p mytechpulse-rehearsal -f "$compose_file" "$@"
}

if [ "$action" = stop ]; then
  # 止めるだけ。containerを消さず、DB・volumeも残す
  dc --profile serve --profile legacy --profile migrate stop >/dev/null 2>&1
  echo "rehearsal-browser: stopped"
  exit 0
fi

fail() {
  echo "rehearsal-browser: failed ($1)" >&2
  exit 1
}

dc up -d --wait db >/dev/null 2>&1 || fail "db"
exists="$(dc exec -T db psql -X -q -At -U postgres -d postgres \
  -c "SELECT 1 FROM pg_database WHERE datname = '$db_name'" 2>/dev/null)" || fail "db"
[ "$exists" = "1" ] || fail "指定したDBが無い"

dc --profile serve up -d --wait api caddy >/dev/null 2>&1 || fail "起動"

wait_http() {
  local tries
  for ((tries = 0; tries <= "${MTP_REHEARSAL_WAIT_SECONDS:-60}"; tries++)); do
    if curl -fsS -o /dev/null --max-time 3 "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# 本番と同じホスト名で、隔離Caddy経由のAPIと画面に届くこと
wait_http -k --resolve api.mytechpulse.net:18443:127.0.0.1 https://api.mytechpulse.net:18443/health/ready || fail "health"
wait_http -k --resolve api.mytechpulse.net:18443:127.0.0.1 https://api.mytechpulse.net:18443/ || fail "画面"

# Swagger UIは、test環境では表示、production設定では404。Caddyを通さず、APIへ直接確かめる
docs_status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:18001/docs)" || fail "swagger"
case "$mode" in
  production) [ "$docs_status" = 404 ] || fail "production設定でSwagger UIが404にならない" ;;
  *) [ "$docs_status" = 200 ] || fail "test環境でSwagger UIが表示されない" ;;
esac

echo "rehearsal-browser: up ($mode) ok"
echo "rehearsal-browser: swagger $docs_status"
echo "rehearsal-browser: 専用profileのブラウザで https://api.mytechpulse.net/app/ を開く（--host-resolver-rules=\"MAP api.mytechpulse.net 127.0.0.1:18443\"）"
