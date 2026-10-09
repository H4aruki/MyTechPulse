#!/usr/bin/env bash
# build済みのGo API imageそのものを検査する（sourceのbinaryではなく最終imageを実行する）。
#   1. /migrate --help が、DB設定なしで終了コード0になる
#   2. 既定のentrypoint(/api)が、使い捨てのPostgreSQLと合成設定で起動し、
#      /health/live と /health/ready が200を返す
#   3. 起動したプロセスがroot(uid 0)ではない
# 本番の値やsecretは使わない。作るのはこのscriptが名前を付けた一時containerとnetworkだけで、
# 終了時にそれらだけを片付ける。
set -euo pipefail

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "使い方: $0 <検査するimage>" >&2
  exit 2
fi
IMAGE="$1"
SUFFIX="$$-$RANDOM"
NET="mtp-imgcheck-$SUFFIX"
DB="mtp-imgcheck-db-$SUFFIX"
API="mtp-imgcheck-api-$SUFFIX"
HOST_PORT="${MTP_IMGCHECK_PORT:-18001}"
SYNTH_PASSWORD="synthetic-$SUFFIX"

cleanup() {
  docker rm -f "$API" "$DB" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "1/3 /migrate --help（DB設定なし）"
docker run --rm --entrypoint /migrate "$IMAGE" --help >/dev/null

echo "2/3 既定entrypointの起動確認（合成DB・合成設定）"
docker network create "$NET" >/dev/null
docker run -d --name "$DB" --network "$NET" \
  -e POSTGRES_PASSWORD="$SYNTH_PASSWORD" -e POSTGRES_DB=mtp_imgcheck \
  postgres:17 >/dev/null
for i in $(seq 1 30); do
  if docker exec "$DB" pg_isready -h 127.0.0.1 -U postgres -d mtp_imgcheck >/dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "検査用DBが起動しませんでした" >&2
    exit 1
  fi
  sleep 1
done

docker run -d --name "$API" --network "$NET" -p "127.0.0.1:${HOST_PORT}:8001" \
  -e APP_ENV=test \
  -e DATABASE_URL="postgres://postgres:${SYNTH_PASSWORD}@${DB}:5432/mtp_imgcheck?sslmode=disable" \
  -e QIITA_ACCESS_TOKEN=synthetic-token \
  "$IMAGE" >/dev/null

check_endpoint() {
  local path="$1"
  for i in $(seq 1 30); do
    code="$(curl -s -o /dev/null -w '%{http_code}' -m 3 "http://127.0.0.1:${HOST_PORT}${path}" || true)"
    if [ "$code" = "200" ]; then
      echo "  ${path} -> 200"
      return 0
    fi
    sleep 1
  done
  echo "${path} が200になりませんでした（最後の応答: ${code:-なし}）" >&2
  docker logs "$API" >&2 || true
  return 1
}
check_endpoint /health/live
check_endpoint /health/ready

echo "3/3 非root実行の確認"
configured_user="$(docker inspect --format '{{.Config.User}}' "$API")"
case "$configured_user" in
  "" | 0 | 0:* | root | root:*)
    echo "imageの実行ユーザーがrootです（設定: '${configured_user}'）" >&2
    exit 1
    ;;
esac
running_uid="$(docker top "$API" -eo pid,uid | tail -n +2 | head -n 1 | awk '{print $2}')"
if [ -z "$running_uid" ] || [ "$running_uid" = "0" ] || [ "$running_uid" = "root" ]; then
  echo "起動中のプロセスがrootで動いています（uid: '${running_uid}'）" >&2
  exit 1
fi
echo "  実行ユーザー設定: ${configured_user} / 起動中のuid: ${running_uid}"
echo "image検査に合格しました"
