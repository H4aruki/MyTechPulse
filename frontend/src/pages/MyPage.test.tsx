import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { currentUser, fetchSavedArticles, logout } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import { MyPage } from './MyPage'

vi.mock('@/api/endpoints', () => ({
  currentUser: vi.fn(),
  logout: vi.fn(),
  fetchSavedArticles: vi.fn(),
  saveArticle: vi.fn(),
  unsaveArticle: vi.fn(),
}))

const user = { id: 1, username: 'alice', role: 'member' as const }

function renderMyPage() {
  const client = new QueryClient()
  client.setQueryData(authQueryKey, user)
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/mypage']}>
        <Routes>
          <Route path="/mypage" element={<MyPage />} />
          <Route path="/articles" element={<h1>記事一覧画面</h1>} />
          <Route path="/settings" element={<h1>設定画面</h1>} />
          <Route path="/login" element={<h1>ログイン画面</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

describe('MyPage', () => {
  beforeEach(() => {
    vi.mocked(currentUser).mockReset().mockResolvedValue(user)
    vi.mocked(logout).mockReset()
    vi.mocked(fetchSavedArticles)
      .mockReset()
      .mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 })
    window.sessionStorage.clear()
  })
  afterEach(() => {
    cleanup()
  })

  test('ユーザー名だけを表示し、メールアドレスやパスキー設定は出さない', async () => {
    renderMyPage()

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'マイページ' })).toBeInTheDocument()
    expect(screen.queryByText(/メール/)).not.toBeInTheDocument()
    expect(screen.queryByText(/パスキーを(追加|登録)/)).not.toBeInTheDocument()
  })

  test('設定ページへのリンクがあり、移動できる', async () => {
    renderMyPage()

    fireEvent.click(await screen.findByRole('link', { name: /設定（パスキーの管理など）へ/ }))

    expect(await screen.findByRole('heading', { name: '設定画面' })).toBeInTheDocument()
  })

  test('左上に戻るリンクがあり、タイトルより前にあって、記事一覧へ戻れる', async () => {
    renderMyPage()

    const back = await screen.findByRole('link', { name: '戻る' })
    const title = screen.getByRole('heading', { name: 'マイページ' })
    expect(back.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(back).toHaveAttribute('href', '/articles')

    fireEvent.click(back)

    expect(await screen.findByRole('heading', { name: '記事一覧画面' })).toBeInTheDocument()
  })

  test('保存した記事の欄を表示する', async () => {
    renderMyPage()

    expect(screen.getByRole('heading', { name: '保存した記事' })).toBeInTheDocument()
    expect(await screen.findByText(/まだ保存した記事はありません/)).toBeInTheDocument()
  })

  test('ログアウトに成功すると、情報を捨ててログイン画面へ移る', async () => {
    vi.mocked(logout).mockResolvedValue(undefined)
    const client = renderMyPage()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toBeUndefined()
  })

  test('ログアウトが401でも、ログイン画面へ移る', async () => {
    vi.mocked(logout).mockRejectedValue(new UnauthorizedError())
    renderMyPage()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('401以外の失敗は失敗を知らせ、移動しない', async () => {
    vi.mocked(logout).mockRejectedValue(new ApiError({ status: 503 }))
    const client = renderMyPage()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('ログアウトに失敗しました')
    expect(screen.getByRole('heading', { name: 'マイページ' })).toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toEqual(user)
  })
})
