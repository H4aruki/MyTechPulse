import { request } from './client'
import type { AuthResponse, FeedResponse, LoginRequest, SignupRequest, User } from './types'

export function login(body: LoginRequest) {
  return request<AuthResponse>('POST', '/api/v1/auth/login', body)
}

export function signup(body: SignupRequest) {
  return request<AuthResponse>('POST', '/api/v1/auth/signup', body)
}

/** ログイン中の利用者。未ログインなら401（UnauthorizedError） */
export function currentUser() {
  return request<User>('GET', '/api/v1/auth/me')
}

export function logout() {
  return request<void>('POST', '/api/v1/auth/logout')
}

export function fetchFeed() {
  return request<FeedResponse>('GET', '/api/v1/feed')
}

/** 記事クリックを送信してタグの興味重みを更新させる（クリック学習） */
export function recordArticleClick(tags: string[]) {
  return request<void>('POST', '/api/v1/feedback/article-clicks', { tags })
}
