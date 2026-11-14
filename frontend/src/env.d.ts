/// <reference types="vite/client" />

// 分析（PostHog）の環境変数。どちらも任意で、未設定なら分析は動かない
interface ImportMetaEnv {
  readonly VITE_POSTHOG_KEY?: string
  readonly VITE_POSTHOG_HOST?: string
}
