#!/usr/bin/env bash
# Git フックを有効化する。何度実行しても同じ結果になる。
set -eu
root=$(git rev-parse --show-toplevel)
cd "$root"
git config core.hooksPath .githooks
chmod +x .githooks/* scripts/setup-hooks.sh
echo "core.hooksPath = $(git config core.hooksPath)"
echo "実行権限を付けたフック:"
ls -1 .githooks | sed 's/^/  /'
