import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { currentUser, logout } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import { passkeysQueryKey } from '@/lib/passkey'
import { SAVED_ARTICLE_IDS_QUERY_KEY, SAVED_ARTICLES_QUERY_KEY } from '@/lib/savedArticles'
import { SettingsPage } from './SettingsPage'

vi.mock('@/api/endpoints', () => ({ currentUser: vi.fn(), logout: vi.fn() }))
vi.mock('@/components/PasskeySettings', () => ({
  PasskeySettings: ({ onUnauthorized }: { onUnauthorized: () => void }) => (
    <button type="button" onClick={onUnauthorized}>
      パスキー認証切れ
    </button>
  ),
}))

const user = { id: 1, username: 'alice', role: 'member' as const }
const openedKey = 'mtp.feedback.openedArticles'
const feedKey = ['feed', 'recommended']
const otherKey = ['other']

function renderSettings() {
  const client = new QueryClient()
  client.setQueryData(authQueryKey, user)
  client.setQueryData(passkeysQueryKey, [{ id: 'p1' }])
  client.setQueryData(feedKey, [{ url: 'https://a' }])
  client.setQueryData(otherKey, 'keep')
  window.sessionStorage.setItem(openedKey, '["https://a"]')
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/settings']}>
        <Routes>
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/login" element={<h1>ログイン画面</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

function expectSessionDiscarded(client: QueryClient) {
  expect(client.getQueryData(authQueryKey)).toBeUndefined()
  expect(client.getQueryData(passkeysQueryKey)).toBeUndefined()
  expect(client.getQueryData(feedKey)).toBeUndefined()
  expect(client.getQueryData(otherKey)).toBe('keep')
  expect(window.sessionStorage.getItem(openedKey)).toBeNull()
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.mocked(currentUser).mockReset().mockResolvedValue(user)
    vi.mocked(logout).mockReset()
    window.sessionStorage.clear()
  })
  afterEach(() => {
    cleanup()
  })

  test('ログイン中の利用者名を表示する', async () => {
    renderSettings()

    expect(await screen.findByText('alice')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '設定' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('ログアウトに成功すると、利用者ごとの情報を捨ててログイン画面へ移る', async () => {
    vi.mocked(logout).mockResolvedValue(undefined)
    const client = renderSettings()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(logout).toHaveBeenCalledTimes(1)
    expectSessionDiscarded(client)
  })

  test('ログアウトが401でも、ログイン画面へ移り、情報を捨てる', async () => {
    vi.mocked(logout).mockRejectedValue(new UnauthorizedError())
    const client = renderSettings()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expectSessionDiscarded(client)
  })

  test('401以外の失敗は失敗を知らせ、移動も情報の破棄もしない', async () => {
    vi.mocked(logout).mockRejectedValue(new ApiError({ status: 503 }))
    const client = renderSettings()

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('ログアウトに失敗しました')
    expect(screen.getByRole('heading', { name: '設定' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'ログイン画面' })).not.toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toEqual(user)
    expect(client.getQueryData(passkeysQueryKey)).toEqual([{ id: 'p1' }])
    expect(client.getQueryData(feedKey)).toEqual([{ url: 'https://a' }])
    expect(window.sessionStorage.getItem(openedKey)).toBe('["https://a"]')
  })

  test('パスキー設定が認証切れを知らせても、ログイン画面へ移り、情報を捨てる', async () => {
    const client = renderSettings()

    fireEvent.click(screen.getByRole('button', { name: 'パスキー認証切れ' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    await waitFor(() => expectSessionDiscarded(client))
    expect(logout).not.toHaveBeenCalled()
  })

  test('ログアウトに成功すると、保存した記事の情報も捨てる', async () => {
    vi.mocked(logout).mockResolvedValue(undefined)
    const client = renderSettings()
    client.setQueryData(SAVED_ARTICLES_QUERY_KEY, [{ id: 1 }])
    client.setQueryData(SAVED_ARTICLE_IDS_QUERY_KEY, [1])

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(client.getQueryData(SAVED_ARTICLES_QUERY_KEY)).toBeUndefined()
    expect(client.getQueryData(SAVED_ARTICLE_IDS_QUERY_KEY)).toBeUndefined()
  })

  test('ログアウトが401でも、保存した記事の情報を捨てる', async () => {
    vi.mocked(logout).mockRejectedValue(new UnauthorizedError())
    const client = renderSettings()
    client.setQueryData(SAVED_ARTICLES_QUERY_KEY, [{ id: 1 }])
    client.setQueryData(SAVED_ARTICLE_IDS_QUERY_KEY, [1])

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    expect(client.getQueryData(SAVED_ARTICLES_QUERY_KEY)).toBeUndefined()
    expect(client.getQueryData(SAVED_ARTICLE_IDS_QUERY_KEY)).toBeUndefined()
  })

  test('パスキー設定が認証切れを知らせても、保存した記事の情報を捨てる', async () => {
    const client = renderSettings()
    client.setQueryData(SAVED_ARTICLES_QUERY_KEY, [{ id: 1 }])
    client.setQueryData(SAVED_ARTICLE_IDS_QUERY_KEY, [1])

    fireEvent.click(screen.getByRole('button', { name: 'パスキー認証切れ' }))

    expect(await screen.findByRole('heading', { name: 'ログイン画面' })).toBeInTheDocument()
    await waitFor(() => {
      expect(client.getQueryData(SAVED_ARTICLES_QUERY_KEY)).toBeUndefined()
      expect(client.getQueryData(SAVED_ARTICLE_IDS_QUERY_KEY)).toBeUndefined()
    })
  })
})
