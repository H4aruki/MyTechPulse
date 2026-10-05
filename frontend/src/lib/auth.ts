import { useQuery } from '@tanstack/react-query'
import { currentUser } from '@/api/endpoints'
import type { User } from '@/api/types'

/** ログイン状態はブラウザ側に持たず、サーバーの `/auth/me` の結果を正とする */
export const authQueryKey = ['auth', 'me'] as const

export function useCurrentUser() {
  return useQuery<User, Error>({
    queryKey: authQueryKey,
    queryFn: currentUser,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 5 * 60 * 1000,
  })
}
