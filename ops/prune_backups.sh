#!/usr/bin/env bash
# 古いバックアップの世代を削除する（#179）。ops/backup_db.sh が、バックアップの取得と検証に成功した後に呼ぶ。
#
#   使い方: ops/prune_backups.sh <バックアップのdirectory> <残す日数>
#
# 消すのは、directory直下の mytechpulse_*.dump と、その .sha256 のうち、更新から「残す日数」を超えたものだけ。
# 次の場合は、何も消さない（安全のため）。
#   - 残す日数以内の新しい .dump が1つも無い（日付がおかしい、バックアップが止まっている、など）
#   - 同じ名前に .keep を付けたファイルがある（例: mytechpulse_X.dump.keep）。切り替え直前のバックアップなど、残したいもの用
# 他のファイル・サブdirectory・シンボリックリンクには触れない。
# 出力は、消した個数の1行だけ（標準エラー）。標準出力には何も出さない（呼び出し元が、最後の行を取るため）。
set -euo pipefail

[ "$#" -eq 2 ] || { echo "使い方: prune_backups.sh <バックアップのdirectory> <残す日数>" >&2; exit 2; }
dir="${1%/}"
days="$2"

[[ "$days" =~ ^[1-9][0-9]*$ ]] || { echo "拒否: 残す日数は1以上の整数で指定してください" >&2; exit 2; }
[ -d "$dir" ] && [ ! -L "$dir" ] || { echo "拒否: バックアップのdirectoryがありません" >&2; exit 2; }

# 新しいバックアップが、残す日数以内に1つも無いときは、消さない
if [ -z "$(find "$dir" -maxdepth 1 -type f -name 'mytechpulse_*.dump' -mtime -"$days" -print -quit)" ]; then
  echo "prune: 残す日数以内の新しいバックアップが無いため、何も消しません" >&2
  exit 0
fi

removed=0
while IFS= read -r -d '' file; do
  base="${file%.sha256}"
  [ ! -e "$base.keep" ] || continue
  rm -f -- "$file"
  removed=$((removed + 1))
done < <(find "$dir" -maxdepth 1 -type f \( -name 'mytechpulse_*.dump' -o -name 'mytechpulse_*.dump.sha256' \) -mtime +"$days" -print0)

echo "prune: ${removed} 個の古いバックアップ（${days}日超）を削除しました" >&2
