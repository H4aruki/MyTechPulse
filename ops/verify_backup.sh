#!/usr/bin/env bash
set -euo pipefail

dump_path="${1:?usage: verify_backup.sh DUMP_FILE}"

test -f "$dump_path"
test -f "$dump_path.sha256"

(cd "$(dirname "$dump_path")" && sha256sum -c "$(basename "$dump_path").sha256")
docker compose exec -T db pg_restore --list < "$dump_path" > /dev/null

printf 'OK: backup verified\n'
