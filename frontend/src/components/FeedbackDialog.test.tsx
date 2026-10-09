import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { completeFeedbackFollowup, dismissFeedback, submitFeedbackOverall } from '@/api/endpoints'
import { FeedbackDialog, type ShownPresentation } from './FeedbackDialog'

vi.mock('@/api/endpoints', () => ({
  submitFeedbackOverall: vi.fn(),
  completeFeedbackFollowup: vi.fn(),
  dismissFeedback: vi.fn(),
}))

const PROMPT = '11111111-1111-4111-8111-111111111111'
const SUBMISSION = '22222222-2222-4222-8222-222222222222'

function presentation(overrides: Partial<ShownPresentation> = {}): ShownPresentation {
  return {
    eligible: true,
    status: 'shown',
    stage: 'overall',
    prompt_id: PROMPT,
    form: {
      title: 'おすすめ記事についてのアンケート',
      version: 1,
      questions: [
        { id: 10, key: 'overall', text: '今日のおすすめ記事は役に立ちましたか？', sort_order: 1, required: true },
        { id: 11, key: 'interest_match', text: '興味に合っていましたか？', sort_order: 2, required: true, display_if_question_id: 10, display_if_score_max: 2 },
        { id: 12, key: 'freshness', text: '新しさに満足しましたか？', sort_order: 3, required: true, display_if_question_id: 10, display_if_score_max: 2 },
        { id: 13, key: 'usability', text: '使いやすかったですか？', sort_order: 4, required: true, display_if_question_id: 10, display_if_score_max: 2 },
      ],
    },
    ...overrides,
  }
}

function renderDialog(p = presentation()) {
  const onClose = vi.fn()
  const onUnauthorized = vi.fn()
  render(<FeedbackDialog presentation={p} onClose={onClose} onUnauthorized={onUnauthorized} />)
  return { onClose, onUnauthorized }
}

const FOLLOWUP_TEXTS = ['興味に合っていましたか？', '新しさに満足しましたか？', '使いやすかったですか？']

describe('FeedbackDialog', () => {
  beforeEach(() => {
    vi.mocked(submitFeedbackOverall).mockReset()
    vi.mocked(completeFeedbackFollowup).mockReset()
    vi.mocked(dismissFeedback)
      .mockReset()
      .mockResolvedValue({ prompt_id: PROMPT, status: 'dismissed', stage: 'overall' })
  })
  afterEach(() => cleanup())

  test('最初は総合評価の1問だけを、意味の分かるラベル付きで出し、操作位置を移す', () => {
    renderDialog()
    expect(screen.getByRole('dialog', { name: 'おすすめ記事についてのアンケート' })).toBeInTheDocument()
    expect(screen.getByText('今日のおすすめ記事は役に立ちましたか？')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /5\s*とても満足/ })).toBeInTheDocument()
    expect(screen.queryByText('興味に合っていましたか？')).not.toBeInTheDocument()
    expect(screen.getByText(/アカウントに紐づけて保存し、サービス改善の分析に使います/)).toBeInTheDocument()
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  test('3〜5を選ぶと保存してお礼を出し、閉じても離脱を記録しない', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'completed', followup_required: false })
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /4\s*満足/ }))
    expect(await screen.findByText('ご回答ありがとうございました。')).toBeInTheDocument()
    expect(submitFeedbackOverall).toHaveBeenCalledWith({ prompt_id: PROMPT, question_id: 10, score: 4 })
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    expect(dismissFeedback).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  test('1〜2を選ぶと追加3問を同じ画面に出し、全部選ぶまで送信できない', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'partial', followup_required: true })
    vi.mocked(completeFeedbackFollowup).mockResolvedValue({ submission_id: SUBMISSION, status: 'completed', followup_required: false })
    renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /2\s*不満/ }))
    expect(await screen.findByText('興味に合っていましたか？')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: '送信' })
    expect(send).toBeDisabled()
    for (const name of FOLLOWUP_TEXTS) {
      fireEvent.click(screen.getByRole('group', { name }).querySelector('input[value="3"]')!)
    }
    expect(send).toBeEnabled()
    fireEvent.click(send)
    expect(await screen.findByText('ご回答ありがとうございました。')).toBeInTheDocument()
    expect(completeFeedbackFollowup).toHaveBeenCalledWith(SUBMISSION, [
      { question_id: 11, score: 3 },
      { question_id: 12, score: 3 },
      { question_id: 13, score: 3 },
    ])
  })

  test('追加質問の保存に失敗しても選択を残して再送信でき、途中で閉じると離脱を記録する', async () => {
    vi.mocked(submitFeedbackOverall).mockResolvedValue({ submission_id: SUBMISSION, status: 'partial', followup_required: true })
    vi.mocked(completeFeedbackFollowup).mockRejectedValue(new ApiError({ status: 500 }))
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /1\s*とても不満/ }))
    await screen.findByText('興味に合っていましたか？')
    for (const name of FOLLOWUP_TEXTS) {
      fireEvent.click(screen.getByRole('group', { name }).querySelector('input[value="2"]')!)
    }
    fireEvent.click(screen.getByRole('button', { name: '送信' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: '興味に合っていましたか？' }).querySelector('input[value="2"]'),
    ).toBeChecked()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(dismissFeedback).toHaveBeenCalledWith(PROMPT)
    expect(onClose).toHaveBeenCalled()
  })

  test('総合評価の保存に失敗したら追加質問へ進まず、もう一度選べる', async () => {
    vi.mocked(submitFeedbackOverall).mockRejectedValueOnce(new ApiError({ status: 0 }, '接続できません'))
    renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /2\s*不満/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('接続できません')
    expect(screen.queryByText('興味に合っていましたか？')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /2\s*不満/ })).toBeEnabled())
  })

  test('認証切れはログイン画面へ戻す', async () => {
    vi.mocked(submitFeedbackOverall).mockRejectedValue(new UnauthorizedError())
    const { onUnauthorized } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /5\s*とても満足/ }))
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
  })

  test('閉じたときの記録が認証切れで失敗したら、ログイン画面へ戻す', async () => {
    vi.mocked(dismissFeedback).mockRejectedValue(new UnauthorizedError())
    const { onClose, onUnauthorized } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: '閉じる' }))
    expect(onClose).toHaveBeenCalled()
    await waitFor(() => expect(onUnauthorized).toHaveBeenCalled())
  })

  test('送信中は閉じられず、閉じた記録と回答が同時に送られない', async () => {
    let finish: (v: { submission_id: string; status: 'completed'; followup_required: boolean }) => void = () => {}
    vi.mocked(submitFeedbackOverall).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: /4\s*満足/ }))
    expect(screen.getByRole('button', { name: '閉じる' })).toBeDisabled()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(dismissFeedback).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
    finish({ submission_id: SUBMISSION, status: 'completed', followup_required: false })
    expect(await screen.findByText('ご回答ありがとうございました。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '閉じる' })).toBeEnabled()
  })

  test('部分回答が保存済みの再送結果では、追加質問から始める', () => {
    renderDialog(presentation({ stage: 'followup', submission_id: SUBMISSION }))
    expect(screen.getByText('興味に合っていましたか？')).toBeInTheDocument()
  })

  test('Tabキーの移動はポップアップの中で回る', () => {
    renderDialog()
    const dialog = screen.getByRole('dialog')
    const focusables = dialog.querySelectorAll<HTMLElement>('button:not([disabled])')
    focusables[focusables.length - 1].focus()
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(document.activeElement).toBe(focusables[0])
  })
})
