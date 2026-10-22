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

if grep -rnE "INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER" \
    ops/sql/inspect_schema.sql ops/sql/audit_tag_collisions.sql; then
    fail 'schema audit SQL must be read-only'
fi

grep -qE 'pg_constraint' ops/sql/inspect_schema.sql || fail 'constraint audit is missing'
grep -qE 'pg_get_serial_sequence' ops/sql/inspect_schema.sql || fail 'sequence audit is missing'
grep -qE 'BEGIN READ ONLY' ops/sql/audit_tag_collisions.sql || fail 'tag audit is not read-only'
grep -qE "RAISE EXCEPTION 'tag normalization collision'" \
    ops/sql/audit_tag_collisions.sql || fail 'tag collision stop is missing'

grep -qE 'pg_dump -Fc' ops/backup_db.sh || fail 'backup is not custom format'
grep -qE 'sha256sum' ops/backup_db.sh || fail 'backup checksum is missing'
grep -qE 'prune_backups.sh' ops/backup_db.sh || fail 'backup retention is not wired'
grep -qE 'RETENTION_DAYS' ops/backup_db.sh || fail 'backup retention days is missing'
# 削除の処理は、バックアップの取得・検証の後ろにあること（失敗したときに消さないため）
restore_line="$(grep -nE 'pg_restore --list' ops/backup_db.sh | head -1 | cut -d: -f1)"
prune_line="$(grep -nE 'dirname .*prune_backups.sh' ops/backup_db.sh | head -1 | cut -d: -f1)"
[ "$prune_line" -gt "$restore_line" ] || fail 'backup retention runs before the backup is verified'

if ./ops/restore_db.sh sample.dump mytechpulse >/dev/null 2>&1; then
    fail 'existing database name was accepted'
fi

if ./ops/restore_db.sh sample.dump 'mytechpulse_restore_bad-name' >/dev/null 2>&1; then
    fail 'unsafe restore database name was accepted'
fi

grep -Fq 'mytechpulse_restore_*) ;;' ops/restore_db.sh || \
    fail 'restore prefix guard is missing'
grep -Fq '[[ "$restore_db" =~ ^mytechpulse_restore_' ops/restore_db.sh || \
    fail 'restore name validation is missing'
grep -qE 'orphan recommend row' ops/sql/verify_restored_db.sql || \
    fail 'restored relation check is missing'
grep -qE 'content_digest' ops/restore_db.sh || \
    fail 'restored content digest check is missing'
grep -qE 'ROLLBACK;' ops/verify_restored_db.sh || \
    fail 'synthetic write is not rolled back'

printf 'OK: database safety static checks\n'
