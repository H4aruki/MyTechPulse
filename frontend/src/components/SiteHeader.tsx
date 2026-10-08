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
      <div className="mx-auto flex min-h-16 max-w-6xl flex-wrap items-center justify-between gap-x-3 gap-y-2 px-4 py-2">
        <Link
          to={authenticated ? '/articles' : '/login'}
          className="flex items-center gap-2 font-display text-base font-bold text-ink sm:text-lg"
        >
          <img src="/img/mytechpulse-logo2.png" alt="" className="h-8 w-8 object-contain" />
          MyTechPulse
        </Link>

        {/* スマホでは、ロゴを大きく見せるため、2段目に回す */}
        <nav
          className="order-last flex w-full items-center justify-center gap-6 md:order-none md:w-auto md:gap-4"
          aria-label="提供元サイト"
        >
          <a href="https://qiita.com/" target="_blank" rel="noreferrer noopener" title="Qiita">
            <img src="/img/Qiita_logo.png" alt="Qiita" className="h-7 w-auto md:h-8" />
          </a>
          <a href="https://zenn.dev/" target="_blank" rel="noreferrer noopener" title="Zenn">
            <img src="/img/Zenn_logo.png" alt="Zenn" className="h-7 w-auto md:h-8" />
          </a>
        </nav>

        <div className="ml-auto flex items-center gap-2 md:ml-0">
          {authenticated ? (
            <button
              type="button"
              onClick={onLogout}
              className="whitespace-nowrap rounded-lg border border-slate-300 px-3 py-2 text-sm font-bold sm:px-4 text-ink-muted transition hover:bg-slate-100"
            >
              ログアウト
            </button>
          ) : (
            <>
              <Link
                to="/signup"
                className="whitespace-nowrap rounded-lg border border-slate-300 px-3 py-2 text-sm font-bold sm:px-4 text-ink-muted transition hover:bg-slate-100"
              >
                新規登録
              </Link>
              <Link
                to="/login"
                className="whitespace-nowrap rounded-lg bg-brand-500 px-3 py-2 text-sm font-bold sm:px-4 text-white transition hover:bg-brand-700"
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
