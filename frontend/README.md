# MyTechPulse フロントエンド

Vite + React + TypeScript 構成のフロントエンド。素のHTML/CSS/JS から刷新したもの（Issue #48）。

## 構成

SEOが効く範囲と効かない範囲を分けたハイブリッド構成になっている。

| パス | 実体 | スタイル | 方式 |
| --- | --- | --- | --- |
| `/` | `index.html` | `src/lp.css` | 静的HTML（実質SSG）。Reactを読み込まないJSゼロのページ。検索インデックスとOGPのために事前生成された状態を保つ |
| `/app`・`/app/*` | `app.html` + `src/` | `src/index.css` | React SPA。認証必須のためインデックス対象外（`noindex`） |

ログイン後の画面はユーザーごとにパーソナライズされインデックス不可なので、SSRサーバーは持たない。バックエンド（Go製API）と合わせて「静的フロント + API」の2ピース構成。

CSSのエントリはLPとSPAで分かれている。LPをFigmaの新デザイン（sky系）に刷新した際（Issue #80）、SPAは旧配色（`brand-*`）のまま残したためで、デザイントークンが別系統になっている。**LPだけを触るときは `src/lp.css`、ログイン後の画面を触るときは `src/index.css`** を見ること。将来SPAも同じデザインに揃える場合は、この2ファイルを統合するのが自然な着地点になる。

SPAのエントリを `app/index.html` ではなく `app.html` に置いているのは Cloudflare Pages の制約による。Pages は `_redirects` の書き換え先から `.html` と `/index` を剥がして正規化するため、`/app/*  /app/index.html  200` は書き換え先が自分のパターンに再度一致するループと判定され、ルールごと無視される。`npx wrangler pages dev dist` を実行すると `Infinite loop detected in this rule and has been ignored` として再現できる。

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
├── lib/          # ログイン状態の確認など横断的な処理
└── pages/        # ルーティング単位の画面
```

## 実装上の注意

- **APIの成否はHTTPステータスで判断する**。失敗の本文は共通の形（Problem Details）で、`code` で種類を見分ける。401だけは `UnauthorizedError` として扱い、ログイン画面へ戻す（`src/api/client.ts`）。詳細は `docs/BasicDesignSpecifications/API/ApiCommonRules.md`
- **ログイン状態はHttpOnlyのCookieで保つ**。画面側のプログラムは合言葉に触れず、すべての呼び出しで `credentials: 'include'` と、書き込み操作用の専用ヘッダー `X-MTP-CSRF` を付ける（`src/api/client.ts`）。ログイン中かどうかは `/api/v1/auth/me` で確認する（`src/lib/auth.ts`）
- **APIの型は自動で作る**。`server/openapi/openapi.json` から `npm run api:generate` で `src/api/generated.ts` を作り、`npm run api:check` で古くなっていないかを確かめる。手で書き換えない
- **タグ定義は `src/constants/tags.ts` にハードコードされている**。バックエンドの `tag` テーブルとの二重管理になっており、動的取得に切り替えるかを Issue #47 で検討中
- **SPAの深いリンク**（`/app/login` 等）を直接開くにはホスティング側の rewrite が必要。`public/_redirects`（Netlify / Cloudflare Pages 用）を用意してある。開発・preview では `vite.config.ts` の `appSpaFallback` プラグインが同じ役割を担う
