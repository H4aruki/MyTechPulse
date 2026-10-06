#!/usr/bin/env bash
# docker-compose.rehearsal.yml が、本番と切り離された構成になっていることを確かめる。
# 使い方: bash ops/rehearsal_compose_test.sh（docker composeが使えること。コンテナは起動しない）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

digest_image='ghcr.io/h4aruki/mytechpulse-api-go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
frontend_dir="$(mktemp -d)"
trap 'rmdir -- "$frontend_dir"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

# 必須の環境変数をすべて渡して設定を展開する。渡さないものは引数で上書きする
render() {
    env -u MTP_REHEARSAL_IMAGE -u MTP_REHEARSAL_DB_PASSWORD -u MTP_REHEARSAL_FRONTEND_DIR \
        "$@" \
        docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml \
        --profile migrate --profile serve config 2>&1
}

full_env=(MTP_REHEARSAL_IMAGE="$digest_image" MTP_REHEARSAL_DB_PASSWORD=synthetic-rehearsal-only
    MTP_REHEARSAL_FRONTEND_DIR="$frontend_dir")

rendered="$(render "${full_env[@]}")" || fail "必須の値を渡しても設定を展開できない: $rendered"

passed=0
ok() {
    passed=$((passed + 1))
    printf 'ok: %s\n' "$1"
}

# 本番のvolume・project・portを一切使わない
for forbidden in 'mytechpulse_db_data' 'caddy_data' 'caddy_config' 'name: mytechpulse$'; do
    if grep -Eq "$forbidden" <<<"$rendered"; then
        fail "本番と共通の名前が含まれている: $forbidden"
    fi
done
ok '本番のvolume名とproject名を含まない'

grep -q '^name: mytechpulse-rehearsal$' <<<"$rendered" || fail "project名が隔離用でない"
grep -q 'name: mytechpulse_rehearsal_db$' <<<"$rendered" || fail "DB用volume名が隔離用でない"
ok 'project名とDB用volume名が隔離用'

published="$(grep -E '^\s+published:' <<<"$rendered" | tr -d ' "' | sort | tr '\n' ' ')"
[ "$published" = 'published:15432 published:18001 published:18443 ' ] \
    || fail "公開portが想定外: $published"
if grep -Eq 'published: "?(8000|8001|5432|80|443)"?$' <<<"$rendered"; then
    fail "本番と同じhost portを公開している"
fi
ok '公開portは15432/18001/18443だけ'

# 公開portはすべてloopbackだけに束縛する
[ "$(grep -c 'host_ip: 127.0.0.1' <<<"$rendered")" -eq 3 ] || fail "loopback以外へ公開するportがある"
ok '公開portはすべて127.0.0.1に束縛'

if grep -Eq '^\s+build:' <<<"$rendered"; then
    fail "build節がある（検証済みimageを使わずにbuildしてしまう）"
fi
ok 'build節が無い'

[ "$(grep -c "image: $digest_image" <<<"$rendered")" -eq 2 ] || fail "APIとmigrationが同じdigestのimageを使っていない"
if grep -Eq 'image: ghcr\.io/.*(:latest|:[0-9a-f]{7,40})$' <<<"$rendered"; then
    fail "tagで指定したimageがある"
fi
ok 'APIとmigrationは同じdigestのimage'

grep -q 'APP_ENV: test' <<<"$rendered" || fail "既定のAPP_ENVがtestでない"
ok '既定のAPP_ENVはtest'

# 必須の値が無ければ、設定の展開自体を失敗させる
if render MTP_REHEARSAL_DB_PASSWORD=synthetic-rehearsal-only MTP_REHEARSAL_FRONTEND_DIR="$frontend_dir" >/dev/null; then
    fail "imageを指定しなくても展開できてしまう"
fi
if render MTP_REHEARSAL_IMAGE="$digest_image" MTP_REHEARSAL_FRONTEND_DIR="$frontend_dir" >/dev/null; then
    fail "DB passwordを指定しなくても展開できてしまう"
fi
if render MTP_REHEARSAL_IMAGE="$digest_image" MTP_REHEARSAL_DB_PASSWORD=synthetic-rehearsal-only >/dev/null; then
    fail "frontendのdirectoryを指定しなくても展開できてしまう"
fi
ok 'image・DB password・frontend directoryが無いと展開に失敗する'

# profile無しではdbだけが対象になる（復元前にmigrationやAPIが動かない）
services="$(env "${full_env[@]}" \
    docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml config --services)"
[ "$services" = 'db' ] || fail "profile無しでdb以外が対象になる: $services"
ok 'profile無しはdbだけ'

printf 'OK: %s rehearsal compose cases passed\n' "$passed"
