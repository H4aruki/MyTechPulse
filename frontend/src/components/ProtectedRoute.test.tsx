import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { currentUser } from '@/api/endpoints'
import { ProtectedRoute } from './ProtectedRoute'

vi.mock('@/api/endpoints', () => ({ currentUser: vi.fn() }))

function renderProtectedPage() {
  const client = new QueryClient()
  return render(
    <QueryClientProvider client={client}>
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
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProtectedRoute', () => {
  beforeEach(() => {
    vi.mocked(currentUser).mockReset()
  })
  afterEach(() => {
    cleanup()
    localStorage.clear()
  })

  test('確認中は読み込み表示になる', () => {
    vi.mocked(currentUser).mockReturnValue(new Promise(() => {}))
    renderProtectedPage()
    expect(screen.getByText('読み込んでいます…')).toBeInTheDocument()
  })

  test('ログイン済みなら保護された画面を表示する', async () => {
    vi.mocked(currentUser).mockResolvedValue({ id: 1, username: 'u', role: 'member' })
    renderProtectedPage()
    expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
  })

  test('401ならログイン画面へ移動する', async () => {
    vi.mocked(currentUser).mockRejectedValue(new UnauthorizedError())
    renderProtectedPage()
    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '記事一覧' })).not.toBeInTheDocument()
  })

  test('通信障害・5xxはログイン画面へ送らず再試行を出す', async () => {
    vi.mocked(currentUser).mockRejectedValue(new ApiError({ status: 503 }))
    renderProtectedPage()
    expect(await screen.findByRole('button', { name: '再試行' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'ログイン' })).not.toBeInTheDocument()
  })

  test('旧トークンがlocalStorageにあっても認証済み扱いしない', async () => {
    localStorage.setItem('access_token', 'old')
    vi.mocked(currentUser).mockRejectedValue(new UnauthorizedError())
    renderProtectedPage()
    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
  })
})
