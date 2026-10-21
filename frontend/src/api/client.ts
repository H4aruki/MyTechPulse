import type { components } from './generated'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL

if (!API_BASE_URL) {
  throw new Error(
    'VITE_API_BASE_URL が未設定です。frontend/.env.example を参考に .env を作成してください。',
  )
}

/** Go版APIが返す標準のエラー形式（RFC 9457 Problem Details） */
export type ProblemDetails = components['schemas']['ErrorModel']

type PartialProblem = Pick<ProblemDetails, 'status'> & Partial<ProblemDetails>

/** ログインしていない、またはセッションが切れている（HTTP 401） */
export class UnauthorizedError extends Error {
  constructor() {
    super('認証の有効期限が切れました。再度ログインしてください。')
    this.name = 'UnauthorizedError'
  }
}

/** 401以外の失敗。403/409/422/5xxや通信障害の区別を problem に保つ */
export class ApiError extends Error {
  readonly problem: PartialProblem
  /** 429のときだけ。Retry-After ヘッダー（秒）が読めたら入る */
  readonly retryAfterSeconds?: number

  constructor(problem: PartialProblem, message?: string, retryAfterSeconds?: number) {
    super(message ?? problem.detail ?? `サーバーエラーが発生しました（HTTP ${problem.status}）。`)
    this.name = 'ApiError'
    this.problem = problem
    this.retryAfterSeconds = retryAfterSeconds
  }
}

/** 回数制限（429）で利用者に見せる文言。待ち時間が分かれば添える */
export function rateLimitMessage(retryAfterSeconds?: number): string {
  const wait =
    retryAfterSeconds !== undefined && retryAfterSeconds > 0
      ? `${formatWait(retryAfterSeconds)}ほど`
      : 'しばらく'
  return `操作が多すぎます。${wait}待ってからもう一度お試しください。`
}

function formatWait(seconds: number): string {
  if (seconds < 60) return `${seconds}秒`
  return `${Math.ceil(seconds / 60)}分`
}

function parseRetryAfter(response: Response): number | undefined {
  const value = Number(response.headers.get('Retry-After'))
  return Number.isInteger(value) && value > 0 ? value : undefined
}

function isProblem(response: Response): boolean {
  return (response.headers.get('Content-Type') ?? '').includes('application/problem+json')
}

/**
 * 全API呼び出しの共通処理。
 * 認証はHttpOnly Cookieなので、JavaScriptからは値に触れず credentials: 'include' だけ指定する。
 * 状態を変える要求にはCSRF対策のヘッダーを付ける。
 */
export async function request<T>(
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  body?: unknown,
): Promise<T> {
  let response: Response
  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method,
      credentials: 'include',
      headers:
        method === 'GET'
          ? { Accept: 'application/json' }
          : {
              Accept: 'application/json',
              'Content-Type': 'application/json',
              'X-MTP-CSRF': '1',
            },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError({ status: 0 }, 'サーバーに接続できませんでした。通信環境を確認してください。')
  }

  if (response.status === 401) throw new UnauthorizedError()

  if (!response.ok) {
    let problem: PartialProblem = { status: response.status }
    if (isProblem(response)) {
      try {
        problem = (await response.json()) as ProblemDetails
      } catch {
        // 本文が壊れていてもステータスだけで失敗を表す
      }
    }
    if (response.status === 429) {
      const retryAfter = parseRetryAfter(response)
      throw new ApiError(problem, rateLimitMessage(retryAfter), retryAfter)
    }
    throw new ApiError(problem)
  }

  if (response.status === 204) return undefined as T

  try {
    return (await response.json()) as T
  } catch {
    throw new ApiError({ status: response.status }, 'サーバーの応答を解釈できませんでした。')
  }
}
