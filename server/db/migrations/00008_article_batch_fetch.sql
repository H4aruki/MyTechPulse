-- +goose Up
-- 記事の定期一括取得（Issue #66 ステップ1）。取得した記事を貯める表と、取得の履歴を残す表を足す。
-- 既存の表は変更しない。フィードはまだこの表を読まない。
-- 取得は ARTICLE_FETCH_ENABLED=true のときだけ動く（既定は無効）。

CREATE TABLE articles (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source       text NOT NULL CHECK (source IN ('qiita', 'zenn')),
    external_id  text NOT NULL,
    url          text NOT NULL UNIQUE,
    title        text NOT NULL,
    author_name  text NOT NULL DEFAULT '',
    likes        integer NOT NULL DEFAULT 0 CHECK (likes >= 0),
    published_at timestamptz NOT NULL,
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);
CREATE INDEX articles_source_published_at_idx ON articles (source, published_at);

-- 記事に付くタグ。tag 表にあるタグだけを結ぶ（外部のデータから tag の行は作らない）。
-- published_at はフィードの検索用に記事から写す。
CREATE TABLE article_tags (
    article_id   bigint  NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    "tag_ID"     integer NOT NULL REFERENCES tag ("tag_ID") ON DELETE CASCADE,
    published_at timestamptz NOT NULL,
    PRIMARY KEY (article_id, "tag_ID")
);
CREATE INDEX article_tags_tag_published_idx ON article_tags ("tag_ID", published_at DESC);

-- 取得の対象にするタグ。zenn_topic はZennのトピック名。
CREATE TABLE fetch_tags (
    "tag_ID"   integer PRIMARY KEY REFERENCES tag ("tag_ID") ON DELETE CASCADE,
    zenn_topic text NOT NULL CHECK (zenn_topic <> ''),
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 取得の実行履歴。error_message には分類済みの文だけを入れる（URL・トークン・応答本文は入れない）。
CREATE TABLE fetch_runs (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slot_at        timestamptz NOT NULL,
    trigger        text NOT NULL CHECK (trigger IN ('scheduled', 'catchup', 'retry')),
    attempt        smallint NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 3),
    started_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    status         text NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'succeeded', 'partial', 'failed')),
    error_message  text NOT NULL DEFAULT '',
    qiita_requests integer NOT NULL DEFAULT 0,
    zenn_requests  integer NOT NULL DEFAULT 0,
    upserted       integer NOT NULL DEFAULT 0,
    deleted        integer NOT NULL DEFAULT 0
);
CREATE INDEX fetch_runs_slot_idx ON fetch_runs (slot_at, started_at DESC);

-- 初期の取得対象は、会員登録の100タグ（00006と同じ一覧）。
-- zenn_topic は今のコード（zenn.TopicFor）と同じ導き方（小文字にして空白を除く）。既にある行は変えない。
INSERT INTO fetch_tags ("tag_ID", zenn_topic)
SELECT t."tag_ID", lower(regexp_replace(t.tag_name, '\s+', '', 'g'))
FROM tag t
WHERE lower(btrim(t.tag_name)) IN (
    SELECT lower(btrim(v.tag_name)) FROM (VALUES
        ('Python'),
        ('JavaScript'),
        ('TypeScript'),
        ('Java'),
        ('C'),
        ('C++'),
        ('C#'),
        ('Go'),
        ('Rust'),
        ('Ruby'),
        ('PHP'),
        ('Swift'),
        ('Kotlin'),
        ('Scala'),
        ('Dart'),
        ('R'),
        ('Haskell'),
        ('Perl'),
        ('Elixir'),
        ('Julia'),
        ('React'),
        ('Next.js'),
        ('Vue.js'),
        ('Nuxt.js'),
        ('Angular'),
        ('Svelte'),
        ('Astro'),
        ('TailwindCSS'),
        ('Bootstrap'),
        ('CSS'),
        ('Sass/SCSS'),
        ('HTML'),
        ('Webpack'),
        ('Vite'),
        ('Node.js'),
        ('Express'),
        ('FastAPI'),
        ('Django'),
        ('Flask'),
        ('Spring Boot'),
        ('Ruby on Rails'),
        ('Laravel'),
        ('NestJS'),
        ('GraphQL'),
        ('MySQL'),
        ('PostgreSQL'),
        ('SQLite'),
        ('MongoDB'),
        ('Redis'),
        ('Firebase'),
        ('Supabase'),
        ('DynamoDB'),
        ('Elasticsearch'),
        ('Oracle Database'),
        ('AWS'),
        ('GCP'),
        ('Azure'),
        ('Docker'),
        ('Kubernetes'),
        ('Terraform'),
        ('Ansible'),
        ('Cloudflare'),
        ('Netlify'),
        ('Vercel'),
        ('Git'),
        ('GitHub'),
        ('GitLab'),
        ('Bitbucket'),
        ('CircleCI'),
        ('Jenkins'),
        ('GitHub Actions'),
        ('CI/CD'),
        ('DevOps'),
        ('機械学習'),
        ('深層学習'),
        ('人工知能'),
        ('TensorFlow'),
        ('PyTorch'),
        ('scikit-learn'),
        ('OpenAI'),
        ('LLM'),
        ('データ分析'),
        ('自然言語処理（NLP）'),
        ('iOS'),
        ('Android'),
        ('Flutter'),
        ('React Native'),
        ('SwiftUI'),
        ('Jetpack Compose'),
        ('アルゴリズム'),
        ('データ構造'),
        ('セキュリティ'),
        ('暗号化'),
        ('テスト自動化'),
        ('TDD（テスト駆動開発）'),
        ('Clean Architecture'),
        ('デザインパターン'),
        ('リファクタリング'),
        ('アジャイル開発'),
        ('Scrum')
    ) AS v(tag_name)
)
ON CONFLICT ("tag_ID") DO NOTHING;

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
DROP TABLE fetch_runs;
DROP TABLE fetch_tags;
DROP TABLE article_tags;
DROP TABLE articles;
