import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { UnauthorizedError } from './api/client'
import './index.css'
import { authQueryKey } from './lib/auth'

const queryClient: QueryClient = new QueryClient({
  queryCache: new QueryCache({
    // 記事取得などの途中でセッション切れになったら、ログイン状態を確認し直す。
    // 確認結果が401なら ProtectedRoute がログイン画面へ戻す。
    // 確認自体の失敗では再確認しない（無限ループ防止）。
    onError: (error, query) => {
      if (error instanceof UnauthorizedError && query.queryKey[0] !== authQueryKey[0]) {
        void queryClient.invalidateQueries({ queryKey: authQueryKey })
      }
    },
  }),
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
