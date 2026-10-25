import type { QueryCache, QueryKey } from '@tanstack/react-query'
import type { PostHog } from 'posthog-js'
import { UnauthorizedError } from '@/api/client'

/** 分析（PostHog）の設定。どちらも環境変数で渡し、画面側には値を書かない */
export type AnalyticsEnv = {
  VITE_POSTHOG_KEY?: string
  VITE_POSTHOG_HOST?: string
}

/** 利用者名など個人データになりうる表示に付ける。クリックの記録とリプレイの文字から外れる */
export const NO_CAPTURE_CLASS = 'ph-no-capture'

let client: PostHog | null = null
let pendingUserId: string | null = null

/**
 * PostHog を初期化する。
 * - KEY が未設定なら、posthog-js を読み込まず、通信もしない。
 * - KEY があっても HOST が未設定なら、地域を決めずに無効にする。
 * 読み込みに失敗しても、画面の動作は妨げない。
 */
export async function initAnalytics(env: AnalyticsEnv = import.meta.env): Promise<void> {
  const key = env.VITE_POSTHOG_KEY?.trim()
  if (!key) return
  const host = env.VITE_POSTHOG_HOST?.trim()
  if (!host) {
    console.warn('VITE_POSTHOG_HOST が未設定のため、分析を無効にしました。')
    return
  }
  if (client) return

  try {
    const { default: posthog } = await import('posthog-js')
    posthog.init(key, {
      api_host: host,
      // 利用者名などを持たないため、識別した利用者だけをPersonとして作る
      person_profiles: 'identified_only',
      // SPAの画面遷移（history API）ごとにページ閲覧を記録する
      capture_pageview: 'history_change',
      autocapture: true,
      session_recording: {
        maskAllInputs: true,
        maskTextSelector: `.${NO_CAPTURE_CLASS}, [data-${NO_CAPTURE_CLASS}]`,
      },
    })
    client = posthog
    if (pendingUserId) client.identify(pendingUserId)
  } catch {
    // 分析の失敗で画面を壊さない
  }
}

/**
 * ログイン中の利用者を、内部の利用者ID（不透明な数値）だけで識別する。
 * 名前・メール・利用者名は渡さない。初期化の完了前に呼ばれたときは、完了後に反映する。
 */
export function identifyUser(id: number): void {
  const distinctId = String(id)
  if (pendingUserId === distinctId) return
  pendingUserId = distinctId
  client?.identify(distinctId)
}

/** ログアウト・認証切れのとき、識別を外して次の利用者と混ざらないようにする */
export function resetAnalytics(): void {
  pendingUserId = null
  client?.reset()
}

/**
 * セッションの終わりで識別を外す。戻り値は購読の解除関数。
 * - ログイン状態のキャッシュが消えたとき（ログアウト。各画面の leaveSession が消す）
 * - ログイン状態の確認（/auth/me）が401になったとき（セッション切れ。ProtectedRoute はキャッシュを消さずにログイン画面へ移す）
 */
export function resetOnSessionEnd(cache: QueryCache, authKey: QueryKey): () => void {
  return cache.subscribe((event) => {
    if (event.query.queryKey[0] !== authKey[0]) return
    if (
      event.type === 'removed' ||
      (event.type === 'updated' &&
        event.action.type === 'error' &&
        event.action.error instanceof UnauthorizedError)
    ) {
      resetAnalytics()
    }
  })
}

/** テスト用: モジュールの状態を初期に戻す */
export function resetAnalyticsStateForTest(): void {
  client = null
  pendingUserId = null
}
