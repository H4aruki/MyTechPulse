// パスキーでのログイン（ボタンの明示ceremonyと、利用者名欄の conditional UI）の進行を1つにまとめる。
//
// 前提（サーバー）: チャレンジのトークンは、begin の応答本文（challengeToken）で受け取り、finish の要求本文で返す。
// Cookieは使わないので、トークンは run ごとに独立し、別のタブの begin に上書きされない。トークンは5分で切れ、
// finish で成否にかかわらず消費される。古い・期限切れ・再利用のトークンへの finish は401になり、失敗回数にも数えられる。
// そのため「finish へ送るのは、生きている run の応答だけ」にする。
//   - 直列化: begin の前に、前の run が片付くのを待つ（ブラウザが保留中の get を持ったまま次の get を呼ばない）
//   - abort した run の結果は、後から届いても finish へ送らず捨てる
//   - finish へ進む直前に、await を挟まずに確認して finishing へ進む
// 複数タブ: 各タブが自分のトークンを持つので、別のタブの操作でこちらの finish が失敗することはない。

import { ApiError, UnauthorizedError } from '@/api/client'
import { beginPasskeyLogin, finishPasskeyLogin } from '@/api/endpoints'
import type { AuthResponse } from '@/api/types'
import {
  getPasskeyAssertion,
  isConditionalPasskeyLoginAvailable,
  PasskeyLoginAbortedError,
  PasskeyLoginCancelledError,
} from './passkey'

export const PASSKEY_LOGIN_FAILED = 'パスキーでログインできませんでした。もう一度お試しください。'
export const LOGIN_LOCKED =
  'ログインの失敗が続いたため、しばらく操作できません。時間をおいてからもう一度お試しください。'

/** conditional の待機を作り直す間隔（サーバーのチャレンジは5分で切れる） */
export const CONDITIONAL_REFRESH_MS = 4 * 60 * 1000
/** これより古いチャレンジの応答は、finish へ送らず捨てる */
export const CHALLENGE_MAX_AGE_MS = CONDITIONAL_REFRESH_MS + 50 * 1000
/** 1回のページ表示で自動に行う begin の上限（begin はIPごとに15分60回の枠がある） */
export const CONDITIONAL_MAX_BEGINS = 10
/** 自動の begin の最低間隔 */
export const CONDITIONAL_MIN_INTERVAL_MS = 5 * 1000
/** 前の run が片付くのを待つ上限。超えたら begin を送らず、idle へ戻す */
export const PREVIOUS_RUN_WAIT_MAX_MS = 4 * 1000

export type PasskeyLoginPhase = 'idle' | 'conditional' | 'explicit' | 'password' | 'finishing'

export interface PasskeyLoginHandlers {
  onPhase: (phase: PasskeyLoginPhase) => void
  onLoggedIn: (data: AuthResponse) => void
  /** 固定文言だけを渡す */
  onError: (message: string) => void
}

/** 利用者に見せる文言を決める。取り消しと中止は null（何も表示しない） */
export function classifyPasskeyLoginError(error: unknown): string | null {
  if (error instanceof PasskeyLoginCancelledError) return null
  // 利用者の有無や失敗の理由は区別せず、固定の文言にする
  if (error instanceof UnauthorizedError) return PASSKEY_LOGIN_FAILED
  if (error instanceof ApiError) {
    return error.problem.status === 429 ? LOGIN_LOCKED : error.message
  }
  // 端末側の想定外の失敗は、内部の文言を出さない
  return PASSKEY_LOGIN_FAILED
}

type RunKind = 'conditional' | 'explicit'

interface Run {
  readonly kind: RunKind
  readonly controller: AbortController
  /** 前の run が片付くまで待つための Promise */
  readonly prev: Promise<void>
  readonly done: () => void
  stage: 'begin' | 'get' | 'finish'
  begunAt: number
  refreshTimer: ReturnType<typeof setTimeout> | null
}

/** 前の run が上限までに片付かなかった（その run の結果は破棄扱い） */
class PreviousRunTimeoutError extends Error {}

// モジュール単位の直列化。画面を離れて戻っても、前の画面の begin が片付くまで次の begin を送らない
let tail: Promise<void> = Promise.resolve()

/** テスト用: 直列化の状態を初期に戻す */
export function resetPasskeyLoginForTest(): void {
  tail = Promise.resolve()
}

/** 前の run を待つ。上限を超えたら reject する（prev 自体は reject しない） */
function waitPrevious(prev: Promise<void>): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new PreviousRunTimeoutError()), PREVIOUS_RUN_WAIT_MAX_MS)
    void prev.then(() => {
      clearTimeout(timer)
      resolve()
    })
  })
}

function newRun(kind: RunKind): Run {
  const prev = tail
  let done: () => void = () => {}
  tail = new Promise<void>((resolve) => {
    done = resolve
  })
  return { kind, controller: new AbortController(), prev, done, stage: 'begin', begunAt: 0, refreshTimer: null }
}

export class PasskeyLoginController {
  handlers: PasskeyLoginHandlers = { onPhase: () => {}, onLoggedIn: () => {}, onError: () => {} }

  private phase: PasskeyLoginPhase = 'idle'
  private current: Run | null = null
  private active = false
  private available = false
  private epoch = 0
  private autoBegins = 0
  private cancelRetried = false
  private lastBeginAt = 0
  /** 非表示の間に更新・再開の時期が来たので、表示に戻ったら行う */
  private wantStart = false
  private startTimer: ReturnType<typeof setTimeout> | null = null
  private scheduleTimer: ReturnType<typeof setTimeout> | null = null

  get currentPhase(): PasskeyLoginPhase {
    return this.phase
  }

  start(): void {
    this.active = true
    document.addEventListener('visibilitychange', this.onVisibility)
    const epoch = this.epoch
    // StrictMode の二重マウントで begin が2回走らないよう、開始を1tick遅らせる（cleanup で取り消す）
    this.startTimer = setTimeout(() => {
      this.startTimer = null
      void isConditionalPasskeyLoginAvailable().then((ok) => {
        if (epoch !== this.epoch || !ok) return
        this.available = true
        if (document.visibilityState === 'hidden') {
          // 非表示のタブでは begin を送らない。表示に戻ったときに始める
          this.wantStart = true
          return
        }
        this.startConditional()
      })
    }, 0)
  }

  dispose(): void {
    this.epoch += 1
    this.active = false
    this.available = false
    this.wantStart = false
    document.removeEventListener('visibilitychange', this.onVisibility)
    this.clearTimers()
    // 送信中の finish は止められない（request が signal を受け取らない）。結果は捨てる
    this.dropCurrent()
    this.setPhase('idle')
  }

  /** 「パスキーでログイン」ボタン */
  startExplicit(): void {
    if (this.phase !== 'idle' && this.phase !== 'conditional') return
    this.clearTimers()
    this.dropCurrent()
    const run = newRun('explicit')
    this.current = run
    this.setPhase('explicit')
    void this.execute(run)
  }

  /** パスワード送信の前に呼ぶ。受け付けたら true。conditional は止める */
  startPassword(): boolean {
    if (this.phase !== 'idle' && this.phase !== 'conditional') return false
    this.clearTimers()
    this.dropCurrent()
    this.setPhase('password')
    return true
  }

  endPassword(outcome: 'failed' | 'locked' | 'error'): void {
    if (this.phase !== 'password') return
    this.setPhase('idle')
    if (outcome === 'failed') this.requestConditional()
  }

  private setPhase(next: PasskeyLoginPhase): void {
    if (this.phase === next) return
    this.phase = next
    this.handlers.onPhase(next)
  }

  private clearTimers(): void {
    if (this.startTimer) clearTimeout(this.startTimer)
    if (this.scheduleTimer) clearTimeout(this.scheduleTimer)
    this.startTimer = null
    this.scheduleTimer = null
  }

  private dropCurrent(): void {
    const run = this.current
    this.current = null
    if (!run) return
    if (run.refreshTimer) clearTimeout(run.refreshTimer)
    run.controller.abort()
  }

  private isCurrent(run: Run): boolean {
    return this.current === run && !run.controller.signal.aborted
  }

  private onVisibility = (): void => {
    if (document.visibilityState === 'hidden' || !this.wantStart) return
    this.wantStart = false
    this.requestConditional()
  }

  /** 自動の begin を依頼する。上限・非表示・最低間隔を守る */
  private requestConditional(): void {
    if (!this.active || !this.available || this.autoBegins >= CONDITIONAL_MAX_BEGINS) return
    if (this.phase !== 'idle' && this.phase !== 'conditional') return
    if (document.visibilityState === 'hidden') {
      this.wantStart = true
      return
    }
    if (this.scheduleTimer) clearTimeout(this.scheduleTimer)
    const wait = Math.max(0, this.lastBeginAt + CONDITIONAL_MIN_INTERVAL_MS - Date.now())
    this.scheduleTimer = setTimeout(() => {
      this.scheduleTimer = null
      this.startConditional()
    }, wait)
  }

  private startConditional(): void {
    if (!this.active || !this.available || this.autoBegins >= CONDITIONAL_MAX_BEGINS) return
    if (this.phase !== 'idle' && this.phase !== 'conditional') return
    this.dropCurrent()
    this.wantStart = false
    this.autoBegins += 1
    this.lastBeginAt = Date.now()
    const run = newRun('conditional')
    this.current = run
    this.setPhase('conditional')
    void this.execute(run)
  }

  private onRefreshDue(run: Run): void {
    if (this.current !== run) return
    run.refreshTimer = null
    if (this.autoBegins >= CONDITIONAL_MAX_BEGINS) {
      // 作り直せない。期限切れのチャレンジを待機に残さない
      this.dropCurrent()
      this.setPhase('idle')
      return
    }
    this.requestConditional()
  }

  private async execute(run: Run): Promise<void> {
    try {
      // 待ちきれないときは、前の run の get が保留中かもしれないので先へ進まない（finally の done で tail は進む）
      await waitPrevious(run.prev)
      if (!this.isCurrent(run)) return
      // challengeToken はこの run の finish にだけ使う（保存しない）
      const { publicKey, challengeToken } = await beginPasskeyLogin()
      if (!this.isCurrent(run)) return
      run.begunAt = Date.now()
      run.stage = 'get'
      if (run.kind === 'conditional') {
        run.refreshTimer = setTimeout(() => this.onRefreshDue(run), CONDITIONAL_REFRESH_MS)
      }
      const credential = await getPasskeyAssertion(
        publicKey,
        run.kind === 'conditional'
          ? { mediation: 'conditional', signal: run.controller.signal }
          : { signal: run.controller.signal },
      )
      await this.finish(run, challengeToken, credential)
    } catch (error) {
      this.fail(run, error)
    } finally {
      run.done()
    }
  }

  private async finish(run: Run, challengeToken: string, credential: Record<string, unknown>): Promise<void> {
    // ここから finishing へ進むまでは await を挟まない（ボタンやパスワードとの競合を同期で決める）。
    // abort 済み・別の run に置き換わった結果は、利用者に見せず捨てる
    if (!this.isCurrent(run) || this.phase !== run.kind) return
    if (Date.now() - run.begunAt >= CHALLENGE_MAX_AGE_MS) {
      // 期限切れのトークンを送ると401になり失敗に数えられる。送らず、利用者には固定文言で知らせる
      this.settle(run)
      this.handlers.onError(PASSKEY_LOGIN_FAILED)
      this.requestConditional()
      return
    }
    run.stage = 'finish'
    if (run.refreshTimer) clearTimeout(run.refreshTimer)
    this.setPhase('finishing')
    const data = await finishPasskeyLogin({ challengeToken, credential })
    if (this.current !== run) return
    this.handlers.onLoggedIn(data)
  }

  private settle(run: Run): void {
    if (run.refreshTimer) clearTimeout(run.refreshTimer)
    run.refreshTimer = null
    this.current = null
    this.setPhase('idle')
  }

  private fail(run: Run, error: unknown): void {
    if (this.current !== run) return
    this.settle(run)
    // conditional は、ページを開いただけで始まる。begin や端末側の失敗は表示せず、
    // 利用者が選んだ結果である finish の失敗だけを表示する
    const message = classifyPasskeyLoginError(error)
    if (message !== null && (run.kind === 'explicit' || run.stage === 'finish')) {
      this.handlers.onError(message)
    }
    if (error instanceof PasskeyLoginAbortedError) return
    if (error instanceof PasskeyLoginCancelledError) {
      if (run.kind === 'explicit') {
        this.requestConditional()
      } else if (!this.cancelRetried) {
        // 候補を選んだあとの取り消しなどで autofill が消えると不便なため、1回だけ作り直す
        this.cancelRetried = true
        this.requestConditional()
      }
      return
    }
    // 401は、失敗したのは選んだ1回だけ。次の選択のために待機を戻す。
    // 429・通信の失敗・500・想定外の失敗では、枠を消費しないよう自動では再開しない
    if (error instanceof UnauthorizedError) this.requestConditional()
  }
}
