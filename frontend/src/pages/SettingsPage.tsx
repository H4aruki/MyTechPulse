import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { UnauthorizedError } from '@/api/client'
import { logout } from '@/api/endpoints'
import { AppLayout } from '@/components/AppLayout'
import { PasskeySettings } from '@/components/PasskeySettings'
import { authQueryKey, useCurrentUser } from '@/lib/auth'
import { clearFeedbackState } from '@/lib/feedbackTracker'
import { passkeysQueryKey } from '@/lib/passkey'
import { SAVED_ARTICLE_IDS_QUERY_KEY, SAVED_ARTICLES_QUERY_KEY } from '@/lib/savedArticles'

export function SettingsPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: user } = useCurrentUser()

  // ログアウト・認証切れのどちらでも、利用者ごとの情報を捨ててログイン画面へ移る
  const leaveSession = useCallback(() => {
    queryClient.removeQueries({ queryKey: authQueryKey })
    queryClient.removeQueries({ queryKey: passkeysQueryKey })
    queryClient.removeQueries({ queryKey: ['feed'] })
    queryClient.removeQueries({ queryKey: SAVED_ARTICLES_QUERY_KEY })
    queryClient.removeQueries({ queryKey: SAVED_ARTICLE_IDS_QUERY_KEY })
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
      <div className="mx-auto w-full max-w-3xl space-y-8 px-6 py-12">
        <h1 className="text-3xl font-bold tracking-tight text-ink">設定</h1>
        {logoutFailed && (
          <p role="alert" className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
            ログアウトに失敗しました。もう一度お試しください。
          </p>
        )}
        {user && (
          <p className="text-sm text-ink-70">
            ログイン中のユーザー名: <span className="font-bold text-ink">{user.username}</span>
          </p>
        )}
        <PasskeySettings onUnauthorized={leaveSession} />
      </div>
    </AppLayout>
  )
}
