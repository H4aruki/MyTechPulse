import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { fetchPasskeys } from '@/api/endpoints'
import { PasskeySettings } from './PasskeySettings'

vi.mock('@/api/endpoints', () => ({
  beginPasskeyRegistration: vi.fn(),
  deletePasskey: vi.fn(),
  fetchPasskeys: vi.fn(),
  finishPasskeyRegistration: vi.fn(),
  renamePasskey: vi.fn(),
}))

describe('PasskeySettings の分析からの除外', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.resetAllMocks()
  })

  test('利用者が付けた名前を含む行（表示とボタン）は ph-no-capture の中にある', async () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    vi.stubGlobal('navigator', { credentials: { create: vi.fn() } })
    vi.mocked(fetchPasskeys).mockResolvedValue({
      passkeys: [{ id: 1, name: '山田太郎のiPhone', created_at: '2026-10-16T00:00:00Z' }],
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <PasskeySettings onUnauthorized={vi.fn()} />
      </QueryClientProvider>,
    )

    const name = await screen.findByText('山田太郎のiPhone')
    const rename = screen.getByRole('button', { name: '山田太郎のiPhoneの名前を変更' })
    const remove = screen.getByRole('button', { name: '山田太郎のiPhoneを削除' })
    for (const el of [name, rename, remove]) {
      expect(el.closest('.ph-no-capture')).not.toBeNull()
    }
  })
})
