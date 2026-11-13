import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { UnauthorizedError } from '@/api/client'
import { useCurrentUser } from '@/lib/auth'
import { loginRedirectState } from '@/lib/returnTo'

/**
 * ログイン済みかどうかは `/auth/me` で判定する。
 * 401なら元の場所を渡してログイン画面へ戻し（ログイン後に戻る）、通信障害や5xxは再試行できる表示にする（未ログイン扱いにしない）。
 */
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { isPending, isError, error, refetch } = useCurrentUser()
  const location = useLocation()

  if (isPending) {
    return <p className="py-16 text-center text-ink-70">読み込んでいます…</p>
  }

  if (isError) {
    if (error instanceof UnauthorizedError) {
      return <Navigate to="/login" replace state={loginRedirectState(location)} />
    }
    return (
      <div role="alert" className="mx-auto max-w-md px-4 py-16 text-center">
        <p className="mb-4 text-sm text-red-700">
          サーバーに接続できませんでした。時間をおいて再度お試しください。
        </p>
        <button
          type="button"
          onClick={() => refetch()}
          className="btn-primary ring-focus min-h-11 px-4 py-2 text-sm"
        >
          再試行
        </button>
      </div>
    )
  }

  return <>{children}</>
}
