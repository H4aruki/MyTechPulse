import { Link, useLocation } from 'react-router-dom'

interface SiteHeaderProps {
  /** ログイン後の画面ではログアウトボタンを、未ログイン時は登録/ログインボタンを出す */
  authenticated?: boolean
  onLogout?: () => void
}

const menuLink =
  'ring-focus rounded-sm text-sm text-ink-70 transition-colors hover:text-accent-ink'
const menuLinkCurrent = 'ring-focus rounded-sm text-sm font-semibold text-ink'
const headerButton =
  'ring-focus min-h-11 shrink-0 px-3 py-2 text-sm whitespace-nowrap sm:px-5 sm:py-2.5'

function Logo({ authenticated }: { authenticated: boolean }) {
  const className = 'ring-focus flex items-center gap-2.5 rounded-sm'
  const inner = (
    <>
      <img src="/img/mytechpulse-logo2.png" alt="" className="h-8 w-8 object-contain" />
      <span className="font-mono text-base font-semibold tracking-tight text-ink">MyTechPulse</span>
    </>
  )
  // LP（/）はSPAの外にあるため、未ログイン時は通常のリンクでページごと移動する
  if (authenticated) {
    return (
      <Link to="/articles" className={className}>
        {inner}
      </Link>
    )
  }
  return (
    <a href="/" className={className}>
      {inner}
    </a>
  )
}

function AppLink({ to, current, children }: { to: string; current: boolean; children: string }) {
  return (
    <Link
      to={to}
      aria-current={current ? 'page' : undefined}
      className={current ? menuLinkCurrent : menuLink}
    >
      {children}
    </Link>
  )
}

/**
 * トップページと同じ半透明の帯。
 * 未ログイン時の「機能・仕組み・対応ソース」はトップページへのリンク。
 * ログイン後は、記事一覧・マイページ・設定とログアウトに切り替える。
 * 狭い画面でもログアウトやログインは帯の上に残す（三本線メニューの奥に隠さない）。
 */
export function SiteHeader({ authenticated = false, onLogout }: SiteHeaderProps) {
  const { pathname } = useLocation()

  return (
    <header className="site-header sticky top-0 z-50 border-b border-line backdrop-blur-xl">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3 px-4 py-3 sm:px-6 sm:py-4 md:px-10">
        <Logo authenticated={authenticated} />
        {authenticated ? (
          <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2 sm:gap-x-6">
            <nav className="flex flex-wrap items-center gap-x-4 gap-y-1" aria-label="メインメニュー">
              <AppLink to="/articles" current={pathname === '/articles'}>
                記事一覧
              </AppLink>
              <AppLink to="/mypage" current={pathname === '/mypage'}>
                マイページ
              </AppLink>
              <AppLink to="/settings" current={pathname === '/settings'}>
                設定
              </AppLink>
            </nav>
            <button type="button" onClick={onLogout} className={`btn-ghost ${headerButton}`}>
              ログアウト
            </button>
          </div>
        ) : (
          <>
            <nav className="hidden items-center gap-8 md:flex" aria-label="トップページ">
              <a href="/#features" className={menuLink}>
                機能
              </a>
              <a href="/#how" className={menuLink}>
                仕組み
              </a>
              <a href="/#sources" className={menuLink}>
                対応ソース
              </a>
            </nav>
            <div className="flex items-center gap-2 sm:gap-3">
              <Link
                to="/login"
                aria-current={pathname === '/login' ? 'page' : undefined}
                className={`btn-ghost ${headerButton}`}
              >
                ログイン
              </Link>
              <Link
                to="/signup"
                aria-current={pathname === '/signup' ? 'page' : undefined}
                className={`btn-primary ${headerButton}`}
              >
                新規登録
              </Link>
            </div>
          </>
        )}
      </div>
    </header>
  )
}
