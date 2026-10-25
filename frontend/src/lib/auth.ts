import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { currentUser } from '@/api/endpoints'
import type { User } from '@/api/types'
import { identifyUser } from '@/lib/analytics'

/** ログイン状態はブラウザ側に持たず、サーバーの `/auth/me` の結果を正とする */
export const authQueryKey = ['auth', 'me'] as const

export function useCurrentUser() {
  const query = useQuery<User, Error>({
    queryKey: authQueryKey,
    queryFn: currentUser,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 5 * 60 * 1000,
  })
  // 分析には内部の利用者IDだけを渡す（名前は渡さない）
  const userId = query.data?.id
  useEffect(() => {
    if (userId !== undefined) identifyUser(userId)
  }, [userId])
  return query
}
