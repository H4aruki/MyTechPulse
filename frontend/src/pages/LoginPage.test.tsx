import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { beginPasskeyLogin, finishPasskeyLogin, login } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import { LoginPage } from './LoginPage'

vi.mock('@/api/endpoints', () => ({
  login: vi.fn(),
  beginPasskeyLogin: vi.fn(),
  finishPasskeyLogin: vi.fn(),
}))

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

// サーバーが返すログイン用の設定（利用者名なしなので allowCredentials は空）
const assertionOptions = {
  challenge: 'AQID',
  rpId: 'localhost',
  allowCredentials: null,
  userVerification: 'required',
}

function fakeAssertion() {
  return {
    id: 'Y3JlZA',
    rawId: new Uint8Array([1, 2]).buffer,
    type: 'public-key',
    authenticatorAttachment: 'platform',
    response: {
      clientDataJSON: new Uint8Array([3]).buffer,
      authenticatorData: new Uint8Array([4]).buffer,
      signature: new Uint8Array([5]).buffer,
      userHandle: new Uint8Array([6]).buffer,
    },
    getClientExtensionResults: () => ({}),
  }
}

/** パスキーでのログインに対応したブラウザにする（credentials.get を差し替える） */
function supportPasskey(get: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
  Object.defineProperty(navigator, 'credentials', { value: { get }, configurable: true })
}

function submit(username: string, password: string) {
  fireEvent.change(screen.getByLabelText('ユーザー名'), { target: { value: username } })
  fireEvent.change(screen.getByLabelText('パスワード'), { target: { value: password } })
  fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.mocked(login).mockReset()
    vi.mocked(beginPasskeyLogin).mockReset()
    vi.mocked(finishPasskeyLogin).mockReset()
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    Reflect.deleteProperty(navigator, 'credentials')
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

  test('成功すると、前の利用者のアンケート判定の記録を消す', async () => {
    window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
    vi.mocked(login).mockResolvedValue({ user })
    renderLogin()

    submit('alice', 'secret')

    expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
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

  test('429は失敗が続いたため待つよう案内する', async () => {
    vi.mocked(login).mockRejectedValue(new ApiError({ status: 429, code: 'too_many_requests' }))
    renderLogin()

    submit('alice', 'secret')

    expect(await screen.findByRole('alert')).toHaveTextContent('しばらく操作できません')
    expect(login).toHaveBeenCalledTimes(1)
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

  test('パスキー非対応のブラウザでは「パスキーでログイン」を出さない', () => {
    renderLogin()

    expect(screen.queryByRole('button', { name: 'パスキーでログイン' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'ログイン' })).toBeInTheDocument()
  })

  test('PublicKeyCredential があっても credentials.get が無ければ出さない', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    Object.defineProperty(navigator, 'credentials', { value: {}, configurable: true })
    renderLogin()

    expect(screen.queryByRole('button', { name: 'パスキーでログイン' })).not.toBeInTheDocument()
  })

  test('対応しているブラウザでは「パスキーでログイン」を出す', () => {
    supportPasskey(vi.fn())
    renderLogin()

    expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
  })

  describe('パスキーでログイン', () => {
    test('begin→端末の確認→finishの順に進み、パスワードと同じ後処理で記事一覧へ移動する', async () => {
      window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
      const get = vi.fn().mockResolvedValue(fakeAssertion())
      supportPasskey(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      const client = renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      // 利用者名なし（discoverable）で、端末に認証情報を選ばせる
      expect(get).toHaveBeenCalledTimes(1)
      const options = get.mock.calls[0][0].publicKey
      expect(Array.from(new Uint8Array(options.challenge))).toEqual([1, 2, 3])
      expect(options.allowCredentials).toEqual([])
      expect(options.userVerification).toBe('required')
      expect(vi.mocked(finishPasskeyLogin).mock.calls[0][0]).toEqual({
        credential: {
          id: 'Y3JlZA',
          rawId: 'AQI',
          type: 'public-key',
          authenticatorAttachment: 'platform',
          response: { clientDataJSON: 'Aw', authenticatorData: 'BA', signature: 'BQ', userHandle: 'Bg' },
          clientExtensionResults: {},
        },
      })
      expect(client.getQueryData(authQueryKey)).toEqual(user)
      expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
      expect(login).not.toHaveBeenCalled()
    })

    test('401は理由を区別しない固定の文言を出し、パスワードの入力は使える', async () => {
      supportPasskey(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'パスキーでログインできませんでした。もう一度お試しください。',
      )
      expect(screen.queryByRole('heading', { name: '記事一覧' })).not.toBeInTheDocument()
      expect(screen.getByLabelText('ユーザー名')).toBeEnabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    })

    test('エラーの後でも、パスワードでログインできる', async () => {
      supportPasskey(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      vi.mocked(login).mockResolvedValue({ user })
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      await screen.findByRole('alert')
      submit('alice', 'secret')

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(vi.mocked(login).mock.calls[0][0]).toEqual({ username: 'alice', password: 'secret' })
    })

    test('429は失敗が続いたため待つよう案内する', async () => {
      supportPasskey(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(
        new ApiError({ status: 429, code: 'too_many_requests' }),
      )
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('しばらく操作できません')
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
    })

    test('通信に失敗したときは、その案内を出し、自動で再送しない', async () => {
      supportPasskey(vi.fn())
      vi.mocked(beginPasskeyLogin).mockRejectedValue(
        new ApiError({ status: 0 }, 'サーバーに接続できませんでした。通信環境を確認してください。'),
      )
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('サーバーに接続できませんでした')
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
    })

    test('端末側の想定外の失敗は、内部の文言を出さず固定の文言にする', async () => {
      supportPasskey(vi.fn().mockRejectedValue(new DOMException('rp id mismatch', 'SecurityError')))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      const alert = await screen.findByRole('alert')
      expect(alert).toHaveTextContent('パスキーでログインできませんでした。もう一度お試しください。')
      expect(alert).not.toHaveTextContent('rp id mismatch')
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
    })

    test('端末の確認を取り消しても、エラーを出さず、そのまま操作を続けられる', async () => {
      const get = vi.fn().mockRejectedValue(new DOMException('x', 'NotAllowedError'))
      supportPasskey(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled(),
      )
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
    })

    test('前に出ていたエラーは、押し直した時点で消える', async () => {
      const get = vi
        .fn()
        .mockResolvedValueOnce(fakeAssertion())
        .mockRejectedValueOnce(new DOMException('x', 'NotAllowedError'))
      supportPasskey(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      await screen.findByRole('alert')
      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
      await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    })

    test('処理中は両方のボタンを止め、二重に送らない', async () => {
      supportPasskey(vi.fn().mockResolvedValue(fakeAssertion()))
      let release: (value: { publicKey: Record<string, unknown> }) => void = () => {}
      vi.mocked(beginPasskeyLogin).mockReturnValue(
        new Promise((resolve) => {
          release = resolve
        }),
      )
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      renderLogin()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      const busyButton = await screen.findByRole('button', { name: '確認中…' })

      expect(busyButton).toBeDisabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeDisabled()
      fireEvent.click(busyButton)
      fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(login).not.toHaveBeenCalled()

      release({ publicKey: assertionOptions })
      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
    })

    test('パスワードの処理中は「パスキーでログイン」も止める', async () => {
      supportPasskey(vi.fn())
      vi.mocked(login).mockReturnValue(new Promise(() => {}))
      renderLogin()

      submit('alice', 'secret')

      expect(await screen.findByRole('button', { name: 'ログイン中…' })).toBeDisabled()
      const passkeyButton = screen.getByRole('button', { name: 'パスキーでログイン' })
      expect(passkeyButton).toBeDisabled()
      fireEvent.click(passkeyButton)
      expect(beginPasskeyLogin).not.toHaveBeenCalled()
    })
  })
})
