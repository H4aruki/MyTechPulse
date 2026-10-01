#!/usr/bin/env bash
set -euo pipefail

restore_db="${1:?usage: verify_restored_db.sh NEW_DB_NAME}"
if [[ ! "$restore_db" =~ ^mytechpulse_restore_[a-z0-9_]+$ ]]; then
    echo "restore database must start with mytechpulse_restore_ and use lowercase letters, digits, or underscores" >&2
    exit 2
fi

docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres \
    -d "$restore_db" < ops/sql/verify_restored_db.sql

docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$restore_db" <<'SQL'
BEGIN;
INSERT INTO "user" (user_name, password)
VALUES ('__restore_probe_user__', '$2b$12$synthetic.not.a.real.user.hash');
INSERT INTO tag (tag_name) VALUES ('__restore_probe_tag__');
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 1
FROM "user" AS u, tag AS t
WHERE u.user_name = '__restore_probe_user__'
  AND t.tag_name = '__restore_probe_tag__';
ROLLBACK;
SQL

printf 'OK: restored database verified\n'
