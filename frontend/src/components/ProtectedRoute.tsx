import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { tokenStorage } from '@/lib/auth'

/**
 * 旧実装では articles.html のロード時にトークン有無を見て location.href で飛ばしていた。
 * ルーター側の関心事として切り出し、保護対象ページを宣言的に囲めるようにしたもの。
 */
export function ProtectedRoute({ children }: { children: ReactNode }) {
  if (!tokenStorage.get()) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
