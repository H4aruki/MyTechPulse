import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, type Location } from 'react-router-dom'
import { ApiError, rateLimitMessage, UnauthorizedError } from '@/api/client'
import { loginRedirectState } from '@/lib/returnTo'

interface SaveButtonProps {
  saved: boolean
  /** 保存するなら true、外すなら false。失敗したら reject する */
  onToggle: (save: boolean) => Promise<unknown>
}

/** 保存の直後に「取り消す」を出しておく時間 */
const UNDO_VISIBLE_MS = 5000

interface Failure {
  save: boolean
  error: unknown
}

function failureMessage({ save, error }: Failure, location: Location): ReactNode {
  if (error instanceof UnauthorizedError) {
    return (
      <>
        保存するにはログインが必要です。
        <Link
          to="/login"
          state={loginRedirectState(location)}
          className="font-bold underline"
        >
          ログインする
        </Link>
      </>
    )
  }
  if (error instanceof ApiError && error.problem.status === 409) {
    return '保存は500件まで。いらない保存を外してください'
  }
  if (error instanceof ApiError && error.problem.status === 429) {
    return rateLimitMessage(error.retryAfterSeconds)
  }
  return save
    ? '保存できませんでした。時間をおいて再度お試しください。'
    : '保存を外せませんでした。時間をおいて再度お試しください。'
}

/** 記事カードの右上に置く、保存の切り替えボタン。カードの「開く」ボタンの兄弟として置く */
export function SaveButton({ saved, onToggle }: SaveButtonProps) {
  const location = useLocation()
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<Failure | null>(null)

  const [undoVisible, setUndoVisible] = useState(false)
  const undoTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearUndoTimer = () => {
    if (undoTimer.current !== null) {
      clearTimeout(undoTimer.current)
      undoTimer.current = null
    }
  }

  useEffect(() => clearUndoTimer, [])

  const run = async (save: boolean) => {
    if (pending) return
    clearUndoTimer()
    setUndoVisible(false)
    setPending(true)
    setFailure(null)
    try {
      await onToggle(save)
      if (save) {
        // 成功が分かってから出す。もう一度保存したら5秒を数え直す
        setUndoVisible(true)
        undoTimer.current = setTimeout(() => {
          undoTimer.current = null
          setUndoVisible(false)
        }, UNDO_VISIBLE_MS)
      }
    } catch (error) {
      setFailure({ save, error })
    } finally {
      setPending(false)
    }
  }

  const handleClick = () => run(!saved)
  const handleUndo = () => run(false)

  return (
    <>
      <button
        type="button"
        aria-pressed={saved}
        aria-label={saved ? '保存を外す' : '保存する'}
        disabled={pending}
        onClick={handleClick}
        className="absolute top-1 right-1 flex h-11 w-11 cursor-pointer items-center justify-center rounded-full text-brand-700 transition hover:bg-brand-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 disabled:cursor-not-allowed disabled:opacity-60"
      >
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          className="h-6 w-6"
          fill={saved ? 'currentColor' : 'none'}
          stroke="currentColor"
          strokeWidth={2}
          strokeLinejoin="round"
        >
          <path d="M6 3h12a1 1 0 0 1 1 1v17l-7-4-7 4V4a1 1 0 0 1 1-1z" />
        </svg>
      </button>
      <div role="status">
        {undoVisible && (
          <p className="mt-2 flex items-center gap-3 rounded-lg bg-brand-50 px-3 py-2 text-xs text-brand-700">
            保存しました
            <button
              type="button"
              disabled={pending}
              onClick={handleUndo}
              className="cursor-pointer font-bold underline focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 disabled:cursor-not-allowed disabled:opacity-60"
            >
              取り消す
            </button>
          </p>
        )}
      </div>
      {failure && (
        <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">
          {failureMessage(failure, location)}
        </p>
      )}
    </>
  )
}
