\set ON_ERROR_STOP on
BEGIN READ ONLY;

SELECT count(*) AS collision_groups,
       count(*) = 0 AS can_continue
FROM (
    SELECT lower(btrim(tag_name))
    FROM public.tag
    GROUP BY lower(btrim(tag_name))
    HAVING count(*) > 1
) AS collisions
\gset audit_
\echo :audit_collision_groups

COMMIT;
\if :audit_can_continue
\else
    DO $$ BEGIN RAISE EXCEPTION 'tag normalization collision'; END $$;
\endif
