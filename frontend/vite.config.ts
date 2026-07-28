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
      req.url = '/app/index.html'
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

// LP（index.html）は静的HTMLのまま事前生成し、アプリ本体（app/index.html）だけを
// React SPA として配信するマルチページ構成。SSRサーバーを持たずにLPのSEO/OGPを確保する。
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
        app: fileURLToPath(new URL('./app/index.html', import.meta.url)),
      },
    },
  },
})
