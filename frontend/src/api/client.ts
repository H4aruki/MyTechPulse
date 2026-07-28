import { tokenStorage } from '@/lib/auth'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL

if (!API_BASE_URL) {
  throw new Error(
    'VITE_API_BASE_URL が未設定です。frontend/.env.example を参考に .env を作成してください。',
  )
}

/** トークン欠如・不正・期限切れ。呼び出し側でログイン画面へ戻す判断に使う */
export class UnauthorizedError extends Error {
  constructor() {
    super('認証の有効期限が切れました。再度ログインしてください。')
    this.name = 'UnauthorizedError'
  }
}

/** ネットワーク障害や5xxなど、status規約以前の失敗 */
export class ApiError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

interface RequestOptions {
  /** Authorization ヘッダーを付与するか（保護対象エンドポイント用） */
  auth?: boolean
  body?: unknown
}

/**
 * 全API呼び出しの共通処理。
 * 401は既存のstatus規約の例外としてHTTPステータスで返るため、ここでUnauthorizedErrorに変換する。
 */
async function post<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { auth = false, body } = options

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (auth) {
    const token = tokenStorage.get()
    if (!token) throw new UnauthorizedError()
    headers.Authorization = `Bearer ${token}`
  }

  let response: Response
  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method: 'POST',
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError('サーバーに接続できませんでした。通信環境を確認してください。')
  }

  if (response.status === 401) throw new UnauthorizedError()

  if (!response.ok) {
    throw new ApiError(`サーバーエラーが発生しました（HTTP ${response.status}）。`)
  }

  try {
    return (await response.json()) as T
  } catch {
    throw new ApiError('サーバーの応答を解釈できませんでした。')
  }
}

export const apiClient = { post }
