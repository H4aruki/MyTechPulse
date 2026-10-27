import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test } from 'vitest'
import { LEGAL_VERSION } from '@/lib/legal'
import { PrivacyPage } from './PrivacyPage'

describe('PrivacyPage', () => {
  afterEach(() => {
    cleanup()
  })

  function renderPage() {
    render(
      <MemoryRouter>
        <PrivacyPage />
      </MemoryRouter>,
    )
  }

  test('見出しと版、施行日の欄を出す', () => {
    renderPage()

    expect(screen.getByRole('heading', { level: 1, name: 'プライバシーポリシー' })).toBeInTheDocument()
    expect(screen.getByText(new RegExp(`版: ${LEGAL_VERSION}`))).toBeInTheDocument()
    expect(screen.getByText(/施行日: 【要記入：施行日】/)).toBeInTheDocument()
  })

  test.each(['Cookie とブラウザの保存領域', 'パスキー', '利用状況の分析（PostHog）', '外部サービス'])(
    '「%s」の節がある',
    (title) => {
      renderPage()

      expect(screen.getByRole('heading', { level: 2, name: new RegExp(title, 'u') })).toBeInTheDocument()
    },
  )

  test('PostHogは設定が有効になった場合に使うと書く', () => {
    renderPage()

    expect(screen.getByText(/分析ツールの設定が有効になった場合に/)).toBeInTheDocument()
  })

  test('削除は問い合わせ先への連絡で受けると書く', () => {
    renderPage()

    expect(screen.getByText(/退会（アカウント削除）の機能は、まだ提供していません/)).toBeInTheDocument()
  })
})
