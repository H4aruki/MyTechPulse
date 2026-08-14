import { defineConfig, type Connect, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'

/**
 * Vite 既定のSPAフォールバックは未知のパスをルートの index.html（＝LP）へ返してしまい、
 * /app/login のような深いリンクを直接開くとLPが表示される。
 * /app 配下だけはSPAのエントリへ差し戻す。
 *
 * 本番でも同等の書き換えがホスティング側に必要（public/_redirects を参照）。
 */
function appSpaFallback(): Plugin {
  const rewrite: Connect.NextHandleFunction = (req, _res, next) => {
    const url = req.url?.split('?')[0]
    // 拡張子付き（アセット要求）は対象外にして、HTMLへのナビゲーションだけを差し戻す
    if (url && /^\/app(\/|$)/.test(url) && !url.slice(1).includes('.')) {
      req.url = '/app.html'
    }
    next()
  }

  return {
    name: 'app-spa-fallback',
    configureServer(server) {
      server.middlewares.use(rewrite)
    },
    configurePreviewServer(server) {
      server.middlewares.use(rewrite)
    },
  }
}

// LP（index.html）は静的HTMLのまま事前生成し、アプリ本体（app.html）だけを
// React SPA として配信するマルチページ構成。SSRサーバーを持たずにLPのSEO/OGPを確保する。
//
// SPAのエントリを `app/index.html` ではなく `app.html` に置いているのは Cloudflare Pages の
// 制約による。Pages は書き換え先から `.html` と `/index` を剥がして正規化するため、
// `/app/*  /app/index.html  200` は「書き換え先が自分のパターンに再び一致する」ループと
// 判定されてルールごと捨てられる（ローカルの `wrangler pages dev` で
// "Infinite loop detected in this rule and has been ignored" を確認済み）。
// 出力を `/app.html`（配信URLは `/app`）にすることでループ判定を回避している。
export default defineConfig({
  plugins: [react(), tailwindcss(), appSpaFallback()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      input: {
        lp: fileURLToPath(new URL('./index.html', import.meta.url)),
        app: fileURLToPath(new URL('./app.html', import.meta.url)),
      },
    },
  },
})
