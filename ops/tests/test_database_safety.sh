#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

for required_file in \
    ops/sql/inspect_schema.sql \
    ops/sql/audit_tag_collisions.sql \
    ops/verify_backup.sh \
    ops/restore_db.sh \
    ops/sql/verify_restored_db.sql \
    ops/verify_restored_db.sh \
    ops/tests/test_database_restore_integration.sh; do
    test -f "$required_file" || fail "missing $required_file"
done

if rg -n "INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER" \
    ops/sql/inspect_schema.sql ops/sql/audit_tag_collisions.sql; then
    fail 'schema audit SQL must be read-only'
fi

rg -q 'pg_constraint' ops/sql/inspect_schema.sql || fail 'constraint audit is missing'
rg -q 'pg_get_serial_sequence' ops/sql/inspect_schema.sql || fail 'sequence audit is missing'
rg -q 'BEGIN READ ONLY' ops/sql/audit_tag_collisions.sql || fail 'tag audit is not read-only'
rg -q "RAISE EXCEPTION 'tag normalization collision'" \
    ops/sql/audit_tag_collisions.sql || fail 'tag collision stop is missing'

rg -q 'pg_dump -Fc' ops/backup_db.sh || fail 'backup is not custom format'
rg -q 'sha256sum' ops/backup_db.sh || fail 'backup checksum is missing'
rg -q 'prune_backups.sh' ops/backup_db.sh || fail 'backup retention is not wired'
rg -q 'RETENTION_DAYS' ops/backup_db.sh || fail 'backup retention days is missing'
# 削除の処理は、バックアップの取得・検証の後ろにあること（失敗したときに消さないため）
restore_line="$(rg -n 'pg_restore --list' ops/backup_db.sh | head -1 | cut -d: -f1)"
prune_line="$(rg -n 'dirname .*prune_backups.sh' ops/backup_db.sh | head -1 | cut -d: -f1)"
[ "$prune_line" -gt "$restore_line" ] || fail 'backup retention runs before the backup is verified'

if ./ops/restore_db.sh sample.dump mytechpulse >/dev/null 2>&1; then
    fail 'existing database name was accepted'
fi

if ./ops/restore_db.sh sample.dump 'mytechpulse_restore_bad-name' >/dev/null 2>&1; then
    fail 'unsafe restore database name was accepted'
fi

rg -F -q 'mytechpulse_restore_*) ;;' ops/restore_db.sh || \
    fail 'restore prefix guard is missing'
rg -F -q '[[ "$restore_db" =~ ^mytechpulse_restore_' ops/restore_db.sh || \
    fail 'restore name validation is missing'
rg -q 'orphan recommend row' ops/sql/verify_restored_db.sql || \
    fail 'restored relation check is missing'
rg -q 'content_digest' ops/restore_db.sh || \
    fail 'restored content digest check is missing'
rg -q 'ROLLBACK;' ops/verify_restored_db.sh || \
    fail 'synthetic write is not rolled back'

printf 'OK: database safety static checks\n'
