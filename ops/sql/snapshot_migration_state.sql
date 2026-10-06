-- 移行前後の状態を1つのJSONにまとめる（読み取り専用）。
-- 呼び出し側（ops/snapshot_migration_state.sh）が、先に一時表 snapshot_nonce(n) へ
-- 秘密nonceを1行だけ入れておく。利用者名・password・tag名などの生の値は返さない。
-- 内容の比較には、DB内で計算したtable単位の集約digestだけを使う。
\set ON_ERROR_STOP on
BEGIN READ ONLY;

WITH user_rows AS (
    SELECT u."user_ID" AS id,
           encode(sha256(convert_to(n || ':row:user:' ||
               jsonb_build_array(u."user_ID", u.user_name, u.password)::text,
               'UTF8')), 'hex') AS row_digest
    FROM public."user" AS u CROSS JOIN snapshot_nonce
),
tag_rows AS (
    SELECT t."tag_ID" AS id,
           encode(sha256(convert_to(n || ':row:tag:' ||
               jsonb_build_array(t."tag_ID", t.tag_name)::text,
               'UTF8')), 'hex') AS row_digest
    FROM public.tag AS t CROSS JOIN snapshot_nonce
),
recommend_rows AS (
    SELECT r."user_ID" AS user_id,
           r."tag_ID" AS tag_id,
           encode(sha256(convert_to(n || ':row:recommend:' ||
               jsonb_build_array(r."user_ID", r."tag_ID", r.match_int)::text,
               'UTF8')), 'hex') AS row_digest
    FROM public.recommend AS r CROSS JOIN snapshot_nonce
),
digests AS (
    SELECT jsonb_build_object(
        'user', encode(sha256(convert_to(n || ':table:user:' ||
            coalesce((SELECT string_agg(row_digest, '' ORDER BY id) FROM user_rows), ''),
            'UTF8')), 'hex'),
        'tag', encode(sha256(convert_to(n || ':table:tag:' ||
            coalesce((SELECT string_agg(row_digest, '' ORDER BY id) FROM tag_rows), ''),
            'UTF8')), 'hex'),
        'recommend', encode(sha256(convert_to(n || ':table:recommend:' ||
            coalesce((SELECT string_agg(row_digest, '' ORDER BY user_id, tag_id)
                        FROM recommend_rows), ''),
            'UTF8')), 'hex')
    ) AS table_digests,
    -- 他の実行のsnapshotと混ぜないための、秘密ではない識別子
    left(encode(sha256(convert_to(n || ':run-id', 'UTF8')), 'hex'), 16) AS run_id
    FROM snapshot_nonce
),
constraint_rows AS (
    SELECT jsonb_build_object(
               'table', replace(c.conrelid::regclass::text, '"', ''),
               'name', c.conname,
               'type', c.contype::text,
               -- 名前に依存しない形。migrationの既存スキーマ検査と同じ規則
               'key', replace(
                   c.conrelid::regclass::text || '|' || c.contype::text || '|' ||
                   coalesce((SELECT string_agg(a.attname, ',' ORDER BY k.ord)
                               FROM unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
                               JOIN pg_attribute a
                                 ON a.attrelid = c.conrelid AND a.attnum = k.attnum), '')
                   || CASE WHEN c.contype = 'f' THEN
                        '>' || c.confrelid::regclass::text || '.' ||
                        (SELECT a.attname FROM pg_attribute a
                          WHERE a.attrelid = c.confrelid AND a.attnum = c.confkey[1])
                        || ':' || c.confdeltype::text
                      ELSE '' END,
                   '"', ''),
               'definition', pg_get_constraintdef(c.oid)
           ) AS item
    FROM pg_constraint AS c
    WHERE c.contype IN ('p', 'f', 'u', 'c')
      AND c.conrelid IN (
          SELECT oid FROM pg_class
          WHERE relnamespace = 'public'::regnamespace
            AND relname IN ('user', 'tag', 'recommend', 'auth_session')
            AND relkind = 'r')
),
sequence_rows AS (
    SELECT jsonb_build_object(
               'table', s.table_name,
               'column', s.column_name,
               'last_value', q.last_value,
               'start_value', q.start_value,
               'increment_by', q.increment_by,
               'max_id', s.max_id
           ) AS item
    FROM (
        SELECT 'user'::text AS table_name, 'user_ID'::text AS column_name,
               pg_get_serial_sequence('public."user"', 'user_ID') AS sequence_name,
               (SELECT max("user_ID")::bigint FROM public."user") AS max_id
        UNION ALL
        SELECT 'tag', 'tag_ID',
               pg_get_serial_sequence('public.tag', 'tag_ID'),
               (SELECT max("tag_ID")::bigint FROM public.tag)
    ) AS s
    LEFT JOIN pg_sequences AS q
      ON q.schemaname = 'public'
     AND to_regclass(format('%I.%I', q.schemaname, q.sequencename)) =
         to_regclass(s.sequence_name)
)
SELECT jsonb_build_object(
    'schema_version', 1,
    'run_id', d.run_id,
    'counts', jsonb_build_object(
        'user', (SELECT count(*) FROM public."user"),
        'tag', (SELECT count(*) FROM public.tag),
        'recommend', (SELECT count(*) FROM public.recommend)
    ),
    'constraints', coalesce((SELECT jsonb_agg(item ORDER BY item::text) FROM constraint_rows),
                            '[]'::jsonb),
    'sequences', coalesce((SELECT jsonb_agg(item ORDER BY item::text) FROM sequence_rows),
                          '[]'::jsonb),
    'table_digests', d.table_digests
)::text
FROM digests AS d;

ROLLBACK;
