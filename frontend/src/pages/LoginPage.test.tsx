import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { beginPasskeyLogin, finishPasskeyLogin, login } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import {
  CHALLENGE_MAX_AGE_MS,
  CONDITIONAL_MAX_BEGINS,
  CONDITIONAL_MIN_INTERVAL_MS,
  CONDITIONAL_REFRESH_MS,
  PREVIOUS_RUN_WAIT_MAX_MS,
  resetPasskeyLoginForTest,
} from '@/lib/passkeyLogin'
import { LoginPage } from './LoginPage'

vi.mock('@/api/endpoints', () => ({
  login: vi.fn(),
  beginPasskeyLogin: vi.fn(),
  finishPasskeyLogin: vi.fn(),
}))

const user = { id: 1, username: 'alice', role: 'member' as const }

function renderLogin({ strict = false }: { strict?: boolean } = {}) {
  const client = new QueryClient()
  const page = (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/articles" element={<h1>記事一覧</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
  render(strict ? <StrictMode>{page}</StrictMode> : page)
  return client
}

// サーバーが返すログイン用の設定（利用者名なしなので allowCredentials は空）
const assertionOptions = {
  challenge: 'AQID',
  rpId: 'localhost',
  allowCredentials: null,
  userVerification: 'required',
}

function fakeAssertion(id = 'Y3JlZA') {
  return {
    id,
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

type ConditionalSupport = boolean | 'missing' | 'reject'

/**
 * パスキーでのログインに対応したブラウザにする（credentials.get を差し替える）。
 * conditional UI は、既定では非対応（isConditionalMediationAvailable が無い）
 */
function supportPasskey(get: (...args: never[]) => unknown, conditional: ConditionalSupport = 'missing') {
  const ctor = function PublicKeyCredential() {}
  if (conditional === 'reject') {
    Object.assign(ctor, { isConditionalMediationAvailable: vi.fn().mockRejectedValue(new Error('x')) })
  } else if (conditional !== 'missing') {
    Object.assign(ctor, { isConditionalMediationAvailable: vi.fn().mockResolvedValue(conditional) })
  }
  vi.stubGlobal('PublicKeyCredential', ctor)
  Object.defineProperty(navigator, 'credentials', { value: { get }, configurable: true })
}

interface GetArgs {
  mediation?: string
  signal?: AbortSignal
}

interface PendingGet {
  resolve: (value: unknown) => void
  reject: (reason: unknown) => void
  signal: AbortSignal
}

/**
 * conditional の get は、解決されるまで待つ。signal が中止されると AbortError で reject する
 * （実ブラウザと同じ。ignoreAbort は「中止後に結果が遅れて届く」競合を作るためのもの）。
 * conditional でない get は modal へ回す
 */
function conditionalBrowser({ ignoreAbort = false }: { ignoreAbort?: boolean } = {}) {
  const pending: PendingGet[] = []
  const modal = vi.fn()
  const onAbort = vi.fn()
  const get = vi.fn((args: GetArgs) => {
    if (args.mediation !== 'conditional') return modal(args)
    return new Promise((resolve, reject) => {
      const signal = args.signal as AbortSignal
      pending.push({ resolve, reject, signal })
      signal.addEventListener('abort', () => {
        onAbort()
        if (!ignoreAbort) reject(new DOMException('', 'AbortError'))
      })
    })
  })
  supportPasskey(get, true)
  return { get, modal, pending, onAbort }
}

function submit(username: string, password: string) {
  fireEvent.change(screen.getByLabelText('ユーザー名'), { target: { value: username } })
  fireEvent.change(screen.getByLabelText('パスワード'), { target: { value: password } })
  fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
}

const advance = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })

/** 通常の非同期処理（開始の遅延・Promise の連鎖）が落ち着くまで待つ */
const settle = () => advance(20)

function setVisibility(state: 'hidden' | 'visible') {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'))
  })
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.mocked(login).mockReset()
    vi.mocked(beginPasskeyLogin).mockReset()
    vi.mocked(finishPasskeyLogin).mockReset()
  })
  afterEach(() => {
    cleanup()
    // 直列化の状態（tail）が次のテストへ漏れないようにする
    resetPasskeyLoginForTest()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    Reflect.deleteProperty(navigator, 'credentials')
    Reflect.deleteProperty(document, 'visibilityState')
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

  test('PublicKeyCredential が無いときは、begin も呼ばない', async () => {
    renderLogin()

    await new Promise((resolve) => setTimeout(resolve, 20))

    expect(beginPasskeyLogin).not.toHaveBeenCalled()
  })

  test('対応しているブラウザでは「パスキーでログイン」を出す', () => {
    supportPasskey(vi.fn())
    renderLogin()

    expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
  })

  test('利用者名とパスワードの欄に webauthn を付ける（autofill にパスキーを出すため）', () => {
    renderLogin()

    expect(screen.getByLabelText('ユーザー名')).toHaveAttribute('autocomplete', 'username webauthn')
    expect(screen.getByLabelText('パスワード')).toHaveAttribute(
      'autocomplete',
      'current-password webauthn',
    )
  })

  // 明示のボタンの挙動は、conditional UI があってもなくても変わらない
  describe.each([
    { name: 'conditional なし', conditional: false },
    { name: 'conditional あり', conditional: true },
  ])('パスキーでログイン（$name）', ({ conditional }) => {
    // conditional ありのときは、ページ表示で begin が1回余分に呼ばれる
    const initialBegins = conditional ? 1 : 0
    const begins = initialBegins + 1

    /** modalGet が、ボタンで始まる確認の get。conditional の get は待機にする */
    function support(modalGet: (...args: never[]) => unknown) {
      if (!conditional) {
        supportPasskey(modalGet)
        return
      }
      const get = vi.fn((args: GetArgs) =>
        args.mediation === 'conditional'
          ? new Promise((_resolve, reject) => {
              args.signal?.addEventListener('abort', () => reject(new DOMException('', 'AbortError')))
            })
          : (modalGet as (a: GetArgs) => unknown)(args),
      )
      supportPasskey(get, true)
    }

    /** ボタンを押す前に、conditional の開始を済ませる（押した順で結果が変わらないように） */
    async function renderReady() {
      const client = renderLogin()
      if (conditional) await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(1))
      return client
    }

    test('begin→端末の確認→finishの順に進み、パスワードと同じ後処理で記事一覧へ移動する', async () => {
      window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
      const get = vi.fn().mockResolvedValue(fakeAssertion())
      support(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      const client = await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(begins)
      // 利用者名なし（discoverable）で、端末に認証情報を選ばせる
      expect(get).toHaveBeenCalledTimes(1)
      const options = get.mock.calls[0][0].publicKey
      expect(Array.from(new Uint8Array(options.challenge))).toEqual([1, 2, 3])
      expect(options.allowCredentials).toEqual([])
      expect(options.userVerification).toBe('required')
      expect(get.mock.calls[0][0].mediation).toBeUndefined()
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
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(client.getQueryData(authQueryKey)).toEqual(user)
      expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
      expect(login).not.toHaveBeenCalled()
    })

    test('401は理由を区別しない固定の文言を出し、パスワードの入力は使える', async () => {
      support(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      await renderReady()

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
      support(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      vi.mocked(login).mockResolvedValue({ user })
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      await screen.findByRole('alert')
      submit('alice', 'secret')

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(vi.mocked(login).mock.calls[0][0]).toEqual({ username: 'alice', password: 'secret' })
    })

    test('429は失敗が続いたため待つよう案内する', async () => {
      support(vi.fn().mockResolvedValue(fakeAssertion()))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(
        new ApiError({ status: 429, code: 'too_many_requests' }),
      )
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('しばらく操作できません')
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
    })

    test('通信に失敗したときは、その案内を出し、自動で再送しない', async () => {
      support(vi.fn())
      vi.mocked(beginPasskeyLogin).mockRejectedValue(
        new ApiError({ status: 0 }, 'サーバーに接続できませんでした。通信環境を確認してください。'),
      )
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('サーバーに接続できませんでした')
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(begins)
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
    })

    test('端末側の想定外の失敗は、内部の文言を出さず固定の文言にする', async () => {
      support(vi.fn().mockRejectedValue(new DOMException('rp id mismatch', 'SecurityError')))
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      const alert = await screen.findByRole('alert')
      expect(alert).toHaveTextContent('パスキーでログインできませんでした。もう一度お試しください。')
      expect(alert).not.toHaveTextContent('rp id mismatch')
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
    })

    test('端末の確認を取り消しても、エラーを出さず、そのまま操作を続けられる', async () => {
      const get = vi.fn().mockRejectedValue(new DOMException('x', 'NotAllowedError'))
      support(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      await renderReady()

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
      support(get)
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      await screen.findByRole('alert')
      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

      await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
      await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    })

    test('処理中は両方のボタンを止め、二重に送らない', async () => {
      support(vi.fn().mockResolvedValue(fakeAssertion()))
      let release: (value: { publicKey: Record<string, unknown> }) => void = () => {}
      vi.mocked(beginPasskeyLogin).mockReturnValue(
        new Promise((resolve) => {
          release = resolve
        }),
      )
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      await renderReady()

      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))
      const busyButton = await screen.findByRole('button', { name: '確認中…' })

      expect(busyButton).toBeDisabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeDisabled()
      fireEvent.click(busyButton)
      fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
      // conditional ありでも、先の begin の応答が届くまで、ボタンの begin は送らない
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(Math.max(initialBegins, 1))
      expect(login).not.toHaveBeenCalled()

      release({ publicKey: assertionOptions })
      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
      // 連打や二重の送信で、begin が増えていない
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(begins)
    })

    test('パスワードの処理中は「パスキーでログイン」も止める', async () => {
      support(vi.fn())
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
      vi.mocked(login).mockReturnValue(new Promise(() => {}))
      await renderReady()

      submit('alice', 'secret')

      expect(await screen.findByRole('button', { name: 'ログイン中…' })).toBeDisabled()
      const passkeyButton = screen.getByRole('button', { name: 'パスキーでログイン' })
      expect(passkeyButton).toBeDisabled()
      fireEvent.click(passkeyButton)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(initialBegins)
    })
  })

  describe('conditional UI（利用者名欄の autofill）', () => {
    beforeEach(() => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: assertionOptions })
    })

    const clickPasskeyButton = () =>
      fireEvent.click(screen.getByRole('button', { name: 'パスキーでログイン' }))

    test.each<[string, ConditionalSupport]>([
      ['isConditionalMediationAvailable が無い', 'missing'],
      ['false を返す', false],
      ['reject する', 'reject'],
    ])('未対応のブラウザ（%s）では begin を呼ばず、ボタンだけを出す', async (_name, support) => {
      const get = vi.fn()
      supportPasskey(get, support)
      renderLogin()

      await settle()

      expect(beginPasskeyLogin).not.toHaveBeenCalled()
      expect(get).not.toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
    })

    test('表示すると begin を1回呼び、mediation:conditional と signal 付きで get を呼ぶ。操作は止めない', async () => {
      const { get } = conditionalBrowser()
      renderLogin()

      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      const args = get.mock.calls[0][0]
      expect(args.mediation).toBe('conditional')
      expect(args.signal).toBeInstanceOf(AbortSignal)
      expect(args.signal?.aborted).toBe(false)
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
    })

    test('autofill で選んだ成功は、finish のあと、authクエリを更新して記事一覧へ移動する', async () => {
      window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
      const { get, pending } = conditionalBrowser()
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      const client = renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      pending[0].resolve(fakeAssertion('cond'))

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(vi.mocked(finishPasskeyLogin).mock.calls[0][0].credential.id).toBe('cond')
      expect(client.getQueryData(authQueryKey)).toEqual(user)
      expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
    })

    test('ボタンを押すと conditional を中止し、その後で begin と modal の get を呼ぶ', async () => {
      const { get, modal, pending, onAbort } = conditionalBrowser()
      modal.mockResolvedValue(fakeAssertion('modal'))
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      clickPasskeyButton()

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(pending[0].signal.aborted).toBe(true)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
      expect(get).toHaveBeenCalledTimes(2)
      expect(get.mock.calls[1][0].mediation).toBeUndefined()
      // 中止 → 2回目の begin → 2回目の get の順（2つの ceremony が同時にならない）
      const [aborted] = onAbort.mock.invocationCallOrder
      const secondBegin = vi.mocked(beginPasskeyLogin).mock.invocationCallOrder[1]
      const secondGet = get.mock.invocationCallOrder[1]
      expect(aborted).toBeLessThan(secondBegin)
      expect(secondBegin).toBeLessThan(secondGet)
      expect(vi.mocked(finishPasskeyLogin).mock.calls[0][0].credential.id).toBe('modal')
    })

    test('conditional の begin が保留中にボタンを押しても、先の begin の応答が届くまで次の begin を送らない', async () => {
      const { get, modal } = conditionalBrowser()
      modal.mockResolvedValue(fakeAssertion('modal'))
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      let releaseFirst: () => void = () => {}
      vi.mocked(beginPasskeyLogin)
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              releaseFirst = () => resolve({ publicKey: assertionOptions })
            }),
        )
        .mockResolvedValue({ publicKey: assertionOptions })
      renderLogin()
      await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(1))

      clickPasskeyButton()
      await settle()

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(get).not.toHaveBeenCalled()

      releaseFirst()

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
      // 中止済みの conditional は、begin の応答が届いても get を呼ばない
      expect(get).toHaveBeenCalledTimes(1)
      expect(get.mock.calls[0][0].mediation).toBeUndefined()
    })

    test('中止の後に conditional の結果が遅れて届いても finish を呼ばない（finish は明示側の1回だけ）', async () => {
      const { get, modal, pending } = conditionalBrowser({ ignoreAbort: true })
      modal.mockResolvedValue(fakeAssertion('modal'))
      vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      clickPasskeyButton()
      await settle()
      // 中止した get が片付くまで、次の ceremony は始まらない
      expect(get).toHaveBeenCalledTimes(1)
      pending[0].resolve(fakeAssertion('stale'))

      expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(vi.mocked(finishPasskeyLogin).mock.calls[0][0].credential.id).toBe('modal')
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    test('パスワードを送信すると conditional を中止する。結果が遅れて届いても finish は呼ばない', async () => {
      const { get, pending } = conditionalBrowser({ ignoreAbort: true })
      vi.mocked(login).mockReturnValue(new Promise(() => {}))
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      submit('alice', 'secret')

      await waitFor(() => expect(login).toHaveBeenCalledTimes(1))
      expect(pending[0].signal.aborted).toBe(true)
      pending[0].resolve(fakeAssertion('stale'))
      await settle()
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    test('画面を離れる（アンマウント）と中止する。結果が遅れて届いても finish は呼ばない', async () => {
      const { get, pending } = conditionalBrowser({ ignoreAbort: true })
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      cleanup()

      expect(pending[0].signal.aborted).toBe(true)
      pending[0].resolve(fakeAssertion('stale'))
      await settle()
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
    })

    test('StrictMode でも begin と get は1回だけで、後始末で中止される', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin({ strict: true })

      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
      await settle()

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(get).toHaveBeenCalledTimes(1)
      expect(pending[0].signal.aborted).toBe(false)

      cleanup()

      expect(pending[0].signal.aborted).toBe(true)
    })

    test('conditional の取り消し（NotAllowedError）は表示せず、1回だけ作り直す', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      pending[0].reject(new DOMException('x', 'NotAllowedError'))
      await advance(CONDITIONAL_MIN_INTERVAL_MS)

      await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()

      pending[1].reject(new DOMException('x', 'NotAllowedError'))
      await advance(CONDITIONAL_REFRESH_MS)

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    })

    test.each([
      ['429', new ApiError({ status: 429, code: 'too_many_requests' })],
      ['通信の失敗', new ApiError({ status: 0 }, 'サーバーに接続できませんでした。')],
    ])('conditional の begin が %s でも、表示せず、作り直さず、ボタンは使える', async (_name, error) => {
      const { get } = conditionalBrowser()
      vi.mocked(beginPasskeyLogin).mockRejectedValue(error)
      renderLogin()
      await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(1))

      await advance(CONDITIONAL_REFRESH_MS * 2)

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(get).not.toHaveBeenCalled()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
    })

    test('conditional の finish が401なら固定の文言を出し、1回作り直す', async () => {
      const { get, pending } = conditionalBrowser()
      vi.mocked(finishPasskeyLogin).mockRejectedValue(new UnauthorizedError())
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      pending[0].resolve(fakeAssertion())

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'パスキーでログインできませんでした。もう一度お試しください。',
      )
      await advance(CONDITIONAL_MIN_INTERVAL_MS)
      await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
      expect(finishPasskeyLogin).toHaveBeenCalledTimes(1)
    })

    test('conditional の finish が429ならロックの文言を出し、作り直さない', async () => {
      const { get, pending } = conditionalBrowser()
      vi.mocked(finishPasskeyLogin).mockRejectedValue(
        new ApiError({ status: 429, code: 'too_many_requests' }),
      )
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      pending[0].resolve(fakeAssertion())

      expect(await screen.findByRole('alert')).toHaveTextContent('しばらく操作できません')
      await advance(CONDITIONAL_REFRESH_MS * 2)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
    })

    test('明示のボタンが取り消されたら、conditional を1回戻す', async () => {
      const { get, modal } = conditionalBrowser()
      modal.mockRejectedValue(new DOMException('x', 'NotAllowedError'))
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      clickPasskeyButton()
      await waitFor(() => expect(modal).toHaveBeenCalledTimes(1))
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      await advance(CONDITIONAL_MIN_INTERVAL_MS)

      // 1: 最初の conditional、2: ボタン、3: 戻した conditional
      await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(3))
      expect(get.mock.calls[2][0].mediation).toBe('conditional')
    })

    test('パスワードが401なら conditional を戻し、429なら戻さない', async () => {
      const { get } = conditionalBrowser()
      vi.mocked(login)
        .mockRejectedValueOnce(new UnauthorizedError())
        .mockRejectedValueOnce(new ApiError({ status: 429, code: 'too_many_requests' }))
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      submit('alice', 'wrong')
      expect(await screen.findByRole('alert')).toHaveTextContent('ユーザー名またはパスワードが間違っています。')
      await advance(CONDITIONAL_MIN_INTERVAL_MS)
      await waitFor(() => expect(get).toHaveBeenCalledTimes(2))

      fireEvent.click(screen.getByRole('button', { name: 'ログイン' }))
      await waitFor(() => expect(login).toHaveBeenCalledTimes(2))
      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('しばらく操作できません'))
      await advance(CONDITIONAL_REFRESH_MS * 2)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
    })

    test('4分ごとに中止して作り直し、1回の表示での begin は上限で止まる', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      for (let n = 2; n <= CONDITIONAL_MAX_BEGINS; n += 1) {
        await advance(CONDITIONAL_REFRESH_MS)
        await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(n))
        expect(pending[n - 2].signal.aborted).toBe(true)
      }
      await advance(CONDITIONAL_REFRESH_MS * 2)

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(CONDITIONAL_MAX_BEGINS)
      expect(get).toHaveBeenCalledTimes(CONDITIONAL_MAX_BEGINS)
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    })

    test('begin の上限に達したあと更新の時期が来たら、待機を中止する（期限切れのチャレンジを残さない）', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
      for (let n = 2; n <= CONDITIONAL_MAX_BEGINS; n += 1) {
        await advance(CONDITIONAL_REFRESH_MS)
        await waitFor(() => expect(get).toHaveBeenCalledTimes(n))
      }
      const last = pending[CONDITIONAL_MAX_BEGINS - 1]
      expect(last.signal.aborted).toBe(false)

      await advance(CONDITIONAL_REFRESH_MS)

      expect(last.signal.aborted).toBe(true)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(CONDITIONAL_MAX_BEGINS)
      // 中止の後に結果が届いても、finish へ送らず、何も表示しない
      last.resolve(fakeAssertion('stale'))
      await settle()
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    })

    test('非表示のタブで開いたときは begin を送らず、表示に戻ったときに始める（上限にも含める）', async () => {
      const { get } = conditionalBrowser()
      setVisibility('hidden')
      renderLogin()

      await advance(CONDITIONAL_REFRESH_MS)

      expect(beginPasskeyLogin).not.toHaveBeenCalled()
      expect(get).not.toHaveBeenCalled()

      setVisibility('visible')
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
      // 表示のたびに始め直さない
      setVisibility('visible')
      await settle()
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)

      for (let n = 2; n <= CONDITIONAL_MAX_BEGINS; n += 1) {
        await advance(CONDITIONAL_REFRESH_MS)
        await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(n))
      }
      await advance(CONDITIONAL_REFRESH_MS * 2)
      expect(beginPasskeyLogin).toHaveBeenCalledTimes(CONDITIONAL_MAX_BEGINS)
    })

    describe('前の処理が片付かないとき', () => {
      /** 最初の conditional の begin が、解決するまで返らない */
      async function renderWithStuckBegin() {
        const browser = conditionalBrowser()
        browser.modal.mockResolvedValue(fakeAssertion('modal'))
        vi.mocked(finishPasskeyLogin).mockResolvedValue({ user })
        vi.mocked(login).mockResolvedValue({ user })
        let releaseFirst: () => void = () => {}
        vi.mocked(beginPasskeyLogin).mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              releaseFirst = () => resolve({ publicKey: assertionOptions })
            }),
        )
        renderLogin()
        await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(1))
        return { ...browser, releaseFirst: () => releaseFirst() }
      }

      test('上限まで待っても片付かなければ、begin を送らず固定の文言を出して idle に戻る', async () => {
        const { get, modal } = await renderWithStuckBegin()

        clickPasskeyButton()
        await advance(PREVIOUS_RUN_WAIT_MAX_MS - 1000)

        expect(screen.getByRole('button', { name: '確認中…' })).toBeDisabled()
        expect(screen.queryByRole('alert')).not.toBeInTheDocument()

        await advance(1000 + 10)

        expect(await screen.findByRole('alert')).toHaveTextContent(
          'パスキーでログインできませんでした。もう一度お試しください。',
        )
        // チャレンジのCookieが1つなので、待たずに begin を送らない
        expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
        expect(get).not.toHaveBeenCalled()
        expect(modal).not.toHaveBeenCalled()
        expect(finishPasskeyLogin).not.toHaveBeenCalled()
        expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
        expect(screen.getByRole('button', { name: 'ログイン' })).toBeEnabled()
      })

      test('idle に戻ったあとは、パスワードで送信できる', async () => {
        await renderWithStuckBegin()
        clickPasskeyButton()
        await advance(PREVIOUS_RUN_WAIT_MAX_MS + 10)
        await screen.findByRole('alert')

        submit('alice', 'secret')

        expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
        expect(vi.mocked(login).mock.calls[0][0]).toEqual({ username: 'alice', password: 'secret' })
      })

      test('タイムアウトした run が tail を塞がず、押し直せば先へ進む', async () => {
        const { modal } = await renderWithStuckBegin()
        clickPasskeyButton()
        await advance(PREVIOUS_RUN_WAIT_MAX_MS + 10)
        await screen.findByRole('alert')

        clickPasskeyButton()

        expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
        expect(beginPasskeyLogin).toHaveBeenCalledTimes(2)
        expect(modal).toHaveBeenCalledTimes(1)
        expect(vi.mocked(finishPasskeyLogin).mock.calls[0][0].credential.id).toBe('modal')
      })

      test('上限の前に片付いたときは、タイマーが残らず、エラーも出ない', async () => {
        const { releaseFirst } = await renderWithStuckBegin()
        clickPasskeyButton()
        await advance(PREVIOUS_RUN_WAIT_MAX_MS - 1000)

        releaseFirst()

        expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
        await advance(PREVIOUS_RUN_WAIT_MAX_MS * 2)
        expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      })
    })

    test('非表示のタブでは作り直さず、表示に戻ったときに作り直す', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      setVisibility('hidden')
      await advance(CONDITIONAL_REFRESH_MS + 1000)

      expect(beginPasskeyLogin).toHaveBeenCalledTimes(1)
      expect(pending[0].signal.aborted).toBe(false)

      setVisibility('visible')

      await waitFor(() => expect(beginPasskeyLogin).toHaveBeenCalledTimes(2))
      expect(pending[0].signal.aborted).toBe(true)
    })

    test('チャレンジが4分50秒より古いときは、結果を finish へ送らず固定の文言を出す', async () => {
      const { get, pending } = conditionalBrowser()
      renderLogin()
      await waitFor(() => expect(get).toHaveBeenCalledTimes(1))

      // 非表示で作り直しが止まっている間に、5分近く経った
      setVisibility('hidden')
      await advance(CHALLENGE_MAX_AGE_MS + 1000)
      pending[0].resolve(fakeAssertion())

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'パスキーでログインできませんでした。もう一度お試しください。',
      )
      expect(finishPasskeyLogin).not.toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'パスキーでログイン' })).toBeEnabled()
    })
  })
})
