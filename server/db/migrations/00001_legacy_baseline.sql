-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    present integer;
    expected_columns text[];
    actual_columns text[];
    expected_constraints text[];
    actual_constraints text[];
BEGIN
    -- DDLより前に存在状態を一度だけ取得する。途中で作成してから判定しない
    present := (to_regclass('public."user"') IS NOT NULL)::int
             + (to_regclass('public.tag') IS NOT NULL)::int
             + (to_regclass('public.recommend') IS NOT NULL)::int;

    IF present = 0 THEN
        CREATE TABLE public."user" (
            "user_ID" serial PRIMARY KEY,
            user_name varchar(50) NOT NULL UNIQUE,
            password varchar(255) NOT NULL
        );
        CREATE TABLE public.tag (
            "tag_ID" serial PRIMARY KEY,
            tag_name varchar(50) NOT NULL UNIQUE
        );
        CREATE TABLE public.recommend (
            "user_ID" integer NOT NULL REFERENCES public."user"("user_ID") ON DELETE CASCADE,
            "tag_ID" integer NOT NULL REFERENCES public.tag("tag_ID") ON DELETE CASCADE,
            match_int integer NOT NULL,
            PRIMARY KEY ("user_ID", "tag_ID")
        );
        RETURN;
    END IF;

    IF present <> 3 THEN
        RAISE EXCEPTION 'legacy schema mismatch';
    END IF;

    -- 3表がすべて存在する場合はDDLを実行せず、現行スキーマと完全一致するか検査する
    expected_columns := ARRAY(SELECT x FROM unnest(ARRAY[
        'user.user_ID:integer:NO:',
        'user.user_name:character varying:NO:50',
        'user.password:character varying:NO:255',
        'tag.tag_ID:integer:NO:',
        'tag.tag_name:character varying:NO:50',
        'recommend.user_ID:integer:NO:',
        'recommend.tag_ID:integer:NO:',
        'recommend.match_int:integer:NO:'
    ]) AS x ORDER BY x);
    actual_columns := ARRAY(
        SELECT table_name || '.' || column_name || ':' || data_type || ':' || is_nullable || ':'
               || coalesce(character_maximum_length::text, '') AS x
        FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name IN ('user', 'tag', 'recommend')
        ORDER BY x);
    IF expected_columns IS DISTINCT FROM actual_columns THEN
        RAISE EXCEPTION 'legacy schema mismatch';
    END IF;

    expected_constraints := ARRAY(SELECT x FROM unnest(ARRAY[
        'user|p|user_ID',
        'user|u|user_name',
        'tag|p|tag_ID',
        'tag|u|tag_name',
        'recommend|p|user_ID,tag_ID',
        'recommend|f|user_ID>user.user_ID:c',
        'recommend|f|tag_ID>tag.tag_ID:c'
    ]) AS x ORDER BY x);
    -- regclassは"user"を引用符付きで返すため、比較用に外す
    actual_constraints := ARRAY(
        SELECT x FROM (
            SELECT replace(
                       c.conrelid::regclass::text || '|' || c.contype::text || '|' ||
                       (SELECT string_agg(a.attname, ',' ORDER BY k.ord)
                          FROM unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
                          JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum)
                       || CASE WHEN c.contype = 'f' THEN
                            '>' || c.confrelid::regclass::text || '.' ||
                            (SELECT a.attname FROM pg_attribute a
                              WHERE a.attrelid = c.confrelid AND a.attnum = c.confkey[1])
                            || ':' || c.confdeltype::text
                          ELSE '' END,
                       '"', '') AS x
            FROM pg_constraint c
            WHERE c.conrelid IN ('public."user"'::regclass, 'public.tag'::regclass, 'public.recommend'::regclass)
              AND c.contype IN ('p', 'u', 'f')
        ) AS s ORDER BY x);
    IF expected_constraints IS DISTINCT FROM actual_constraints THEN
        RAISE EXCEPTION 'legacy schema mismatch';
    END IF;

    IF pg_get_serial_sequence('public."user"', 'user_ID') IS NULL
       OR pg_get_serial_sequence('public.tag', 'tag_ID') IS NULL THEN
        RAISE EXCEPTION 'legacy schema mismatch';
    END IF;
END
$$;
-- +goose StatementEnd
