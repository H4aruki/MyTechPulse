#!/usr/bin/env bash
# check-risky-filenames.sh の許可と拒否を確かめる。git の索引には触らない。
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
script="$root/scripts/check-risky-filenames.sh"

capture() {
  set +e
  captured=$(printf '%s\n' "$@" | bash "$script")
  captured_status=$?
  set -e
}

expect_allow() {
  capture "$@"
  if [ "$captured_status" -ne 0 ] || [ -n "$captured" ]; then
    echo "許可されるはずのパスを拒否しました: $*" >&2
    printf 'status=%s\n%s\n' "$captured_status" "$captured" >&2
    exit 1
  fi
}

expect_block() {
  local needle="$1"
  shift
  capture "$@"
  if [ "$captured_status" -ne 1 ] || ! printf '%s\n' "$captured" | grep -F -q -- "$needle"; then
    echo "拒否されるはずのパスを見逃しました: $needle" >&2
    printf 'status=%s\n%s\n' "$captured_status" "$captured" >&2
    exit 1
  fi
}

expect_allow \
  "frontend/.env.development" \
  ".env.example" \
  "server/.env.example" \
  "README.md" \
  "src/app.ts"

expect_block "server/.env" "server/.env"
expect_block ".env" ".env"
expect_block "app/.env.local" "app/.env.local"
expect_block "certs/a.pem" "certs/a.pem"
expect_block "id_rsa" "secrets/id_rsa"
expect_block "credentials.json" "credentials.json"
expect_block "k.key" "k.key"
expect_block "backend/.env.development" "backend/.env.development"

capture "README.md" "server/.env" "server/.env.example"
if [ "$captured_status" -ne 1 ] || ! printf '%s\n' "$captured" | grep -F -q -- "  server/.env"; then
  echo "混在した一覧から危険なファイル名を取り出せませんでした" >&2
  exit 1
fi
if printf '%s\n' "$captured" | grep -F -q -- "server/.env.example"; then
  echo ".env.example を危険なファイル名として数えています" >&2
  exit 1
fi

set +e
printf '' | bash "$script" >/dev/null
empty_status=$?
set -e
if [ "$empty_status" -ne 0 ]; then
  echo "空の入力を拒否しました" >&2
  exit 1
fi

echo "check-risky-filenames: ok"
