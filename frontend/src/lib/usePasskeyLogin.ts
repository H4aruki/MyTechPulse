import { useCallback, useEffect, useState } from 'react'
import type { AuthResponse } from '@/api/types'
import { PasskeyLoginController, type PasskeyLoginPhase } from './passkeyLogin'

export type { PasskeyLoginPhase }

export interface UsePasskeyLoginOptions {
  onLoggedIn: (data: AuthResponse) => void
  /** 固定文言だけを渡す */
  onError: (message: string) => void
}

export interface UsePasskeyLoginResult {
  phase: PasskeyLoginPhase
  /** 明示のパスキー・パスワード・finish の送信中 */
  busy: boolean
  startExplicit: () => void
  /** 受け付けたら true。conditional は止める */
  startPassword: () => boolean
  endPassword: (outcome: 'failed' | 'locked' | 'error') => void
}

/** ログイン画面のパスキー（ボタンと conditional UI）の進行。詳しい規則は passkeyLogin.ts */
export function usePasskeyLogin({ onLoggedIn, onError }: UsePasskeyLoginOptions): UsePasskeyLoginResult {
  const [controller] = useState(() => new PasskeyLoginController())
  const [phase, setPhase] = useState<PasskeyLoginPhase>('idle')

  // 最新の関数へ差し替える。controller 自体は作り直さない
  useEffect(() => {
    controller.handlers = { onPhase: setPhase, onLoggedIn, onError }
  }, [controller, onLoggedIn, onError])

  useEffect(() => {
    controller.start()
    return () => controller.dispose()
  }, [controller])

  const startExplicit = useCallback(() => controller.startExplicit(), [controller])
  const startPassword = useCallback(() => controller.startPassword(), [controller])
  const endPassword = useCallback(
    (outcome: 'failed' | 'locked' | 'error') => controller.endPassword(outcome),
    [controller],
  )

  return {
    phase,
    busy: phase === 'explicit' || phase === 'password' || phase === 'finishing',
    startExplicit,
    startPassword,
    endPassword,
  }
}
