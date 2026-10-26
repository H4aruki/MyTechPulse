import { QueryCache, QueryClient } from '@tanstack/react-query'
import { UnauthorizedError } from '@/api/client'
import { authQueryKey } from '@/lib/auth'

/** 認証確認以外のクエリが401で失敗したか。確認自体の失敗では再確認しない（無限ループ防止） */
export function shouldRecheckAuth(error: unknown, queryKey: readonly unknown[]): boolean {
  return error instanceof UnauthorizedError && queryKey[0] !== authQueryKey[0]
}

export function createQueryClient(): QueryClient {
  const queryClient: QueryClient = new QueryClient({
    queryCache: new QueryCache({
      // 記事取得などの途中でセッション切れになったら、ログイン状態を確認し直す。
      // 確認結果が401なら ProtectedRoute がログイン画面へ戻す。
      // 確認自体の失敗では再確認しない（無限ループ防止）。
      onError: (error, query) => {
        if (shouldRecheckAuth(error, query.queryKey)) {
          void queryClient.invalidateQueries({ queryKey: authQueryKey })
        }
      },
    }),
  })
  return queryClient
}
