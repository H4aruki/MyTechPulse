#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

suffix="$(date -u +%Y%m%d%H%M%S)_$RANDOM"
audit_db="mytechpulse_restore_audit_${suffix}"
restore_db="mytechpulse_restore_test_${suffix}"
backup_dir="$(mktemp -d ./backup-restore-test-XXXXXX)"
dump_path=""
audit_created=0
restore_created=0

cleanup() {
    if [ "$restore_created" -eq 1 ]; then
        docker compose exec -T db dropdb -U postgres "$restore_db" || true
    fi
    if [ "$audit_created" -eq 1 ]; then
        docker compose exec -T db dropdb -U postgres "$audit_db" || true
    fi
    if [ -n "$dump_path" ]; then
        rm -f -- "$dump_path" "$dump_path.sha256"
    fi
    rmdir "$backup_dir" 2>/dev/null || true
}
trap cleanup EXIT

docker compose up -d db

docker compose exec -T db createdb -U postgres "$audit_db"
audit_created=1
docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$audit_db" <<'SQL'
CREATE TABLE tag (
    "tag_ID" serial PRIMARY KEY,
    tag_name varchar(50) NOT NULL UNIQUE
);
SQL

docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$audit_db" \
    < ops/sql/audit_tag_collisions.sql

docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$audit_db" <<'SQL'
INSERT INTO tag (tag_name) VALUES ('Go'), (' go ');
SQL

set +e
docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$audit_db" \
    < ops/sql/audit_tag_collisions.sql
audit_status=$?
set -e
if [ "$audit_status" -ne 3 ]; then
    printf 'expected collision audit to exit 3, got %s\n' "$audit_status" >&2
    exit 1
fi

dump_path="$(BACKUP_DIR="$backup_dir" ./ops/backup_db.sh)"
./ops/verify_backup.sh "$dump_path"
restore_created=1
./ops/restore_db.sh "$dump_path" "$restore_db"
./ops/verify_restored_db.sh "$restore_db"

printf 'OK: database backup and restore integration\n'
