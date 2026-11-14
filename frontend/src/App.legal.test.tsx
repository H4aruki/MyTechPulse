import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import App from './App'

// 法務ページは本物を出す。ログインが要る範囲に入っていれば testid で分かる
vi.mock('@/components/ProtectedRoute', () => ({
  ProtectedRoute: ({ children }: { children: ReactNode }) => (
    <div data-testid="protected">{children}</div>
  ),
}))

describe('App の法務ページのルート', () => {
  afterEach(() => {
    cleanup()
    window.history.pushState({}, '', '/')
  })

  test.each([
    ['/app/terms', '利用規約'],
    ['/app/privacy', 'プライバシーポリシー'],
  ])('%s は未ログインでも開ける', async (path, heading) => {
    window.history.pushState({}, '', path)
    render(<App />)

    expect(await screen.findByRole('heading', { level: 1, name: heading })).toBeInTheDocument()
    expect(screen.queryByTestId('protected')).not.toBeInTheDocument()
    expect(window.location.pathname).toBe(path)
  })
})
