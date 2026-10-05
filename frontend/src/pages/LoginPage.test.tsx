import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { login } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import { LoginPage } from './LoginPage'

vi.mock('@/api/endpoints', () => ({ login: vi.fn() }))

const user = { id: 1, username: 'alice', role: 'member' as const }

function renderLogin() {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/articles" element={<h1>記事一覧</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

function submit(username: string, password: string) {
  fireEvent.change(screen.getByLabelText('ユーザー名'), { target: { value: username } })
  fireEvent.change(screen.getByLabelText('パスワード'), { target: { value: password } })
  fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.mocked(login).mockReset()
  })
  afterEach(() => {
    cleanup()
  })

  test('成功すると利用者をauthクエリへ入れて記事一覧へ移動する', async () => {
    vi.mocked(login).mockResolvedValue({ user })
    const client = renderLogin()

    submit('alice', 'secret')

    expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
    // useMutation は第2引数に実行情報も渡すため、送信内容だけを確認する
    expect(vi.mocked(login).mock.calls[0][0]).toEqual({ username: 'alice', password: 'secret' })
    expect(client.getQueryData(authQueryKey)).toEqual(user)
  })

  test('401は利用者名の有無を区別しない同一の文言を出す', async () => {
    vi.mocked(login).mockRejectedValue(new UnauthorizedError())
    renderLogin()

    submit('alice', 'wrong')

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'ユーザー名またはパスワードが間違っています。',
    )
    expect(screen.queryByRole('heading', { name: '記事一覧' })).not.toBeInTheDocument()
  })

  test('401以外の失敗はApiErrorの文言を出し、自動で再送しない', async () => {
    vi.mocked(login).mockRejectedValue(
      new ApiError({ status: 503 }, 'しばらくしてからお試しください。'),
    )
    renderLogin()

    submit('alice', 'secret')

    expect(await screen.findByRole('alert')).toHaveTextContent('しばらくしてからお試しください。')
    expect(login).toHaveBeenCalledTimes(1)
  })

  test('未入力なら送信せず入力エラーを出す', async () => {
    renderLogin()

    fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))

    expect(await screen.findByText('ユーザー名を入力してください')).toBeInTheDocument()
    expect(login).not.toHaveBeenCalled()
  })
})
