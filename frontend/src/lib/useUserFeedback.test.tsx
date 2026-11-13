import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { UnauthorizedError } from '@/api/client'
import { fetchFeedbackStatus, requestFeedbackPresentation } from '@/api/endpoints'
import type { FeedbackPresentationResponse } from '@/api/types'
import { clearFeedbackState } from './feedbackTracker'
import { useUserFeedback } from './useUserFeedback'

vi.mock('@/api/endpoints', () => ({
  fetchFeedbackStatus: vi.fn(),
  requestFeedbackPresentation: vi.fn(),
}))

function shown(promptId: string): FeedbackPresentationResponse {
  return {
    eligible: true,
    status: 'shown',
    stage: 'overall',
    prompt_id: promptId,
    form: {
      title: 'おすすめ記事についてのアンケート',
      version: 1,
      questions: [
        { id: 10, key: 'overall', text: '今日のおすすめ記事は役に立ちましたか？', sort_order: 1, required: true },
      ],
    },
  }
}

function Probe({ onUnauthorized }: { onUnauthorized: () => void }) {
  const feedback = useUserFeedback(onUnauthorized)
  return (
    <div>
      {[1, 2, 3, 4, 5, 6, 7].map((n) => (
        <button
          key={n}
          type="button"
          onClick={() => feedback.articleOpened(`https://example.com/${n}`, Promise.resolve())}
        >
          記事{n}を開く
        </button>
      ))}
      {feedback.presentation ? (
        <>
          <p>{feedback.presentation.form.title}</p>
          <button type="button" onClick={feedback.close}>
            アンケートを閉じる
          </button>
        </>
      ) : (
        <p>アンケートは出ていません</p>
      )}
    </div>
  )
}

function open(n: number) {
  fireEvent.click(screen.getByRole('button', { name: `記事${n}を開く` }))
}

// 表示要求は settleWithin のあと（マイクロタスク）で飛ぶ。
// 開いた直後に同期で回数を見ると、重ね要求の防止を外しても通ってしまう。
function waitForQueuedRequest() {
  return act(() => new Promise((resolve) => setTimeout(resolve, 0)))
}

describe('useUserFeedback', () => {
  beforeEach(() => {
    clearFeedbackState()
    window.sessionStorage.clear()
    vi.mocked(fetchFeedbackStatus).mockReset().mockResolvedValue({ eligible: true })
    vi.mocked(requestFeedbackPresentation).mockReset()
  })
  afterEach(() => {
    cleanup()
    clearFeedbackState()
  })

  test('表示要求が終わるまで、またアンケートを出しているあいだは、記事を開いても重ねて要求しない', async () => {
    let release: (value: FeedbackPresentationResponse) => void = () => {}
    vi.mocked(requestFeedbackPresentation).mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    render(<Probe onUnauthorized={vi.fn()} />)
    await screen.findByText('アンケートは出ていません')

    open(1)
    open(2)
    open(3)
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    open(4)
    await waitForQueuedRequest()
    expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1)

    const promptId = vi.mocked(requestFeedbackPresentation).mock.calls[0][0]
    await act(async () => {
      release(shown(promptId))
    })

    expect(await screen.findByText('おすすめ記事についてのアンケート')).toBeInTheDocument()
    // 表示許可で履歴は消える。1件だけ開いても3件に足りず、出しているあいだ止める仕組みを外しても落ちない。
    open(5)
    open(6)
    open(7)
    await waitForQueuedRequest()
    expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1)
  })

  test('表示中に開いた記事も数に入れ、閉じたあと合わせて3件になると、もう一度表示を要求する', async () => {
    vi.mocked(requestFeedbackPresentation).mockImplementation(async (promptId: string) => shown(promptId))
    render(<Probe onUnauthorized={vi.fn()} />)
    await screen.findByText('アンケートは出ていません')

    open(1)
    open(2)
    open(3)
    expect(await screen.findByText('おすすめ記事についてのアンケート')).toBeInTheDocument()
    open(4)
    await waitForQueuedRequest()
    expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'アンケートを閉じる' }))
    expect(screen.getByText('アンケートは出ていません')).toBeInTheDocument()

    open(5)
    expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1)
    open(6)
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(2))
    expect(screen.getByText('おすすめ記事についてのアンケート')).toBeInTheDocument()
  })

  test('表示できる結果でなければアンケートを出さず、開いた記事の数は残して次も要求する', async () => {
    vi.mocked(requestFeedbackPresentation).mockResolvedValue({
      eligible: true,
      status: 'submitted',
      prompt_id: '11111111-1111-4111-8111-111111111111',
    })
    render(<Probe onUnauthorized={vi.fn()} />)
    await screen.findByText('アンケートは出ていません')

    open(1)
    open(2)
    open(3)
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    expect(screen.getByText('アンケートは出ていません')).toBeInTheDocument()

    open(4)
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(2))
    expect(screen.getByText('アンケートは出ていません')).toBeInTheDocument()
  })

  test('表示要求が認証切れなら、ログインへ戻し、開いた記事の数を捨てる', async () => {
    vi.mocked(requestFeedbackPresentation).mockRejectedValue(new UnauthorizedError())
    const onUnauthorized = vi.fn()
    render(<Probe onUnauthorized={onUnauthorized} />)
    await screen.findByText('アンケートは出ていません')

    open(1)
    open(2)
    open(3)
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
    expect(screen.getByText('アンケートは出ていません')).toBeInTheDocument()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()

    open(4)
    await act(async () => {
      await Promise.resolve()
    })
    expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1)
  })

  test('画面を離れたあとに届いた状態照会では、表示を要求しない', async () => {
    let resolveStatus: (value: { eligible: boolean }) => void = () => {}
    vi.mocked(fetchFeedbackStatus).mockReturnValue(
      new Promise((resolve) => {
        resolveStatus = resolve
      }),
    )
    window.sessionStorage.setItem(
      'mtp.feedback.openedArticles',
      JSON.stringify(['https://example.com/1', 'https://example.com/2', 'https://example.com/3']),
    )
    const { unmount } = render(<Probe onUnauthorized={vi.fn()} />)
    unmount()

    await act(async () => {
      resolveStatus({ eligible: true })
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(requestFeedbackPresentation).not.toHaveBeenCalled()
  })
})
