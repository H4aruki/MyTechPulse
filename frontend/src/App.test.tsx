import { cleanup, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import App from './App'

// 認証の中身は ProtectedRoute.test.tsx で確かめ済み。ここでは「保護の中にあるか」だけを見る
vi.mock('@/components/ProtectedRoute', () => ({
  ProtectedRoute: ({ children }: { children: ReactNode }) => (
    <div data-testid="protected">{children}</div>
  ),
}))
vi.mock('@/pages/ArticlesPage', () => ({ ArticlesPage: () => <h1>記事ページ</h1> }))
vi.mock('@/pages/LoginPage', () => ({ LoginPage: () => <h1>ログインページ</h1> }))
vi.mock('@/pages/MyPage', () => ({ MyPage: () => <h1>マイページ</h1> }))
vi.mock('@/pages/SettingsPage', () => ({ SettingsPage: () => <h1>設定ページ</h1> }))
vi.mock('@/pages/SignupPage', () => ({ SignupPage: () => <h1>会員登録ページ</h1> }))

function renderAt(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

describe('App のルーティング', () => {
  afterEach(() => {
    cleanup()
    window.history.pushState({}, '', '/')
  })

  test('/app/ は記事ページへ移り、ログインが要る範囲に入る', async () => {
    renderAt('/app/')
    const protectedArea = await screen.findByTestId('protected')
    expect(within(protectedArea).getByRole('heading', { name: '記事ページ' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/app/articles')
  })

  test.each([
    ['/app/login', 'ログインページ'],
    ['/app/signup', '会員登録ページ'],
  ])('%s はログインが要らないページを出す', async (path, heading) => {
    renderAt(path)
    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument()
    expect(screen.queryByTestId('protected')).not.toBeInTheDocument()
    expect(window.location.pathname).toBe(path)
  })

  test.each([
    ['/app/articles', '記事ページ'],
    ['/app/mypage', 'マイページ'],
    ['/app/settings', '設定ページ'],
  ])('%s はログインが要る範囲の中でページを出す', async (path, heading) => {
    renderAt(path)
    const protectedArea = await screen.findByTestId('protected')
    expect(within(protectedArea).getByRole('heading', { name: heading })).toBeInTheDocument()
    expect(window.location.pathname).toBe(path)
  })

  test('知らないパスはログインページへ戻す', async () => {
    renderAt('/app/unknown')
    expect(await screen.findByRole('heading', { name: 'ログインページ' })).toBeInTheDocument()
    expect(screen.queryByTestId('protected')).not.toBeInTheDocument()
    expect(window.location.pathname).toBe('/app/login')
  })
})
