import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
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

  describe('別のアプリへの切り替えで中断されたとき', () => {
    let visibility: 'hidden' | 'visible' = 'visible'

    function setVisibility(state: 'hidden' | 'visible') {
      visibility = state
      document.dispatchEvent(new Event('visibilitychange'))
    }

    beforeEach(() => {
      visibility = 'visible'
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
    })
    afterEach(() => {
      visibility = 'visible'
      Reflect.deleteProperty(document, 'visibilityState')
    })

    const registrationOptions = {
      challengeToken: 'reg-token-1',
      publicKey: { challenge: 'AQID', user: { id: 'BAUG', name: 'a', displayName: 'a' } },
    }

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

    test('中断の説明と「もう一度試す」を出し、追加ボタンは使える。押すとやり直して追加できる', async () => {
      const create = stubSupported(
        vi
          .fn()
          .mockImplementationOnce(async () => {
            setVisibility('hidden')
            throw new DOMException('x', 'NotAllowedError')
          })
          .mockResolvedValueOnce(credential),
      )
      vi.mocked(beginPasskeyRegistration).mockResolvedValue(registrationOptions)
      vi.mocked(finishPasskeyRegistration).mockResolvedValue(item(1, 'iPhone'))
      renderSettings()
      await screen.findByText('登録されたパスキーはまだありません。')

      fireEvent.change(screen.getByLabelText('名前（任意）'), { target: { value: 'iPhone' } })
      fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

      const alert = await screen.findByRole('alert')
      setVisibility('visible')
      expect(alert).toHaveTextContent('ほかのアプリに切り替えたため')
      expect(alert).toHaveTextContent('パスキーの追加ができませんでした')
      expect(within(alert).getByRole('button', { name: 'もう一度試す' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'パスキーを追加' })).toBeEnabled()
      expect(finishPasskeyRegistration).not.toHaveBeenCalled()

      fireEvent.click(screen.getByRole('button', { name: 'もう一度試す' }))

      expect(await screen.findByText('パスキーを追加しました。')).toBeInTheDocument()
      expect(beginPasskeyRegistration).toHaveBeenCalledTimes(2)
      expect(create).toHaveBeenCalledTimes(2)
      expect(finishPasskeyRegistration).toHaveBeenCalledTimes(1)
      expect(vi.mocked(finishPasskeyRegistration).mock.calls[0][0].name).toBe('iPhone')
      expect(screen.queryByRole('button', { name: 'もう一度試す' })).not.toBeInTheDocument()
    })

    test('非表示にならない取り消しでは、「もう一度試す」を出さない', async () => {
      stubSupported(vi.fn().mockRejectedValue(new DOMException('x', 'NotAllowedError')))
      vi.mocked(beginPasskeyRegistration).mockResolvedValue(registrationOptions)
      renderSettings()
      await screen.findByText('登録されたパスキーはまだありません。')

      fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('取り消されたか')
      expect(screen.queryByRole('button', { name: 'もう一度試す' })).not.toBeInTheDocument()
    })

    test('やり直しが別のエラーになると、中断の表示と「もう一度試す」は消える', async () => {
      stubSupported(
        vi
          .fn()
          .mockImplementationOnce(async () => {
            setVisibility('hidden')
            throw new DOMException('x', 'NotAllowedError')
          })
          .mockRejectedValueOnce(new DOMException('x', 'NotAllowedError')),
      )
      vi.mocked(beginPasskeyRegistration).mockResolvedValue(registrationOptions)
      renderSettings()
      await screen.findByText('登録されたパスキーはまだありません。')

      fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))
      await screen.findByRole('button', { name: 'もう一度試す' })
      setVisibility('visible')
      fireEvent.click(screen.getByRole('button', { name: 'もう一度試す' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('取り消されたか')
      expect(screen.queryByRole('button', { name: 'もう一度試す' })).not.toBeInTheDocument()
    })

    test('保存先（sessionStorage・localStorage）へ challengeToken を書かない', async () => {
      stubSupported(
        vi.fn().mockImplementationOnce(async () => {
          setVisibility('hidden')
          throw new DOMException('x', 'NotAllowedError')
        }),
      )
      vi.mocked(beginPasskeyRegistration).mockResolvedValue(registrationOptions)
      renderSettings()
      await screen.findByText('登録されたパスキーはまだありません。')

      fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))
      await screen.findByRole('button', { name: 'もう一度試す' })
      setVisibility('visible')

      expect(JSON.stringify({ ...window.sessionStorage })).not.toContain('reg-token-1')
      expect(JSON.stringify({ ...window.localStorage })).not.toContain('reg-token-1')
    })
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
    const add = screen.getByRole('button', { name: 'パスキーを追加' })
    expect(add).toBeDisabled()
    expect(screen.getByText('上限に達しています。不要なものを削除してください。')).toBeInTheDocument()
    fireEvent.click(add)
    expect(beginPasskeyRegistration).not.toHaveBeenCalled()
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

  test('名前を空のまま保存しようとすると、変更を送らず入力を求める', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(7, 'old')] })
    renderSettings()

    fireEvent.click(await screen.findByRole('button', { name: 'oldの名前を変更' }))
    fireEvent.change(screen.getByLabelText('新しい名前'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('名前を入力してください。')
    expect(renamePasskey).not.toHaveBeenCalled()
    expect(screen.getByLabelText('新しい名前')).toBeInTheDocument()
  })

  test('名前の変更をキャンセルすると、元の名前のままにする', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(7, 'old')] })
    renderSettings()

    fireEvent.click(await screen.findByRole('button', { name: 'oldの名前を変更' }))
    fireEvent.change(screen.getByLabelText('新しい名前'), { target: { value: 'new' } })
    fireEvent.click(screen.getByRole('button', { name: 'キャンセル' }))

    expect(screen.queryByLabelText('新しい名前')).not.toBeInTheDocument()
    expect(screen.getByText('old')).toBeInTheDocument()
    expect(renamePasskey).not.toHaveBeenCalled()
  })

  test('一覧の取得に失敗すると、その旨を出す', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockRejectedValue(new ApiError({ status: 500 }))
    renderSettings()

    expect(await screen.findByRole('alert')).toHaveTextContent('パスキーの一覧を取得できませんでした。')
    expect(screen.queryByText('登録されたパスキーはまだありません。')).not.toBeInTheDocument()
  })

  test('一覧の取得が認証切れなら、ログインへ戻し、取得失敗の文は出さない', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockRejectedValue(new UnauthorizedError())
    const onUnauthorized = vi.fn()
    renderSettings(onUnauthorized)

    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
    expect(screen.queryByText('パスキーの一覧を取得できませんでした。')).not.toBeInTheDocument()
  })

  test('追加の開始が認証切れなら、ログインへ戻す', async () => {
    stubSupported()
    vi.mocked(beginPasskeyRegistration).mockRejectedValue(new UnauthorizedError())
    const onUnauthorized = vi.fn()
    renderSettings(onUnauthorized)
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(finishPasskeyRegistration).not.toHaveBeenCalled()
  })

  test('追加の失敗が Error でないときは、決まった文で知らせる', async () => {
    stubSupported()
    vi.mocked(beginPasskeyRegistration).mockRejectedValue('落ちた')
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '失敗しました。時間をおいてもう一度お試しください。',
    )
  })

  test('名前を空のまま追加すると、名前なしで登録を終える', async () => {
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
    stubSupported(vi.fn().mockResolvedValue(credential))
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({
      challengeToken: 'reg-token-1',
      publicKey: { challenge: 'AQID', user: { id: 'BAUG', name: 'a', displayName: 'a' } },
    })
    vi.mocked(finishPasskeyRegistration).mockResolvedValue(item(1, 'パスキー'))
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))

    await waitFor(() => expect(finishPasskeyRegistration).toHaveBeenCalled())
    expect(vi.mocked(finishPasskeyRegistration).mock.calls[0][0].name).toBeUndefined()
    expect(await screen.findByText('パスキーを追加しました。')).toBeInTheDocument()
  })

  test('追加しているあいだにもう一度送っても、登録を二重に始めない', async () => {
    stubSupported()
    let release: (value: { challengeToken: string; publicKey: Record<string, unknown> }) => void = () => {}
    vi.mocked(beginPasskeyRegistration).mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    renderSettings()
    await screen.findByText('登録されたパスキーはまだありません。')

    fireEvent.click(screen.getByRole('button', { name: 'パスキーを追加' }))
    const busy = await screen.findByRole('button', { name: '追加しています…' })
    expect(busy).toBeDisabled()
    fireEvent.click(busy)
    const form = busy.closest('form')
    if (form) fireEvent.submit(form)
    expect(beginPasskeyRegistration).toHaveBeenCalledTimes(1)

    await act(async () => {
      release({
        challengeToken: 'reg-token-1',
        publicKey: { challenge: 'AQID', user: { id: 'BAUG', name: 'a', displayName: 'a' } },
      })
    })
    // 端末の応答が無いので、ここでは登録を終えず、取り消しとして終わる
    expect(await screen.findByRole('alert')).toHaveTextContent('取り消されたか')
    expect(beginPasskeyRegistration).toHaveBeenCalledTimes(1)
  })

  test('削除に失敗すると知らせ、一覧を読み直す', async () => {
    stubSupported()
    vi.mocked(fetchPasskeys).mockResolvedValue({ passkeys: [item(3, 'key')] })
    vi.mocked(deletePasskey).mockRejectedValue(new ApiError({ status: 500, detail: '削除できませんでした。' }))
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderSettings()

    fireEvent.click(await screen.findByRole('button', { name: 'keyを削除' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('削除できませんでした。')
    await waitFor(() => expect(fetchPasskeys).toHaveBeenCalledTimes(2))
    expect(screen.getByText('key')).toBeInTheDocument()
    confirm.mockRestore()
  })
})
