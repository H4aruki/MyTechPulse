import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { UnauthorizedError } from './api/client'
import './index.css'
import { tokenStorage } from './lib/auth'

/**
 * トークン切れ・不正時の後始末を1箇所に集約する。
 * ルーター外からも呼ばれうるため、遷移は location による全体リロードで行う。
 */
function handleApiError(error: unknown) {
  if (error instanceof UnauthorizedError) {
    tokenStorage.clear()
    window.location.assign('/app/login')
  }
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: handleApiError }),
  mutationCache: new MutationCache({ onError: handleApiError }),
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
