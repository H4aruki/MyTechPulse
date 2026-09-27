\set ON_ERROR_STOP on
BEGIN READ ONLY;

SELECT to_regclass('public."user"') IS NOT NULL AS has_user,
       to_regclass('public.tag') IS NOT NULL AS has_tag,
       to_regclass('public.recommend') IS NOT NULL AS has_recommend;

SELECT table_name, column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name IN ('user', 'tag', 'recommend')
ORDER BY table_name, ordinal_position;

SELECT c.conrelid::regclass::text AS table_name,
       c.conname AS constraint_name,
       c.contype AS constraint_type,
       pg_get_constraintdef(c.oid) AS constraint_definition
FROM pg_constraint AS c
JOIN pg_namespace AS n ON n.oid = c.connamespace
WHERE n.nspname = 'public'
  AND c.conrelid IN ('public."user"'::regclass, 'public.tag'::regclass,
                     'public.recommend'::regclass)
  AND c.contype IN ('p', 'f', 'u')
ORDER BY table_name, constraint_type, constraint_name;

SELECT 'user' AS table_name, count(*) AS row_count FROM public."user"
UNION ALL
SELECT 'tag', count(*) FROM public.tag
UNION ALL
SELECT 'recommend', count(*) FROM public.recommend;

WITH serial_columns AS (
    SELECT 'user'::text AS table_name,
           'user_ID'::text AS column_name,
           pg_get_serial_sequence('public."user"', 'user_ID') AS sequence_name,
           (SELECT max("user_ID")::bigint FROM public."user") AS max_id
    UNION ALL
    SELECT 'tag', 'tag_ID',
           pg_get_serial_sequence('public.tag', 'tag_ID'),
           (SELECT max("tag_ID")::bigint FROM public.tag)
)
SELECT s.table_name,
       s.column_name,
       s.sequence_name,
       s.max_id,
       q.last_value,
       q.increment_by,
       CASE
           WHEN q.last_value IS NULL THEN q.start_value
           ELSE q.last_value + q.increment_by
       END AS next_value,
       CASE
           WHEN s.max_id IS NULL THEN true
           WHEN q.last_value IS NULL THEN q.start_value > s.max_id
           ELSE q.last_value + q.increment_by > s.max_id
       END AS next_value_exceeds_max_id
FROM serial_columns AS s
LEFT JOIN pg_sequences AS q
  ON q.schemaname = 'public'
 AND to_regclass(format('%I.%I', q.schemaname, q.sequencename)) =
     to_regclass(s.sequence_name)
ORDER BY s.table_name;

COMMIT;
