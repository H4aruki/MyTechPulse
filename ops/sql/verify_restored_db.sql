\set ON_ERROR_STOP on

DO $$
DECLARE
    user_sequence text;
    tag_sequence text;
    user_next_value bigint;
    tag_next_value bigint;
BEGIN
    IF to_regclass('public."user"') IS NULL
       OR to_regclass('public.tag') IS NULL
       OR to_regclass('public.recommend') IS NULL THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF (SELECT count(*)
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name IN ('user', 'tag', 'recommend')) <> 8 THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (
            VALUES
                ('user'::text, 'user_ID'::text, 'integer'::text, NULL::integer),
                ('user', 'user_name', 'character varying', 50),
                ('user', 'password', 'character varying', 255),
                ('tag', 'tag_ID', 'integer', NULL::integer),
                ('tag', 'tag_name', 'character varying', 50),
                ('recommend', 'user_ID', 'integer', NULL::integer),
                ('recommend', 'tag_ID', 'integer', NULL::integer),
                ('recommend', 'match_int', 'integer', NULL::integer)
        ) AS expected(table_name, column_name, data_type, max_length)
        LEFT JOIN information_schema.columns AS actual
          ON actual.table_schema = 'public'
         AND actual.table_name = expected.table_name
         AND actual.column_name = expected.column_name
        WHERE actual.column_name IS NULL
           OR actual.data_type IS DISTINCT FROM expected.data_type
           OR actual.is_nullable IS DISTINCT FROM 'NO'
           OR actual.character_maximum_length IS DISTINCT FROM expected.max_length
    ) THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF (SELECT count(*) FROM pg_constraint
        WHERE conrelid IN ('public."user"'::regclass, 'public.tag'::regclass,
                           'public.recommend'::regclass)
          AND contype = 'p') <> 3
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public."user"'::regclass
             AND c.contype = 'p'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'user_ID'
                                     AND NOT attisdropped)]::smallint[]
       )
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public.tag'::regclass
             AND c.contype = 'p'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'tag_ID'
                                     AND NOT attisdropped)]::smallint[]
       )
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public.recommend'::regclass
             AND c.contype = 'p'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'user_ID'
                                     AND NOT attisdropped),
                               (SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'tag_ID'
                                     AND NOT attisdropped)]::smallint[]
       ) THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF (SELECT count(*) FROM pg_constraint
        WHERE conrelid IN ('public."user"'::regclass, 'public.tag'::regclass,
                           'public.recommend'::regclass)
          AND contype = 'u') <> 2
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public."user"'::regclass
             AND c.contype = 'u'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'user_name'
                                     AND NOT attisdropped)]::smallint[]
       )
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public.tag'::regclass
             AND c.contype = 'u'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'tag_name'
                                     AND NOT attisdropped)]::smallint[]
       ) THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF (SELECT count(*) FROM pg_constraint
        WHERE conrelid = 'public.recommend'::regclass
          AND contype = 'f') <> 2
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public.recommend'::regclass
             AND c.contype = 'f'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'user_ID'
                                     AND NOT attisdropped)]::smallint[]
             AND c.confrelid = 'public."user"'::regclass
             AND c.confkey = ARRAY[(SELECT attnum FROM pg_attribute
                                    WHERE attrelid = c.confrelid
                                      AND attname = 'user_ID'
                                      AND NOT attisdropped)]::smallint[]
             AND c.confdeltype = 'c'
       )
       OR NOT EXISTS (
           SELECT 1 FROM pg_constraint AS c
           WHERE c.conrelid = 'public.recommend'::regclass
             AND c.contype = 'f'
             AND c.conkey = ARRAY[(SELECT attnum FROM pg_attribute
                                   WHERE attrelid = c.conrelid
                                     AND attname = 'tag_ID'
                                     AND NOT attisdropped)]::smallint[]
             AND c.confrelid = 'public.tag'::regclass
             AND c.confkey = ARRAY[(SELECT attnum FROM pg_attribute
                                    WHERE attrelid = c.confrelid
                                      AND attname = 'tag_ID'
                                      AND NOT attisdropped)]::smallint[]
             AND c.confdeltype = 'c'
       ) THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    user_sequence := pg_get_serial_sequence('public."user"', 'user_ID');
    tag_sequence := pg_get_serial_sequence('public.tag', 'tag_ID');
    IF user_sequence IS NULL OR tag_sequence IS NULL THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    SELECT CASE WHEN last_value IS NULL THEN start_value
                ELSE last_value + increment_by END
    INTO user_next_value
    FROM pg_sequences
    WHERE schemaname = 'public'
      AND to_regclass(format('%I.%I', schemaname, sequencename)) =
          to_regclass(user_sequence);

    SELECT CASE WHEN last_value IS NULL THEN start_value
                ELSE last_value + increment_by END
    INTO tag_next_value
    FROM pg_sequences
    WHERE schemaname = 'public'
      AND to_regclass(format('%I.%I', schemaname, sequencename)) =
          to_regclass(tag_sequence);

    IF user_next_value IS NULL OR tag_next_value IS NULL
       OR user_next_value <= COALESCE((SELECT max("user_ID") FROM public."user"), 0)
       OR tag_next_value <= COALESCE((SELECT max("tag_ID") FROM public.tag), 0) THEN
        RAISE EXCEPTION 'restored schema mismatch';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM recommend AS r
        LEFT JOIN public."user" AS u ON u."user_ID" = r."user_ID"
        LEFT JOIN public.tag AS t ON t."tag_ID" = r."tag_ID"
        WHERE u."user_ID" IS NULL OR t."tag_ID" IS NULL
    ) THEN
        RAISE EXCEPTION 'orphan recommend row';
    END IF;
END $$;
