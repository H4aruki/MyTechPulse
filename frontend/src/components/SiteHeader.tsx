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
      <div className="mx-auto flex min-h-12 max-w-6xl flex-wrap items-center justify-between gap-x-2 gap-y-2 px-3 py-1.5 sm:min-h-16 sm:gap-x-3 sm:px-4 sm:py-2">
        {/* LP（/）はSPAの外にあるため、未ログイン時は通常のリンクでページごと移動する */}
        {authenticated ? (
          <Link
            to="/articles"
            className="flex items-center gap-1.5 font-display text-sm font-bold text-ink sm:gap-2 sm:text-lg"
          >
            <img src="/img/mytechpulse-logo2.png" alt="" className="h-7 w-7 object-contain sm:h-8 sm:w-8" />
            MyTechPulse
          </Link>
        ) : (
          <a
            href="/"
            className="flex items-center gap-1.5 font-display text-sm font-bold text-ink sm:gap-2 sm:text-lg"
          >
            <img src="/img/mytechpulse-logo2.png" alt="" className="h-7 w-7 object-contain sm:h-8 sm:w-8" />
            MyTechPulse
          </a>
        )}

        {/* スマホでは、ヘッダーを1段に収めるため隠す（提供元はフッターの Powered by にもある） */}
        <nav
          className="hidden items-center gap-4 md:flex"
          aria-label="提供元サイト"
        >
          <a href="https://qiita.com/" target="_blank" rel="noreferrer noopener" title="Qiita">
            <img src="/img/Qiita_logo.png" alt="Qiita" className="h-7 w-auto md:h-8" />
          </a>
          <a href="https://zenn.dev/" target="_blank" rel="noreferrer noopener" title="Zenn">
            <img src="/img/Zenn_logo.png" alt="Zenn" className="h-7 w-auto md:h-8" />
          </a>
        </nav>

        <div className="ml-auto flex items-center gap-1.5 sm:gap-2 md:ml-0">
          {authenticated ? (
            <>
              <Link
                to="/mypage"
                className="whitespace-nowrap rounded-lg border border-slate-300 px-2 py-1.5 text-xs font-bold sm:px-4 sm:py-2 sm:text-sm text-ink-muted transition hover:bg-slate-100"
              >
                マイページ
              </Link>
              <Link
                to="/settings"
                className="whitespace-nowrap rounded-lg border border-slate-300 px-2 py-1.5 text-xs font-bold sm:px-4 sm:py-2 sm:text-sm text-ink-muted transition hover:bg-slate-100"
              >
                設定
              </Link>
              <button
              type="button"
              onClick={onLogout}
              className="whitespace-nowrap rounded-lg border border-slate-300 px-2 py-1.5 text-xs font-bold sm:px-4 sm:py-2 sm:text-sm text-ink-muted transition hover:bg-slate-100"
            >
              ログアウト
              </button>
            </>
          ) : (
            <>
              <Link
                to="/signup"
                className="whitespace-nowrap rounded-lg border border-slate-300 px-2 py-1.5 text-xs font-bold sm:px-4 sm:py-2 sm:text-sm text-ink-muted transition hover:bg-slate-100"
              >
                新規登録
              </Link>
              <Link
                to="/login"
                className="whitespace-nowrap rounded-lg bg-brand-500 px-2 py-1.5 text-xs font-bold sm:px-4 sm:py-2 sm:text-sm text-white transition hover:bg-brand-700"
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
