# MyTechPulse フロントエンド

Vite + React + TypeScript 構成のフロントエンド。素のHTML/CSS/JS から刷新したもの（Issue #48）。

## 構成

SEOが効く範囲と効かない範囲を分けたハイブリッド構成になっている。

| パス | 実体 | 方式 |
| --- | --- | --- |
| `/` | `index.html` | 静的HTML（実質SSG）。Reactを読み込まないJSゼロのページ。検索インデックスとOGPのために事前生成された状態を保つ |
| `/app/*` | `app/index.html` + `src/` | React SPA。認証必須のためインデックス対象外（`noindex`） |

ログイン後の画面はユーザーごとにパーソナライズされインデックス不可なので、SSRサーバーは持たない。バックエンド（FastAPI）と合わせて「静的フロント + API」の2ピース構成。

## 開発

```bash
npm install
npm run dev      # http://localhost:5173
npm run build    # 型チェック（tsc -b）+ 本番ビルド
npm run preview  # ビルド結果の確認
```

APIのベースURLは環境変数で切り替える。`.env.example` を参照。

```
VITE_API_BASE_URL=http://127.0.0.1:8000
```

バックエンドを起動していないと、ログイン以降の画面は動作しない（リポジトリルートの `CONTRIBUTING.md` と `README.md` を参照）。

## ディレクトリ

```
src/
├── api/          # バックエンドAPIの型定義と呼び出し（client.ts に共通処理を集約）
├── components/   # 画面をまたぐUI部品（ヘッダー・フッター・記事カード等）
├── constants/    # タグ定義など静的データ
├── lib/          # トークン保管など横断的な処理
└── pages/        # ルーティング単位の画面
```

## 実装上の注意

- **APIは `status`（int）で結果を返す規約**。HTTPステータスではない（401だけは例外）。`src/api/types.ts` の `ApiStatus` を参照し、値の意味を変える場合はバックエンドと必ず揃えること
- **JWTは `localStorage` に保管している**。XSS耐性の観点から httpOnly Cookie への移行を Issue #46 で予定しており、移行しやすいよう `src/lib/auth.ts` に集約してある
- **タグ定義は `src/constants/tags.ts` にハードコードされている**。バックエンドの `tag` テーブルとの二重管理になっており、動的取得に切り替えるかを Issue #47 で検討中
- **SPAの深いリンク**（`/app/login` 等）を直接開くにはホスティング側の rewrite が必要。`public/_redirects`（Netlify / Cloudflare Pages 用）を用意してある。開発・preview では `vite.config.ts` の `appSpaFallback` プラグインが同じ役割を担う
