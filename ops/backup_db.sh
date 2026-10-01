#!/usr/bin/env bash
# PostgreSQLコンテナの mytechpulse DB を custom形式でバックアップする。
# リポジトリルートから実行する想定（crontab例は下記）:
#   0 4 * * * cd /path/to/MyTechPulse && ./ops/backup_db.sh >> backups/backup.log 2>&1
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-backups}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"

mkdir -p "$BACKUP_DIR"
umask 077
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="$BACKUP_DIR/mytechpulse_${STAMP}.dump"

if [ -e "$DEST" ] || [ -e "$DEST.sha256" ]; then
    echo "ERROR: backup destination already exists: $DEST" >&2
    exit 1
fi

compose_exec=(docker compose exec -T)
if [ -n "$POSTGRES_PASSWORD" ]; then
    compose_exec+=(-e "PGPASSWORD=$POSTGRES_PASSWORD")
fi

# pg_dumpは既定で単一スナップショットから読むため、書き込みを止めずに一貫性のあるダンプが取れる。
# PGPASSWORDはコンテナ内のプロセス環境にだけ渡す（コマンドライン引数にすると ps で見える）
"${compose_exec[@]}" db pg_dump -Fc -U postgres -d mytechpulse > "$DEST"

# 空ダンプ（認証失敗等でヘッダすら無い）をバックアップ成功と誤認しないための下限チェック
if [ "$(wc -c < "$DEST")" -lt 100 ]; then
    echo "ERROR: backup file is too small: $DEST" >&2
    exit 1
fi

backup_dir="$(dirname "$DEST")"
backup_name="$(basename "$DEST")"
(cd "$backup_dir" && sha256sum "$backup_name") > "$DEST.sha256"

docker compose exec -T db pg_restore --list < "$DEST" > /dev/null

printf '%s\n' "$DEST"
