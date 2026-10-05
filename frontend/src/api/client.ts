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

  constructor(problem: PartialProblem, message?: string) {
    super(message ?? problem.detail ?? `サーバーエラーが発生しました（HTTP ${problem.status}）。`)
    this.name = 'ApiError'
    this.problem = problem
  }
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
  method: 'GET' | 'POST',
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
    throw new ApiError(problem)
  }

  if (response.status === 204) return undefined as T

  try {
    return (await response.json()) as T
  } catch {
    throw new ApiError({ status: response.status }, 'サーバーの応答を解釈できませんでした。')
  }
}
