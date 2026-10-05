import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError } from '@/api/client'
import { signup } from '@/api/endpoints'
import { authQueryKey } from '@/lib/auth'
import { SignupPage } from './SignupPage'

vi.mock('@/api/endpoints', () => ({ signup: vi.fn() }))

const user = { id: 2, username: 'bob', role: 'member' as const }
const NEXT_BUTTON = '次へ：興味のあるタグを選ぶ'

function renderSignup() {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/signup']}>
        <Routes>
          <Route path="/signup" element={<SignupPage />} />
          <Route path="/articles" element={<h1>記事一覧</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

function fillAccount(username: string, password: string) {
  fireEvent.change(screen.getByLabelText('ユーザー名'), { target: { value: username } })
  fireEvent.change(screen.getByLabelText('パスワード'), { target: { value: password } })
  fireEvent.click(screen.getByRole('button', { name: NEXT_BUTTON }))
}

async function goToTagStep(username = 'bob', password = 'secret') {
  fillAccount(username, password)
  await screen.findByRole('heading', { name: '興味のあるタグを選択してください' })
}

function selectTags(...tags: string[]) {
  for (const tag of tags) {
    fireEvent.click(screen.getByRole('checkbox', { name: tag }))
  }
}

function clickRegister() {
  fireEvent.click(screen.getByRole('button', { name: /^登録する/ }))
}

describe('SignupPage', () => {
  beforeEach(() => {
    vi.mocked(signup).mockReset()
  })
  afterEach(() => {
    cleanup()
  })

  test('成功すると新しい項目名で送信し、利用者をauthクエリへ入れて遷移する', async () => {
    vi.mocked(signup).mockResolvedValue({ user })
    const client = renderSignup()

    await goToTagStep(' bob ', 'secret')
    selectTags('Go', 'React')
    clickRegister()

    expect(await screen.findByRole('heading', { name: '記事一覧' })).toBeInTheDocument()
    // useMutation は第2引数に実行情報も渡すため、送信内容だけを確認する
    expect(vi.mocked(signup).mock.calls[0][0]).toEqual({
      username: 'bob',
      password: 'secret',
      favorite_tags: ['Go', 'React'],
    })
    expect(client.getQueryData(authQueryKey)).toEqual(user)
  })

  test('409は同名の利用者がいる旨を出して最初の手順へ戻る', async () => {
    vi.mocked(signup).mockRejectedValue(new ApiError({ status: 409, code: 'username_taken' }))
    renderSignup()

    await goToTagStep()
    selectTags('Go')
    clickRegister()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'このユーザー名は既に使用されています',
    )
    expect(screen.getByRole('heading', { name: '新規登録' })).toBeInTheDocument()
  })

  test('422は入力内容の誤りを出して最初の手順へ戻る', async () => {
    vi.mocked(signup).mockRejectedValue(new ApiError({ status: 422, code: 'validation_failed' }))
    renderSignup()

    await goToTagStep()
    selectTags('Go')
    clickRegister()

    expect(await screen.findByRole('alert')).toHaveTextContent('入力内容に誤りがあります')
    expect(screen.getByRole('heading', { name: '新規登録' })).toBeInTheDocument()
  })

  test('その他の失敗はApiErrorの文言を出して最初の手順へ戻り、再送しない', async () => {
    vi.mocked(signup).mockRejectedValue(new ApiError({ status: 503 }, '混み合っています。'))
    renderSignup()

    await goToTagStep()
    selectTags('Go')
    clickRegister()

    expect(await screen.findByRole('alert')).toHaveTextContent('混み合っています。')
    expect(screen.getByRole('heading', { name: '新規登録' })).toBeInTheDocument()
    expect(signup).toHaveBeenCalledTimes(1)
  })

  test('空白だけのユーザー名は次の手順へ進めない', async () => {
    renderSignup()

    fillAccount('   ', 'secret')

    expect(await screen.findByText('ユーザー名を入力してください')).toBeInTheDocument()
  })

  test('ユーザー名は51文字以上を受け付けない', async () => {
    renderSignup()

    fillAccount('a'.repeat(51), 'secret')

    expect(await screen.findByText('ユーザー名は50文字以内で入力してください')).toBeInTheDocument()
  })

  test('パスワードは文字数でなくバイト数で72までに制限する', async () => {
    renderSignup()

    // 全角25文字は75バイトになる
    fillAccount('bob', 'あ'.repeat(25))

    expect(
      await screen.findByText('パスワードは72バイト以内で入力してください'),
    ).toBeInTheDocument()
    expect(signup).not.toHaveBeenCalled()
  })

  test('全角24文字（72バイト）のパスワードは受け付ける', async () => {
    renderSignup()

    await goToTagStep('bob', 'あ'.repeat(24))

    expect(
      screen.getByRole('heading', { name: '興味のあるタグを選択してください' }),
    ).toBeInTheDocument()
  })

  test('タグが未選択なら登録できない', async () => {
    renderSignup()

    await goToTagStep()

    expect(screen.getByRole('button', { name: /^登録する/ })).toBeDisabled()
  })
})
