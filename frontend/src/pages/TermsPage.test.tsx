import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test } from 'vitest'
import { LEGAL_VERSION } from '@/lib/legal'
import { TermsPage } from './TermsPage'

describe('TermsPage', () => {
  afterEach(() => {
    cleanup()
  })

  test('見出しと版、施行日の欄を出す', () => {
    render(
      <MemoryRouter>
        <TermsPage />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1, name: '利用規約' })).toBeInTheDocument()
    expect(screen.getByText(new RegExp(`版: ${LEGAL_VERSION}`))).toBeInTheDocument()
    expect(screen.getByText(/施行日: 【要記入：施行日】/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '第3条（禁止事項）' })).toBeInTheDocument()
  })
})
