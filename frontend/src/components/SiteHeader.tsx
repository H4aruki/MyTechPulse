import { Link } from 'react-router-dom'

interface SiteHeaderProps {
  /** ログイン後の画面ではログアウトボタンを、未ログイン時は登録/ログインボタンを出す */
  authenticated?: boolean
  onLogout?: () => void
}

/**
 * 旧実装ではヘッダーが4つのHTMLファイルに重複コピーされていたため、共通化した。
 */
export function SiteHeader({ authenticated = false, onLogout }: SiteHeaderProps) {
  return (
    <header className="sticky top-0 z-50 border-b border-slate-200 bg-white/90 backdrop-blur">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between gap-4 px-4">
        <Link
          to={authenticated ? '/articles' : '/login'}
          className="flex items-center gap-2 font-display text-lg font-bold text-ink"
        >
          <img src="/img/mytechpulse-logo2.png" alt="" className="h-8 w-8 object-contain" />
          MyTechPulse
        </Link>

        <nav className="flex items-center gap-4" aria-label="提供元サイト">
          <a href="https://qiita.com/" target="_blank" rel="noreferrer noopener" title="Qiita">
            <img src="/img/Qiita_logo.png" alt="Qiita" className="h-7 w-7 object-contain" />
          </a>
          <a href="https://zenn.dev/" target="_blank" rel="noreferrer noopener" title="Zenn">
            <img src="/img/Zenn_logo.png" alt="Zenn" className="h-7 w-7 object-contain" />
          </a>
        </nav>

        <div className="flex items-center gap-2">
          {authenticated ? (
            <button
              type="button"
              onClick={onLogout}
              className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-bold text-ink-muted transition hover:bg-slate-100"
            >
              ログアウト
            </button>
          ) : (
            <>
              <Link
                to="/signup"
                className="rounded-lg border border-slate-300 px-4 py-2 text-sm font-bold text-ink-muted transition hover:bg-slate-100"
              >
                新規登録
              </Link>
              <Link
                to="/login"
                className="rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white transition hover:bg-brand-700"
              >
                ログイン
              </Link>
            </>
          )}
        </div>
      </div>
    </header>
  )
}
