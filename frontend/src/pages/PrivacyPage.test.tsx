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

  test('アンケートは2年を過ぎると削除し、アカウント削除でも消えると書く', () => {
    renderPage()

    expect(
      screen.getByText(/まだ終えていない表示は、表示した日時から2年を過ぎると削除します/),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/アカウントを削除したときも、これらの記録は一緒に消えます/),
    ).toBeInTheDocument()
    const untilRequested = screen.getByText(/削除を求められるまで保存します/)
    expect(untilRequested.textContent).not.toContain('アンケート')
    expect(untilRequested.textContent).toContain('興味度')
    expect(untilRequested.textContent).toContain('保存した記事')
    expect(untilRequested.textContent).toContain('登録したパスキー')
  })

  test('パスキーの確認用の乱数は5分、バックアップは7日より長く残ることがあると書く', () => {
    renderPage()

    expect(screen.getAllByText(/有効期間は5分です/).length).toBeGreaterThan(0)
    expect(screen.getByText(/7日より長く残ることがあります/)).toBeInTheDocument()
  })
})
