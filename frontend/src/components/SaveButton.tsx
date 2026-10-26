import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, rateLimitMessage, UnauthorizedError } from '@/api/client'

interface SaveButtonProps {
  saved: boolean
  /** 保存するなら true、外すなら false。失敗したら reject する */
  onToggle: (save: boolean) => Promise<unknown>
}

interface Failure {
  save: boolean
  error: unknown
}

function failureMessage({ save, error }: Failure): ReactNode {
  if (error instanceof UnauthorizedError) {
    return (
      <>
        保存するにはログインが必要です。
        <Link to="/login" className="font-bold underline">
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
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<Failure | null>(null)

  const handleClick = async () => {
    if (pending) return
    const save = !saved
    setPending(true)
    setFailure(null)
    try {
      await onToggle(save)
    } catch (error) {
      setFailure({ save, error })
    } finally {
      setPending(false)
    }
  }

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
      {failure && (
        <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">
          {failureMessage(failure)}
        </p>
      )}
    </>
  )
}
