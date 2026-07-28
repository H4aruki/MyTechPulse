import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from '@/components/ProtectedRoute'
import { tokenStorage } from '@/lib/auth'
import { ArticlesPage } from '@/pages/ArticlesPage'
import { LoginPage } from '@/pages/LoginPage'
import { SignupPage } from '@/pages/SignupPage'

// LP（/）とアプリ本体（/app/）を分離しているため、SPA側は basename を /app に固定する
export default function App() {
  return (
    <BrowserRouter basename="/app">
      <Routes>
        <Route
          index
          element={<Navigate to={tokenStorage.get() ? '/articles' : '/login'} replace />}
        />
        <Route path="login" element={<LoginPage />} />
        <Route path="signup" element={<SignupPage />} />
        <Route
          path="articles"
          element={
            <ProtectedRoute>
              <ArticlesPage />
            </ProtectedRoute>
          }
        />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
