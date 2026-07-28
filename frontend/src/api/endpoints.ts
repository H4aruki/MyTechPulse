import { apiClient } from './client'
import type {
  ClickArticleResponse,
  LoginRequest,
  LoginResponse,
  PersonalNewsResponse,
  SignupRequest,
  SignupResponse,
} from './types'

export function login(body: LoginRequest) {
  return apiClient.post<LoginResponse>('/auth/login_check', { body })
}

export function signup(body: SignupRequest) {
  return apiClient.post<SignupResponse>('/auth/create_user', { body })
}

export function fetchPersonalNews() {
  return apiClient.post<PersonalNewsResponse>('/news/personal_news', { auth: true })
}

/** 記事クリックを送信してタグの興味重みを更新させる（クリック学習） */
export function recordArticleClick(tags: string[]) {
  return apiClient.post<ClickArticleResponse>('/article/click', {
    auth: true,
    body: { tags },
  })
}
