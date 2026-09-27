#!/usr/bin/env bash
set -euo pipefail

dump_path="${1:?usage: restore_db.sh DUMP_FILE NEW_DB_NAME}"
restore_db="${2:?usage: restore_db.sh DUMP_FILE NEW_DB_NAME}"

case "$restore_db" in
  mytechpulse_restore_*) ;;
  *) echo "restore database must start with mytechpulse_restore_" >&2; exit 2 ;;
esac

if [[ "$restore_db" =~ ^mytechpulse_restore_[a-z0-9_]+$ ]]; then
    :
else
    echo "restore database name contains unsafe characters" >&2
    exit 2
fi

content_digest() {
    local database_name="$1"

    docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres \
        -d "$database_name" <<'SQL'
COPY (
    SELECT record_type, first_value, second_value, third_value
    FROM (
        SELECT 'user' AS record_type,
               "user_ID"::text AS first_value,
               user_name AS second_value,
               password AS third_value
        FROM public."user"
        UNION ALL
        SELECT 'tag', "tag_ID"::text, tag_name, ''
        FROM public.tag
        UNION ALL
        SELECT 'recommend', "user_ID"::text, "tag_ID"::text, match_int::text
        FROM public.recommend
    ) AS records
    ORDER BY record_type, first_value, second_value
) TO STDOUT WITH (FORMAT csv)
SQL
}

./ops/verify_backup.sh "$dump_path"

if docker compose exec -T db psql -X -q -U postgres -d postgres -tAc \
    "SELECT 1 FROM pg_database WHERE datname = '$restore_db'" | grep -qx '1'; then
    echo "restore database already exists" >&2
    exit 3
fi

source_digest="$(content_digest mytechpulse | sha256sum | awk '{print $1}')"

docker compose exec -T db createdb -U postgres "$restore_db"
docker compose exec -T db pg_restore -U postgres -d "$restore_db" --exit-on-error < "$dump_path"

restored_digest="$(content_digest "$restore_db" | sha256sum | awk '{print $1}')"
if [ "$source_digest" != "$restored_digest" ]; then
    echo "restored content digest mismatch" >&2
    exit 4
fi

printf 'OK: restored content digest verified\n'
