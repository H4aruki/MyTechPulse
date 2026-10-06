#!/usr/bin/env bash
# 本番の切り替えで使うCaddyの設定3種（Python版向け・メンテナンス・Go版向け）が、
# 想定どおり応答することを、本物のCaddyと偽の向け先で確かめる（#127）。
# 本番・本物のAPI・証明書には触れない。使い捨てのnetworkとcontainerだけを作り、終わりに片付ける。
# 使い方: bash ops/cutover_caddy_test.sh（Docker Desktopが起動していること）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

suffix="$$-$RANDOM"
net="mtp-caddytest-$suffix"
port="${MTP_CADDYTEST_PORT:-18080}"
image="caddy:2-alpine"
containers=()

cleanup() {
    local c
    for c in "${containers[@]}"; do
        docker rm -f "$c" >/dev/null 2>&1 || true
    done
    docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT

passed=0
fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}
ok() {
    passed=$((passed + 1))
    printf 'ok: %s\n' "$1"
}

# 設定を環境変数で渡して起動する（mountを使わないので、パスの違いに左右されない）
start_caddy() {
    # start_caddy NAME ALIAS CONFIG_TEXT [docker runの追加引数...]
    local name="$1" alias_name="$2" config="$3"
    shift 3
    containers+=("$name")
    docker run -d --name "$name" --network "$net" --network-alias "$alias_name" \
        -e "CFG=$config" -e API_DOMAIN=":8080" "$@" "$image" \
        sh -c 'printf "%s\n" "$CFG" >/tmp/Caddyfile && exec caddy run --config /tmp/Caddyfile --adapter caddyfile' >/dev/null
}

wait_up() {
    local i
    for i in $(seq 1 30); do
        if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:${port}/"; then
            return 0
        fi
        sleep 1
    done
    fail "Caddyが起動しない"
}

docker network create "$net" >/dev/null

# 偽の向け先。本物のAPIは使わない
start_caddy "mtp-caddytest-py-$suffix" api ':8000 {
	respond "python-stub" 200
}'
start_caddy "mtp-caddytest-go-$suffix" api-go ':8001 {
	respond "go-stub" 200
}'

# 1. 3つの設定が、Caddyの検証を通る
for file in Caddyfile ops/caddy/Caddyfile.go ops/caddy/Caddyfile.maintenance; do
    out="$(docker run --rm -i -e API_DOMAIN=":8080" "$image" sh -c \
        'cat >/tmp/Caddyfile && caddy validate --config /tmp/Caddyfile --adapter caddyfile' <"$file" 2>&1)" \
        || fail "$file がCaddyの検証に通らない: $out"
done
ok '3つの設定がCaddyの検証を通る'

# 設定を1つ起動して、応答を確かめる共通部品
run_under_test() {
    local label="$1" file="$2" name
    name="mtp-caddytest-sut-$suffix-$label"
    start_caddy "$name" sut "$(cat "$file")" -p "127.0.0.1:${port}:8080"
    wait_up
    SUT="$name"
}
stop_sut() {
    docker rm -f "$SUT" >/dev/null 2>&1 || true
}

# 2. Python版向け（既存のCaddyfile）
run_under_test python Caddyfile
body="$(curl -s --max-time 5 "http://127.0.0.1:${port}/")"
[ "$body" = "python-stub" ] || fail "Python版向けが python-stub へ届いていない: $body"
curl -sI --max-time 5 "http://127.0.0.1:${port}/" | grep -qi '^strict-transport-security: max-age=31536000; includesubdomains' \
    || fail "Python版向けにHSTSが付いていない"
stop_sut
ok 'Python版向けの設定はapiへ届き、HSTSが付く'

# 3. メンテナンス: どの窓口・どの方法でも503と案内を返し、本体へは繋がない
run_under_test maintenance ops/caddy/Caddyfile.maintenance
for target in "GET /" "GET /health/ready" "GET /api/v1/feed" "POST /api/v1/auth/signup" "POST /api/v1/feedback/article-clicks" \
    "DELETE /anything"; do
    method="${target%% *}"
    path="${target#* }"
    status="$(curl -s -o "$PWD/.cutover-caddy-body" -w '%{http_code}' -X "$method" --max-time 5 "http://127.0.0.1:${port}${path}")"
    body="$(cat "$PWD/.cutover-caddy-body")"
    rm -f -- "$PWD/.cutover-caddy-body"
    [ "$status" = 503 ] || fail "メンテナンス中に $target が 503 でない: $status"
    case "$body" in
        *'"code":"maintenance"'*) ;;
        *) fail "メンテナンス中の案内が返っていない: $target" ;;
    esac
    case "$body" in
        *stub*) fail "メンテナンス中に本体へ届いた: $target" ;;
    esac
done
headers="$(curl -sI --max-time 5 "http://127.0.0.1:${port}/")"
grep -qi '^retry-after: 1800' <<<"$headers" || fail "Retry-After が付いていない"
grep -qi '^cache-control: no-store' <<<"$headers" || fail "Cache-Control: no-store が付いていない"
grep -qi '^content-type: application/json' <<<"$headers" || fail "Content-Type がJSONでない"
grep -qi '^strict-transport-security:' <<<"$headers" || fail "メンテナンス中にHSTSが付いていない"
stop_sut
ok 'メンテナンス中は全ての窓口・方法で503と案内を返し、本体へ届かない'

# 4. Go版向け
run_under_test go ops/caddy/Caddyfile.go
body="$(curl -s --max-time 5 "http://127.0.0.1:${port}/")"
[ "$body" = "go-stub" ] || fail "Go版向けが go-stub へ届いていない: $body"
curl -sI --max-time 5 "http://127.0.0.1:${port}/" | grep -qi '^strict-transport-security: max-age=31536000; includesubdomains' \
    || fail "Go版向けにHSTSが付いていない"
stop_sut
ok 'Go版向けの設定はapi-goへ届き、HSTSが付く'

# 5. composeで設定を選べる。既定は従来どおりのPython版向け
compose_caddy_source() {
    # 出力を一度変数へ受けてから読む（awkが先に終わってcomposeがSIGPIPEで失敗するのを避ける）
    local rendered
    rendered="$(env "$@" docker compose --profile prod config 2>/dev/null)"
    awk '/^  caddy:/{f=1} f && /source:/ && /Caddyfile/ {print $2; exit}' <<<"$rendered" | tr '\\' '/'
}
default_source="$(compose_caddy_source API_DOMAIN=example.test)"
case "$default_source" in
    */Caddyfile) ;;
    *) fail "既定の設定が Caddyfile でない: $default_source" ;;
esac
case "$default_source" in
    */ops/caddy/*) fail "既定がメンテナンスやGo版向けになっている" ;;
esac
go_source="$(compose_caddy_source API_DOMAIN=example.test MTP_CADDYFILE=ops/caddy/Caddyfile.go)"
case "$go_source" in
    */ops/caddy/Caddyfile.go) ;;
    *) fail "MTP_CADDYFILE で設定を選べない: $go_source" ;;
esac
ok 'composeは既定でPython版向け、MTP_CADDYFILEで切り替えられる'

# 6. Go版は再起動後に自動で復帰する（止めたときは止まったまま）
rendered_go="$(env API_DOMAIN=example.test docker compose --profile go-preview config 2>/dev/null)"
policy="$(awk '/^  api-go:/{f=1} f && /restart:/ {print $2; exit}' <<<"$rendered_go")"
[ "$policy" = "unless-stopped" ] || fail "api-go の再起動方針が unless-stopped でない: $policy"
ok 'api-goは再起動後に自動で復帰する'

printf 'OK: %s cutover caddy cases passed\n' "$passed"
