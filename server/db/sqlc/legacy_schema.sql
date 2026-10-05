-- sqlcだけが読む既存3表の定義。goose(実行用のマイグレーション)は読まない。
-- 00001_legacy_baseline.sql はDO文の中でテーブルを作るため、sqlcがテーブルを認識できない。
-- そのため同じ定義をここへ写している。00001の定義を変える場合は、この内容も合わせて直す。
CREATE TABLE "user" (
    "user_ID" serial PRIMARY KEY,
    user_name varchar(50) NOT NULL UNIQUE,
    password varchar(255) NOT NULL
);

CREATE TABLE tag (
    "tag_ID" serial PRIMARY KEY,
    tag_name varchar(50) NOT NULL UNIQUE
);

CREATE TABLE recommend (
    "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
    "tag_ID" integer NOT NULL REFERENCES tag("tag_ID") ON DELETE CASCADE,
    match_int integer NOT NULL,
    PRIMARY KEY ("user_ID", "tag_ID")
);
