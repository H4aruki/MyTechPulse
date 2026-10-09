import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { UnauthorizedError } from '@/api/client'
import { completeFeedbackFollowup, dismissFeedback, submitFeedbackOverall } from '@/api/endpoints'
import type { ShownPresentation } from '@/lib/feedbackPresentation'

export type { ShownPresentation }

const SCORES = [1, 2, 3, 4, 5] as const
const SCORE_LABELS: Record<number, string> = {
  1: 'とても不満',
  2: '不満',
  3: 'ふつう',
  4: '満足',
  5: 'とても満足',
}
const FOCUSABLE = 'button:not([disabled]), input:not([disabled])'

type Step = 'overall' | 'followup' | 'thanks'

interface FeedbackDialogProps {
  presentation: ShownPresentation
  onUnauthorized: () => void
  onClose: () => void
}

/** アンケートのポップアップ。設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md 第7章 */
export function FeedbackDialog({ presentation, onUnauthorized, onClose }: FeedbackDialogProps) {
  const { form, prompt_id: promptId } = presentation
  const questions = form.questions ?? []
  const root = questions.find((q) => q.display_if_question_id === undefined)
  const resumed = presentation.stage === 'followup' && !!presentation.submission_id
  const [step, setStep] = useState<Step>(resumed ? 'followup' : 'overall')
  const [submissionId, setSubmissionId] = useState<string | null>(
    presentation.submission_id ?? null,
  )
  const [overallScore, setOverallScore] = useState<number | null>(null)
  const [answers, setAnswers] = useState<Record<number, number>>({})
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus()
  }, [step])

  // 再開時は総合評価の値が手元に無い。部分回答は低評価のときだけできるので、追加質問をすべて出す
  const followups = questions.filter(
    (q) =>
      root !== undefined &&
      q.display_if_question_id === root.id &&
      (overallScore === null || overallScore <= (q.display_if_score_max ?? 0)),
  )
  const complete = followups.every((q) => !q.required || answers[q.id] !== undefined)

  const fail = (e: unknown) => {
    if (e instanceof UnauthorizedError) {
      onUnauthorized()
      return
    }
    setError(e instanceof Error ? e.message : '送信に失敗しました。もう一度お試しください。')
  }

  const close = () => {
    // 送信中に閉じると、回答と閉じた記録が同時に届き、回答が保存されないことがあるため受け付けない
    if (sending) return
    // 回答を終える前に閉じたら離脱として記録を試みる。届かなくても記事閲覧を優先するが、
    // 認証切れだけはログイン画面へ戻す
    if (step !== 'thanks') {
      void dismissFeedback(promptId).catch((e: unknown) => {
        if (e instanceof UnauthorizedError) onUnauthorized()
      })
    }
    onClose()
  }

  const chooseOverall = async (score: number) => {
    if (!root || sending) return
    setSending(true)
    setError(null)
    try {
      const res = await submitFeedbackOverall({ prompt_id: promptId, question_id: root.id, score })
      setOverallScore(score)
      if (res.followup_required) {
        setSubmissionId(res.submission_id)
        setStep('followup')
      } else {
        setStep('thanks')
      }
    } catch (e) {
      fail(e)
    } finally {
      setSending(false)
    }
  }

  const sendFollowup = async () => {
    if (!submissionId || !complete || sending) return
    setSending(true)
    setError(null)
    try {
      await completeFeedbackFollowup(
        submissionId,
        followups
          .filter((q) => answers[q.id] !== undefined)
          .map((q) => ({ question_id: q.id, score: answers[q.id] })),
      )
      setStep('thanks')
    } catch (e) {
      fail(e)
    } finally {
      setSending(false)
    }
  }

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      close()
      return
    }
    if (event.key !== 'Tab' || !dialogRef.current) return
    const items = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (items.length === 0) return
    const first = items[0]
    const last = items[items.length - 1]
    if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    } else if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/40 p-4 sm:items-center">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="feedback-title"
        onKeyDown={onKeyDown}
        className="max-h-[90vh] w-full max-w-md overflow-y-auto rounded-2xl bg-white p-6 shadow-xl"
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <h2 id="feedback-title" className="text-lg font-bold text-ink">
            {form.title}
          </h2>
          <button
            type="button"
            onClick={close}
            disabled={sending}
            aria-label="閉じる"
            className="rounded-lg px-2 text-xl leading-none text-ink-muted hover:bg-slate-100 disabled:opacity-50"
          >
            ×
          </button>
        </div>

        {step === 'overall' && root && (
          <fieldset>
            <legend className="mb-3 text-sm font-bold text-ink">{root.text}</legend>
            <div className="grid grid-cols-5 gap-2">
              {SCORES.map((score) => (
                <button
                  key={score}
                  type="button"
                  disabled={sending}
                  onClick={() => void chooseOverall(score)}
                  className="flex flex-col items-center rounded-lg border border-slate-300 px-1 py-2 text-sm hover:border-brand-500 disabled:opacity-50"
                >
                  <span className="font-bold">{score}</span>
                  <span className="text-[11px] leading-tight text-ink-muted">{SCORE_LABELS[score]}</span>
                </button>
              ))}
            </div>
          </fieldset>
        )}

        {step === 'followup' && (
          <form
            onSubmit={(event) => {
              event.preventDefault()
              void sendFollowup()
            }}
            className="space-y-4"
          >
            {followups.map((q) => (
              <fieldset key={q.id} aria-label={q.text}>
                <legend className="mb-2 text-sm font-bold text-ink">{q.text}</legend>
                <div className="grid grid-cols-5 gap-1">
                  {SCORES.map((score) => (
                    <label
                      key={score}
                      className="flex flex-col items-center rounded-lg border border-slate-200 px-1 py-2 text-xs"
                    >
                      <input
                        type="radio"
                        name={`feedback-${q.id}`}
                        value={score}
                        checked={answers[q.id] === score}
                        onChange={() => setAnswers((prev) => ({ ...prev, [q.id]: score }))}
                      />
                      <span className="font-bold">{score}</span>
                      <span className="text-[11px] leading-tight text-ink-muted">{SCORE_LABELS[score]}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
            ))}
            <button
              type="submit"
              disabled={!complete || sending}
              className="w-full rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white hover:bg-brand-700 disabled:opacity-50"
            >
              送信
            </button>
          </form>
        )}

        {step === 'thanks' && (
          <p role="status" className="py-4 text-center text-sm text-ink">
            ご回答ありがとうございました。
          </p>
        )}

        {error && (
          <p role="alert" className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </p>
        )}

        {step !== 'thanks' && (
          <p className="mt-4 text-xs text-ink-muted">
            回答はあなたのアカウントに紐づけて保存し、サービス改善の分析に使います。
          </p>
        )}
      </div>
    </div>
  )
}
