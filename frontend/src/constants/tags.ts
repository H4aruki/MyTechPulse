/**
 * サインアップ時に選択できる興味タグの定義。
 *
 * 旧 html/signup.html にチェックボックスとして直接ハードコードされていたものを移設した。
 * バックエンドの tag テーブルとの二重管理になっている状態は変わっていないため、
 * API からの動的取得に切り替えるかどうかを Issue #47 で検討する。
 */

export interface TagCategory {
  /** カテゴリ識別子（旧HTMLの data-category を踏襲） */
  id: string
  emoji: string
  label: string
  tags: string[]
}

export const TAG_CATEGORIES: TagCategory[] = [
  {
    id: 'language',
    emoji: '🖥️',
    label: 'プログラミング言語',
    tags: [
      'Python',
      'JavaScript',
      'TypeScript',
      'Java',
      'C',
      'C++',
      'C#',
      'Go',
      'Rust',
      'Ruby',
      'PHP',
      'Swift',
      'Kotlin',
      'Scala',
      'Dart',
      'R',
      'Haskell',
      'Perl',
      'Elixir',
      'Julia',
    ],
  },
  {
    id: 'frontend',
    emoji: '🌐',
    label: 'フロントエンド',
    tags: [
      'React',
      'Next.js',
      'Vue.js',
      'Nuxt.js',
      'Angular',
      'Svelte',
      'Astro',
      'TailwindCSS',
      'Bootstrap',
      'CSS',
      'Sass/SCSS',
      'HTML',
      'Webpack',
      'Vite',
    ],
  },
  {
    id: 'backend',
    emoji: '⚙️',
    label: 'バックエンド / フレームワーク',
    tags: [
      'Node.js',
      'Express',
      'FastAPI',
      'Django',
      'Flask',
      'Spring Boot',
      'Ruby on Rails',
      'Laravel',
      'NestJS',
      'GraphQL',
    ],
  },
  {
    id: 'database',
    emoji: '📦',
    label: 'データベース / ストレージ',
    tags: [
      'MySQL',
      'PostgreSQL',
      'SQLite',
      'MongoDB',
      'Redis',
      'Firebase',
      'Supabase',
      'DynamoDB',
      'Elasticsearch',
      'Oracle Database',
    ],
  },
  {
    id: 'cloud',
    emoji: '☁️',
    label: 'クラウド / インフラ',
    tags: [
      'AWS',
      'GCP',
      'Azure',
      'Docker',
      'Kubernetes',
      'Terraform',
      'Ansible',
      'Cloudflare',
      'Netlify',
      'Vercel',
    ],
  },
  {
    id: 'devtools',
    emoji: '🛠️',
    label: '開発ツール / CI/CD',
    tags: [
      'Git',
      'GitHub',
      'GitLab',
      'Bitbucket',
      'CircleCI',
      'Jenkins',
      'GitHub Actions',
      'CI/CD',
      'DevOps',
    ],
  },
  {
    id: 'ai',
    emoji: '🤖',
    label: 'AI / データサイエンス',
    tags: [
      '機械学習',
      '深層学習',
      '人工知能',
      'TensorFlow',
      'PyTorch',
      'scikit-learn',
      'OpenAI',
      'LLM',
      'データ分析',
      '自然言語処理（NLP）',
    ],
  },
  {
    id: 'mobile',
    emoji: '📱',
    label: 'モバイル開発',
    tags: ['iOS', 'Android', 'Flutter', 'React Native', 'SwiftUI', 'Jetpack Compose'],
  },
  {
    id: 'other',
    emoji: '🧑‍💻',
    label: 'その他エンジニアリング関連',
    tags: [
      'アルゴリズム',
      'データ構造',
      'セキュリティ',
      '暗号化',
      'テスト自動化',
      'TDD（テスト駆動開発）',
      'Clean Architecture',
      'デザインパターン',
      'リファクタリング',
      'アジャイル開発',
      'Scrum',
    ],
  },
]
