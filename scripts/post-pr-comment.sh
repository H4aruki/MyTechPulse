#!/usr/bin/env bash
# 渡したMarkdownファイルを、PRへコメントとして投稿する。
# 先頭行の目印が同じコメントがすでにあれば、新しく作らず上書きする。
# 必要な環境変数: GH_TOKEN, GITHUB_REPOSITORY, PR_NUMBER
set -euo pipefail
file="$1"
marker="$(head -n 1 "$file")"

id="$(gh api "repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments" --paginate \
  --jq ".[] | select(.body | startswith(\"${marker}\")) | .id" | head -n 1)"

if [ -n "$id" ]; then
  gh api -X PATCH "repos/${GITHUB_REPOSITORY}/issues/comments/${id}" -F body=@"$file" > /dev/null
else
  gh api -X POST "repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments" -F body=@"$file" > /dev/null
fi
