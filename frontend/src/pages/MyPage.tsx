import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { UnauthorizedError } from '@/api/client'
import { logout } from '@/api/endpoints'
import { AppLayout } from '@/components/AppLayout'
import { authQueryKey, useCurrentUser } from '@/lib/auth'
import { clearFeedbackState } from '@/lib/feedbackTracker'
import { passkeysQueryKey } from '@/lib/passkey'

/**
 * マイページは概要だけを置く。パスキーなどの設定は /settings に分けている。
 * 個人情報を持たない方針のため、アカウント情報はユーザー名だけを表示する。
 */
export function MyPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: user } = useCurrentUser()

  const leaveSession = useCallback(() => {
    queryClient.removeQueries({ queryKey: authQueryKey })
    queryClient.removeQueries({ queryKey: passkeysQueryKey })
    queryClient.removeQueries({ queryKey: ['feed'] })
    clearFeedbackState()
    navigate('/login', { replace: true })
  }, [queryClient, navigate])

  const logoutMutation = useMutation({
    mutationFn: logout,
    retry: false,
    onSuccess: leaveSession,
    onError: (e: Error) => {
      if (e instanceof UnauthorizedError) leaveSession()
    },
  })
  const logoutFailed = logoutMutation.isError && !(logoutMutation.error instanceof UnauthorizedError)

  return (
    <AppLayout
      authenticated
      onLogout={() => {
        if (!logoutMutation.isPending) logoutMutation.mutate()
      }}
    >
      <div className="mx-auto w-full max-w-3xl space-y-8 px-4 py-10">
        {/* 戻り先は記事一覧。履歴に依存せず、直接開いた場合でも同じ場所へ戻す */}
        <Link
          to="/articles"
          aria-label="戻る"
          className="inline-flex items-center gap-1 text-sm font-bold text-brand-700 hover:underline"
        >
          <span aria-hidden="true">←</span>
          戻る
        </Link>
        <h1 className="text-2xl font-bold text-ink">マイページ</h1>
        {logoutFailed && (
          <p role="alert" className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
            ログアウトに失敗しました。もう一度お試しください。
          </p>
        )}

        <section aria-labelledby="mypage-account" className="space-y-2">
          <h2 id="mypage-account" className="text-lg font-bold text-ink">
            アカウント
          </h2>
          {user && (
            <p className="text-sm text-ink-muted">
              ユーザー名: <span className="font-bold text-ink">{user.username}</span>
            </p>
          )}
          <p className="text-sm">
            <Link to="/settings" className="font-bold text-brand-700 underline">
              設定（パスキーの管理など）へ
            </Link>
          </p>
        </section>

        {/* 記事の保存機能（#236）で、保存した記事の一覧をここに載せる */}
        <section aria-labelledby="mypage-saved" className="space-y-2">
          <h2 id="mypage-saved" className="text-lg font-bold text-ink">
            保存した記事
          </h2>
          <p className="rounded-lg border border-dashed border-slate-300 px-4 py-6 text-sm text-ink-muted">
            記事の保存機能は準備中です。保存した記事は、ここに表示される予定です。
          </p>
        </section>
      </div>
    </AppLayout>
  )
}
