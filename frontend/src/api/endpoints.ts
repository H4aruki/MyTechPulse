import { request } from './client'
import type {
  AuthResponse,
  FeedbackAnswer,
  FeedbackDismissalResponse,
  FeedbackPresentationResponse,
  FeedbackStatusResponse,
  FeedbackSubmissionRequest,
  FeedbackSubmissionResponse,
  FeedResponse,
  LoginRequest,
  SignupRequest,
  User,
} from './types'

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

/** アンケートを出してよいかと、直近の状態（ログイン後・記事一覧の表示時に呼ぶ） */
export function fetchFeedbackStatus() {
  return request<FeedbackStatusResponse>('GET', '/api/v1/user-feedback/status')
}

/** アンケートの表示を要求する。promptId は要求ごとに作り、再送では同じ値を使う */
export function requestFeedbackPresentation(promptId: string) {
  return request<FeedbackPresentationResponse>('POST', '/api/v1/user-feedback/presentations', {
    prompt_id: promptId,
  })
}

export function submitFeedbackOverall(body: FeedbackSubmissionRequest) {
  return request<FeedbackSubmissionResponse>('POST', '/api/v1/user-feedback/submissions', body)
}

export function completeFeedbackFollowup(submissionId: string, answers: FeedbackAnswer[]) {
  return request<FeedbackSubmissionResponse>(
    'PUT',
    `/api/v1/user-feedback/submissions/${encodeURIComponent(submissionId)}`,
    { answers },
  )
}

export function dismissFeedback(promptId: string) {
  return request<FeedbackDismissalResponse>('POST', '/api/v1/user-feedback/dismissals', {
    prompt_id: promptId,
  })
}
