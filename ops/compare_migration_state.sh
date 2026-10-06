#!/usr/bin/env bash
# 2つのsnapshot（ops/snapshot_migration_state.sh が作ったJSON）を比べる。
#
# 使い方: bash ops/compare_migration_state.sh BEFORE_JSON AFTER_JSON [--allow-sequence-advance]
#   BEFORE_JSON  migration前（または比較の基準）のsnapshot
#   AFTER_JSON   migration後（またはcleanup後）のsnapshot
#   --allow-sequence-advance  合成writeでIDの採番だけが進んだcleanup後の比較で使う。
#                             採番値は進んでよいが、戻ってはいけない
# 入力（環境変数）: MTP_SNAPSHOT_DB、任意でMTP_SNAPSHOT_COMPOSE_ARGS（snapshot側と同じ）。
# 比較には、対象DBの一時表だけを使い、何も書き込まない。
# 公開する出力は {"matches":..., "mismatched_tables":N} の1行だけ。
# 終了コード: 0=一致、1=不一致、2=検査不能（入力不正・別実行のsnapshot・DB接続失敗など）
set -euo pipefail
set +x
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

err_file=""
out_file=""
tmp_dir=""

cleanup() {
    rm -f -- "$err_file" "$out_file"
    [ -z "$tmp_dir" ] || rmdir -- "$tmp_dir" 2>/dev/null || true
}
trap cleanup EXIT

fail() {
    printf 'compare: unable (%s)\n' "$1" >&2
    exit "${2:-2}"
}

# shellcheck source=ops/lib/migration_state_common.sh
source "$repo_root/ops/lib/migration_state_common.sh"

before_file="${1:-}"
after_file="${2:-}"
allow_advance=0
case "${3:-}" in
    "") ;;
    --allow-sequence-advance) allow_advance=1 ;;
    *) fail E_CONFIG ;;
esac
[ -n "$before_file" ] && [ -n "$after_file" ] || fail E_CONFIG
[ "$#" -le 3 ] || fail E_CONFIG
validate_db_name

for f in "$before_file" "$after_file"; do
    [ -f "$f" ] && [ ! -L "$f" ] || fail E_INPUT
    # snapshotは1行のJSON。複数行や空はCOPYの区切りを壊すため受け付けない
    [ "$(wc -l <"$f")" -eq 1 ] || fail E_INPUT
done

# 保護した一時fileの置き場所（出力はsnapshot同様に機密扱い）
tmp_dir="$(mktemp -d)"
chmod 700 -- "$tmp_dir"
err_file="$tmp_dir/err"
out_file="$tmp_dir/out"
: >"$err_file"
: >"$out_file"

require_safe_server_logging

# JSONは標準入力のCOPYだけで一時表へ渡す。COPYのtext形式のため、backslashは二重にする
if ! {
    printf '%s\n' \
        'CREATE TEMP TABLE snap (label text NOT NULL, doc text NOT NULL);' \
        'COPY snap (label, doc) FROM STDIN;'
    printf 'before\t'
    sed -e 's/\\/\\\\/g' -- "$before_file"
    printf 'after\t'
    sed -e 's/\\/\\\\/g' -- "$after_file"
    printf '\\.\n'
    cat <<'SQL'
WITH pair AS (
    SELECT (SELECT doc::jsonb FROM snap WHERE label = 'before') AS b,
           (SELECT doc::jsonb FROM snap WHERE label = 'after') AS a
),
tables3(t) AS (VALUES ('user'), ('tag'), ('recommend')),
-- 件数。値が無いsnapshotは不一致として扱う（NULL同士を一致にしない）
bad_counts AS (
    SELECT t FROM tables3, pair
    WHERE b #>> ARRAY['counts', t] IS NULL
       OR a #>> ARRAY['counts', t] IS NULL
       OR b #>> ARRAY['counts', t] <> a #>> ARRAY['counts', t]
),
-- 内容digest
bad_digests AS (
    SELECT t FROM tables3, pair
    WHERE b #>> ARRAY['table_digests', t] IS NULL
       OR a #>> ARRAY['table_digests', t] IS NULL
       OR b #>> ARRAY['table_digests', t] <> a #>> ARRAY['table_digests', t]
),
-- migration前にあった制約が、後で名前も定義も同じまま残っていること
bad_lost_constraints AS (
    SELECT c ->> 'table' AS t
    FROM pair, jsonb_array_elements(b -> 'constraints') AS c
    WHERE NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(a -> 'constraints') AS x
        WHERE x ->> 'table' = c ->> 'table'
          AND x ->> 'name' = c ->> 'name'
          AND x ->> 'definition' = c ->> 'definition')
),
-- 必須の制約。両方に要るもの（既存3表のPK・unique・FK）と、migration後にだけ要るもの
required (side, t, key, name) AS (VALUES
    ('both', 'user', 'user|p|user_ID', NULL::text),
    ('both', 'user', 'user|u|user_name', NULL),
    ('both', 'tag', 'tag|p|tag_ID', NULL),
    ('both', 'tag', 'tag|u|tag_name', NULL),
    ('both', 'recommend', 'recommend|p|user_ID,tag_ID', NULL),
    ('both', 'recommend', 'recommend|f|user_ID>user.user_ID:c', NULL),
    ('both', 'recommend', 'recommend|f|tag_ID>tag.tag_ID:c', NULL),
    ('after', 'user', NULL, 'user_role_check'),
    ('after', 'auth_session', 'auth_session|p|token_hash', NULL),
    ('after', 'auth_session', 'auth_session|f|user_ID>user.user_ID:c', NULL)
),
bad_required AS (
    SELECT r.t FROM required AS r, pair
    WHERE r.side = 'both'
      AND NOT EXISTS (
          SELECT 1 FROM jsonb_array_elements(b -> 'constraints') AS x
          WHERE x ->> 'table' = r.t
            AND (r.key IS NULL OR x ->> 'key' = r.key)
            AND (r.name IS NULL OR x ->> 'name' = r.name))
    UNION ALL
    SELECT r.t FROM required AS r, pair
    WHERE NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(a -> 'constraints') AS x
        WHERE x ->> 'table' = r.t
          AND (r.key IS NULL OR x ->> 'key' = r.key)
          AND (r.name IS NULL OR x ->> 'name' = r.name))
),
-- sequence。次に採番される値が既存IDの最大値を超えていること
seq AS (
    SELECT s AS item FROM pair, jsonb_array_elements(b -> 'sequences') AS s
    UNION ALL
    SELECT s FROM pair, jsonb_array_elements(a -> 'sequences') AS s
),
bad_seq_state AS (
    SELECT item ->> 'table' AS t FROM seq
    WHERE item ->> 'start_value' IS NULL
       OR (item ->> 'max_id' IS NOT NULL
           AND coalesce((item ->> 'last_value')::bigint + (item ->> 'increment_by')::bigint,
                        (item ->> 'start_value')::bigint) <= (item ->> 'max_id')::bigint)
),
-- user/tagのsequenceが両方のsnapshotにあること
bad_seq_missing AS (
    SELECT t FROM tables3, pair
    WHERE t IN ('user', 'tag')
      AND (NOT EXISTS (SELECT 1 FROM jsonb_array_elements(b -> 'sequences') AS s
                       WHERE s ->> 'table' = t)
        OR NOT EXISTS (SELECT 1 FROM jsonb_array_elements(a -> 'sequences') AS s
                       WHERE s ->> 'table' = t))
),
-- 前後で採番状態が変わっていないこと（cleanup後だけ、進むのは許す）
bad_seq_drift AS (
    SELECT bs ->> 'table' AS t
    FROM pair, jsonb_array_elements(b -> 'sequences') AS bs
    LEFT JOIN LATERAL (
        SELECT x FROM jsonb_array_elements(a -> 'sequences') AS x
        WHERE x ->> 'table' = bs ->> 'table' AND x ->> 'column' = bs ->> 'column'
    ) AS m ON true
    WHERE m.x IS NULL
       OR CASE WHEN :'allow_advance' = '1'
               THEN (m.x ->> 'last_value' IS NULL AND bs ->> 'last_value' IS NOT NULL)
                    OR (m.x ->> 'last_value')::bigint < (bs ->> 'last_value')::bigint
               ELSE (m.x ->> 'last_value') IS DISTINCT FROM (bs ->> 'last_value')
          END
),
all_bad AS (
    SELECT t FROM bad_counts
    UNION ALL SELECT t FROM bad_digests
    UNION ALL SELECT t FROM bad_lost_constraints
    UNION ALL SELECT t FROM bad_required
    UNION ALL SELECT t FROM bad_seq_state
    UNION ALL SELECT t FROM bad_seq_missing
    UNION ALL SELECT t FROM bad_seq_drift
)
SELECT CASE
    WHEN b IS NULL OR a IS NULL THEN 'E_INPUT|0'
    WHEN b ->> 'schema_version' IS DISTINCT FROM '1'
      OR a ->> 'schema_version' IS DISTINCT FROM '1' THEN 'E_SCHEMA|0'
    WHEN b ->> 'run_id' IS NULL
      OR b ->> 'run_id' IS DISTINCT FROM a ->> 'run_id' THEN 'E_RUN|0'
    ELSE (SELECT (count(DISTINCT t) = 0)::text || '|' || count(DISTINCT t)::text FROM all_bad)
END
FROM pair;
SQL
} | run_psql -v "allow_advance=$allow_advance" >"$out_file" 2>"$err_file"; then
    fail E_DB
fi

result="$(tr -d '\r\n' <"$out_file")"
case "$result" in
    true\|0)
        printf '{"matches":true,"mismatched_tables":0}\n'
        ;;
    false\|[1-9]*)
        printf '{"matches":false,"mismatched_tables":%s}\n' "${result#false|}"
        exit 1
        ;;
    E_INPUT\|0 | E_SCHEMA\|0 | E_RUN\|0)
        fail "${result%|0}"
        ;;
    *)
        fail E_DB
        ;;
esac
