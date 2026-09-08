import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { tokenStorage } from '@/lib/auth'
import { ProtectedRoute } from './ProtectedRoute'

function renderProtectedPage() {
  return render(
    <MemoryRouter initialEntries={['/private']}>
      <Routes>
        <Route path="/login" element={<h1>ログイン</h1>} />
        <Route
          path="/private"
          element={
            <ProtectedRoute>
              <h1>記事一覧</h1>
            </ProtectedRoute>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('ProtectedRoute', () => {
  afterEach(() => {
    localStorage.clear()
  })

  test('トークンが無い場合はログイン画面へ移動する', () => {
    renderProtectedPage()

    expect(screen.getByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '記事一覧' })).not.toBeInTheDocument()
  })

  test('トークンがある場合は保護された画面を表示する', () => {
    tokenStorage.set('test-token')

    renderProtectedPage()

    expect(screen.getByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
  })
})
