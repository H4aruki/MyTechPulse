#!/usr/bin/env bash
# 合成データだけのdumpを作る（#126）。本番のデータは一切使わない。
#
# 本物の旧版（Python版）APIで利用者を登録して作るため、パスワードの変換値（ハッシュ）は
# 旧版が実際に作る形式になる。Go版がそれを読めるか（既存利用者のログイン）を確かめるのに使う。
# 使い捨てのPostgreSQLと旧版APIをこのscriptが起動し、終わったら、この実行で作った
# container・networkだけを片付ける。既存のcontainer・volumeには触れない。
#
#   使い方: LEGACY_IMAGE=<旧版APIのimage> bash ops/make_synthetic_dump.sh <出力するdumpのpath>
#   出力: <dump> と <dump>.sha256（rehearsal.sh が読む形式）。既にあれば上書きせず終了する。
#   できるdumpに入る合成の利用者（いずれも本物ではない）
#     rehearsal-existing-user / Rehearsal-Existing-Pass-1   （smokeの既存利用者の確認に使う）
#     既存ユーザー-日本語 / パスワード-Pass-1-日本語        （日本語の名前・パスワードの扱いの確認用）
#   rehearsal.sh へは MTP_REHEARSAL_SYNTHETIC=1 を付け、smokeの確認用に次を渡す。
#     MTP_REHEARSAL_EXISTING_USERNAME=rehearsal-existing-user
#     MTP_REHEARSAL_EXISTING_PASSWORD=Rehearsal-Existing-Pass-1
# 終了コード: 0=成功、1=失敗、2=入力の拒否
set -euo pipefail
set +x
umask 077

reject() {
  echo "拒否: $*" >&2
  exit 2
}

output="${1:-}"
legacy_image="${LEGACY_IMAGE:-}"
[ "$#" -eq 1 ] && [ -n "$output" ] || reject "使い方: LEGACY_IMAGE=<image> make_synthetic_dump.sh <出力dump>"
[ -n "$legacy_image" ] || reject "LEGACY_IMAGE（旧版APIのimage）が未設定です"
[[ "$legacy_image" =~ ^[A-Za-z0-9._:/@+=-]+$ ]] || reject "LEGACY_IMAGE の形式が正しくありません"
[ ! -e "$output" ] && [ ! -e "$output.sha256" ] || reject "出力先に既にファイルがあります（上書きしません）"
[ -d "$(dirname "$output")" ] || reject "出力先のdirectoryがありません"
host_port="${MTP_SYNTH_PORT:-18099}"
[[ "$host_port" =~ ^[0-9]+$ ]] || reject "MTP_SYNTH_PORT は数字で指定してください"

suffix="$$-$RANDOM"
net="mtp-synth-$suffix"
db="mtp-synth-db-$suffix"
api="mtp-synth-api-$suffix"
password="synthetic-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
partial="$output.partial-$suffix"

cleanup() {
  docker rm -f "$api" "$db" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -f -- "$partial"
}
trap cleanup EXIT

fail() {
  echo "synthetic-dump: failed ($1)" >&2
  exit 1
}

docker network create "$net" >/dev/null
docker run -d --name "$db" --network "$net" --network-alias db \
  -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB=mytechpulse postgres:17-alpine >/dev/null
for i in $(seq 1 30); do
  docker exec "$db" pg_isready -U postgres -d mytechpulse >/dev/null 2>&1 && break
  [ "$i" -lt 30 ] || fail "db"
  sleep 1
done

# 旧版APIは起動時にテーブルを作る
docker run -d --name "$api" --network "$net" -p "127.0.0.1:${host_port}:8000" \
  -e "DATABASE_URL=postgresql+psycopg://postgres:${password}@db:5432/mytechpulse" \
  -e QIITA_ACCESS_TOKEN=synthetic-token -e SECRET_KEY=synthetic-secret-key-for-dump \
  "$legacy_image" >/dev/null
for i in $(seq 1 60); do
  curl -fsS -o /dev/null --max-time 3 "http://127.0.0.1:${host_port}/" >/dev/null 2>&1 && break
  [ "$i" -lt 60 ] || fail "api"
  sleep 1
done

register() {
  # register 利用者名 パスワード（日本語を含んでも、UTF-8のまま標準入力で渡す）
  local response
  response="$(printf '{"newusername":"%s","newpassword":"%s","favoritetags":["go","react","Python"]}' "$1" "$2" |
    curl -fsS --max-time 20 -H 'Content-Type: application/json; charset=utf-8' --data-binary @- \
      "http://127.0.0.1:${host_port}/auth/create_user")" || fail "登録"
  case "$response" in
    *'"access_token":"'*) ;;
    *) fail "登録（トークンが返らない）" ;;
  esac
}
register 'rehearsal-existing-user' 'Rehearsal-Existing-Pass-1'
register '既存ユーザー-日本語' 'パスワード-Pass-1-日本語'

docker exec "$db" pg_dump -Fc -U postgres -d mytechpulse >"$partial" || fail "dump"
[ "$(wc -c <"$partial")" -ge 100 ] || fail "dump（小さすぎる）"
mv -- "$partial" "$output"
(cd "$(dirname "$output")" && sha256sum "$(basename "$output")" >"$(basename "$output").sha256")
echo "synthetic-dump: ok"
