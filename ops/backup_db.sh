#!/usr/bin/env bash
# PostgreSQLコンテナの mytechpulse DB を pg_dump でバックアップする。
# リポジトリルートから実行する想定（crontab例は下記）:
#   0 4 * * * cd /path/to/MyTechPulse && ./ops/backup_db.sh >> backups/backup.log 2>&1
set -euo pipefail

# ルートの .env（docker compose 用。POSTGRES_PASSWORD を定義）があれば読み込む
if [ -f .env ]; then
    # shellcheck disable=SC1091
    . ./.env
fi

BACKUP_DIR="${BACKUP_DIR:-backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"

mkdir -p "$BACKUP_DIR"
STAMP="$(date +%Y%m%d_%H%M%S)"
DEST="$BACKUP_DIR/mytechpulse_${STAMP}.sql.gz"

# pg_dumpは既定で単一スナップショットから読むため、書き込みを止めずに一貫性のあるダンプが取れる。
# PGPASSWORDはコンテナ内のプロセス環境にだけ渡す（コマンドライン引数にすると ps で見える）
docker compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" db \
    pg_dump -U postgres -d mytechpulse \
    | gzip > "$DEST"

# 空ダンプ（認証失敗等でヘッダすら無い）をバックアップ成功と誤認しないための下限チェック
if [ "$(wc -c < "$DEST")" -lt 100 ]; then
    echo "ERROR: backup file is too small: $DEST" >&2
    exit 1
fi

echo "OK: $DEST ($(wc -c < "$DEST") bytes)"

# RETENTION_DAYS日より古い世代を削除（ローテーション）
find "$BACKUP_DIR" -name 'mytechpulse_*.sql.gz' -mtime +"$RETENTION_DAYS" -delete
