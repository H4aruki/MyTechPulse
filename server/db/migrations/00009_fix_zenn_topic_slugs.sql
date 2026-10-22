-- +goose Up
-- 取得対象タグのうち11件は、zenn_topic（00008でタグ名を小文字にして空白を除いた値）がZennのトピック名と違い、
-- 本番の一括取得で記事が0件だった。zenn_topic だけを、Zennに実在するトピック名へ直す。
-- 表示用のタグ名（tag.tag_name）は変えない。
-- 今の誤った値の行だけを書き換える（運用で直した行や、他のタグの行には触れない。2回流しても同じ結果）。
UPDATE fetch_tags f
SET zenn_topic = v.new_topic
FROM (VALUES
    ('next.js',                 'nextjs'),
    ('ci/cd',                   'cicd'),
    ('node.js',                 'nodejs'),
    ('scikit-learn',            'scikitlearn'),
    ('自然言語処理（nlp）',     'nlp'),
    ('tdd（テスト駆動開発）',   'tdd'),
    ('c++',                     'cpp'),
    ('c#',                      'csharp'),
    ('vue.js',                  'vue'),
    ('nuxt.js',                 'nuxtjs'),
    ('sass/scss',               'scss')
) AS v(old_topic, new_topic)
WHERE f.zenn_topic = v.old_topic;

-- +goose Down
-- 開発環境で作り直すためのもの。本番では使わない。
-- 新しいトピック名（nodejs など）は他のタグでも使いうるため、タグ名とトピック名の両方が合う行だけを戻す。
UPDATE fetch_tags f
SET zenn_topic = v.old_topic
FROM tag t, (VALUES
    ('Next.js',               'next.js',               'nextjs'),
    ('CI/CD',                 'ci/cd',                 'cicd'),
    ('Node.js',               'node.js',               'nodejs'),
    ('scikit-learn',          'scikit-learn',          'scikitlearn'),
    ('自然言語処理（NLP）',   '自然言語処理（nlp）',   'nlp'),
    ('TDD（テスト駆動開発）', 'tdd（テスト駆動開発）', 'tdd'),
    ('C++',                   'c++',                   'cpp'),
    ('C#',                    'c#',                    'csharp'),
    ('Vue.js',                'vue.js',                'vue'),
    ('Nuxt.js',               'nuxt.js',               'nuxtjs'),
    ('Sass/SCSS',             'sass/scss',             'scss')
) AS v(tag_name, old_topic, new_topic)
WHERE f."tag_ID" = t."tag_ID"
  AND lower(btrim(t.tag_name)) = lower(btrim(v.tag_name))
  AND f.zenn_topic = v.new_topic;
