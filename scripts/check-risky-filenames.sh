#!/usr/bin/env bash
# 秘密情報になりやすいファイル名を拒否する。
# 標準入力にパスを1行ずつ受け取り、当たったパスを標準出力へ出して終了コード 1。
# 例外は手元の pre-commit と同じ。
# ちょうど frontend/.env.development と、ファイル名が .env.example のときだけ許可する。
# CI（秘密情報の検査）からも使う。手元を --no-verify で飛ばしても、同じ判定が走る。
set -u

bad=""
while IFS= read -r f || [ -n "${f:-}" ]; do
  [ -z "$f" ] && continue
  base=${f##*/}
  # 意図して追跡している共有設定は、パスがちょうど一致するときだけ許可する
  [ "$f" = "frontend/.env.development" ] && continue
  case "$base" in
    .env.example) ;;
    .env|.env.*|*.pem|*.key|id_rsa|credentials.json) bad+="  $f"$'\n' ;;
  esac
done

if [ -n "$bad" ]; then
  printf '%s' "$bad"
  exit 1
fi
exit 0
