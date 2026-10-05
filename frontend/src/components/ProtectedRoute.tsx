import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { UnauthorizedError } from '@/api/client'
import { useCurrentUser } from '@/lib/auth'

/**
 * ログイン済みかどうかは `/auth/me` で判定する。
 * 401ならログイン画面へ戻し、通信障害や5xxは再試行できる表示にする（未ログイン扱いにしない）。
 */
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { isPending, isError, error, refetch } = useCurrentUser()

  if (isPending) {
    return <p className="py-16 text-center text-ink-muted">読み込んでいます…</p>
  }

  if (isError) {
    if (error instanceof UnauthorizedError) {
      return <Navigate to="/login" replace />
    }
    return (
      <div role="alert" className="mx-auto max-w-md px-4 py-16 text-center">
        <p className="mb-4 text-sm text-red-700">
          サーバーに接続できませんでした。時間をおいて再度お試しください。
        </p>
        <button
          type="button"
          onClick={() => refetch()}
          className="rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white transition hover:bg-brand-700"
        >
          再試行
        </button>
      </div>
    )
  }

  return <>{children}</>
}
