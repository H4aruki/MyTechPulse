import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError } from '@/api/client'
import {
  beginPasskeyRegistration,
  deletePasskey,
  fetchPasskeys,
  finishPasskeyRegistration,
  renamePasskey,
} from '@/api/endpoints'
import { PasskeySettings } from './PasskeySettings'

vi.mock('@/api/endpoints', () => ({
  beginPasskeyRegistration: vi.fn(),
  deletePasskey: vi.fn(),
  fetchPasskeys: vi.fn(),
  finishPasskeyRegistration: vi.fn(),
  renamePasskey: vi.fn(),
}))

const item = (id: number, name: string, last?: string) => ({
  id,
  name,
  created_at: '2026-10-16T00:00:00Z',
  ...(last ? { last_used_at: last } : {}),
})

function renderSettings(onUnauthorized = vi.fn()) {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <PasskeySettings onUnauthorized={onUnauthorized} />
    </QueryClientProvider>,
  )
}

function stubSupported(create = vi.fn()) {
  vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
  vi.stubGlobal('navigator', { credentials: { create } })
  return create
}

describe('PasskeySettings', () => {
  beforeEach(() => {
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [] })
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.resetAllMocks()
  })

  test('非対応のブラウザでは項目ごと出さず、APIも呼ばない', () => {
    vi.stubGlobal('PublicKeyCredential', undefined)
    renderSettings()

    expect(screen.queryByText('パスキー')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'パスキーを追加' })).not.toBeInTheDocument()
    expect(fetchPasskeys).not.toHaveBeenCalled()
  })

  test('一覧に名前と最後に使った日時を出す。未使用はその旨を出す', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({
      passkeys: [item(1, 'iPhone', '2026-10-17T03:04:00Z'), item(2, '仕事用PC')],
    })
    renderSettings()

    expect(await screen.findByText('iPhone')).toBeInTheDocument()
    expect(screen.getByText('仕事用PC')).toBeInTheDocument()
    expect(screen.getByText(/最後に使った日時: 2026/)).toBeInTheDocument()
    expect(screen.getByText('最後に使った日時: まだ使っていません')).toBeInTheDocument()
  })

  test('追加: begin → navigator.credentials.create → finish の順に呼び、一覧を取り直す', async () => {
    const credential = {
      id: 'Y3JlZA',
      rawId: new Uint8Array([1]).buffer,
      type: 'public-key',
      response: {
        clientDataJSON: new Uint8Array([2]).buffer,
        attestationObject: new Uint8Array([3]).buffer,
      },
      getClientExtensionResults: () => ({}),
    }
    const create = stubSupported(vi.fn().mockResolvedValue(credential))
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({
      challengeToken: 'reg-token-1',
      publicKey: { challenge: 'AQID', user: { id: 'BAUG', name: 'a', displayName: 'a' } },
    })
    vi.mocked(finishPasskeyRegistration).mockResolvedValue(item(1, 'iPhone'))
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.change(screen.getByLabelText('名前（任意）'), { target: { value: ' iPhone ' } })
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(1, 'iPhone')] })
    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    expect(await screen.findByText('パスキーを追加しました。')).toBeInTheDocument()
    expect(create).toHaveBeenCalledOnce()
    const body = vi.mocked(finishPasskeyRegistration).mock.calls[0][0]
    expect(body.name).toBe('iPhone')
    // begin の応答のトークンを、そのまま finish へ渡す
    expect(body.challengeToken).toBe('reg-token-1')
    expect(body.credential).toMatchObject({ id: 'Y3JlZA', rawId: 'AQ', type: 'public-key' })
    expect(await screen.findByText('iPhone')).toBeInTheDocument()
  })

  test('端末で取り消したときは登録せず、メッセージを出す', async () => {
    stubSupported(vi.fn().mockRejectedValue(new DOMException('x', 'NotAllowedError')))
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({
      challengeToken: 'reg-token-1',
      publicKey: { challenge: 'AQID', user: { id: 'BAUG', name: 'a', displayName: 'a' } },
    })
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('取り消されたか')
    expect(finishPasskeyRegistration).not.toHaveBeenCalled()
  })

  test('サーバーの失敗は本文のメッセージを出す', async () => {
    stubSupported()
    vi.mocked(beginPasskeyRegistration).mockRejectedValue(
      new ApiError({ status: 409, detail: '登録できるパスキーは10件までです。' }),
    )
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('10件まで')
  })

  test('10件あると追加ボタンを無効にする', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({
      passkeys: Array.from({ length: 10 }, (_, i) => item(i + 1, `p${i + 1}`)),
    })
    renderSettings()

    await screen.findByText('p1')
    expect(screen.getByRole('button', { name: 'パスキーを追加' })).toBeDisabled()
  })

  test('名前の変更', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(7, 'old')] })
    vi.mocked(renamePasskey).mockResolvedValue(undefined)
    renderSettings()

    fireEvent.click(await screen.findByRole('button', { name: 'oldの名前を変更' }))
    fireEvent.change(screen.getByLabelText('新しい名前'), { target: { value: ' new ' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(renamePasskey).toHaveBeenCalled())
    expect(vi.mocked(renamePasskey).mock.calls[0]).toEqual([7, 'new'])
    expect(await screen.findByText('名前を変更しました。')).toBeInTheDocument()
  })

  test('削除は確認してから行い、やめれば呼ばない', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(3, 'key')] })
    vi.mocked(deletePasskey).mockResolvedValue(undefined)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    renderSettings()

    const button = await screen.findByRole('button', { name: 'keyを削除' })
    fireEvent.click(button)
    expect(deletePasskey).not.toHaveBeenCalled()

    fireEvent.click(button)
    await waitFor(() => expect(deletePasskey).toHaveBeenCalled())
    expect(vi.mocked(deletePasskey).mock.calls[0][0]).toBe(3)
    expect(await screen.findByText('パスキーを削除しました。')).toBeInTheDocument()
    confirm.mockRestore()
  })
})
