import { QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'
import { initAnalytics, resetOnSessionEnd } from './lib/analytics'
import { authQueryKey } from './lib/auth'
import { createQueryClient } from './lib/queryClient'

const queryClient = createQueryClient()

// 分析は環境変数が揃ったときだけ動く。ログアウト・認証切れでは識別を外す
void initAnalytics()
resetOnSessionEnd(queryClient.getQueryCache(), authQueryKey)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
