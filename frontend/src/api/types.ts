// backend/app/schemas/ の Pydantic スキーマに対応する型定義。
// 片側だけ変更すると齟齬が出るため、スキーマを変更した際は必ず両方を更新すること。

/**
 * このAPIはHTTPステータスではなくレスポンスボディの `status`（int）で結果を表す規約。
 * 値の意味を変更する場合はバックエンドと必ず揃えること。
 */
export const ApiStatus = {
  /** 成功 */
  SUCCESS: 1,
  /**
   * 失敗。login_check では「認証失敗」（ユーザー名列挙攻撃を防ぐため、
   * ユーザー不存在とパスワード不一致を区別しない）。
   * create_user では「ユーザー名が既に使用されている」。
   */
  FAILURE: 2,
} as const

export type ApiStatusValue = (typeof ApiStatus)[keyof typeof ApiStatus]

/** POST /auth/login_check */
export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  status: number
  access_token?: string | null
}

/** POST /auth/create_user */
export interface SignupRequest {
  newusername: string
  newpassword: string
  favoritetags: string[]
}

export interface SignupResponse {
  status: number
  access_token?: string | null
}

/** 記事1件。Qiita / Zenn 双方で同じ形に正規化されて返る */
export interface Article {
  title: string
  url: string
  source: string
  tags: string[]
  likes: number
  published_at: string
}

/** POST /news/personal_news */
export interface PersonalNewsResponse {
  status: number
  qiita_articles: Article[]
  zenn_articles: Article[]
}

/** POST /article/click */
export interface ClickArticleRequest {
  tags: string[]
}

export interface ClickArticleResponse {
  status: number
}
