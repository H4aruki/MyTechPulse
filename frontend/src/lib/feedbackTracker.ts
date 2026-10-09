// アンケートを出す条件（異なる記事を3件開いた）と、次に表示を要求してよい日時を、
// 現在のログイン中だけ持つ。設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md 第6章

const OPENED_KEY = 'mtp.feedback.openedArticles'

export const FEEDBACK_ARTICLE_THRESHOLD = 3
export const CLICK_SETTLE_TIMEOUT_MS = 10_000

// sessionStorage が使えない環境（プライベートモード等）でも数え続けるための控え
let memoryOpened: string[] = []
// この時刻（ミリ秒）より前は表示を要求しない。null は制限なし、Infinity はこのログイン中は要求しない
let blockedUntil: number | null = null

function readOpened(): string[] {
  try {
    const raw = window.sessionStorage.getItem(OPENED_KEY)
    if (raw === null) return memoryOpened
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []
  } catch {
    return memoryOpened
  }
}

function writeOpened(urls: string[]) {
  memoryOpened = urls
  try {
    window.sessionStorage.setItem(OPENED_KEY, JSON.stringify(urls))
  } catch {
    // 保存できなくても記事の閲覧を優先する
  }
}

/** 開いた記事を記録し、異なる記事の数を返す */
export function recordOpenedArticle(url: string): number {
  const urls = readOpened()
  if (urls.includes(url)) return urls.length
  const next = [...urls, url]
  writeOpened(next)
  return next.length
}

export function openedArticleCount(): number {
  return readOpened().length
}

export function clearOpenedArticles() {
  memoryOpened = []
  try {
    window.sessionStorage.removeItem(OPENED_KEY)
  } catch {
    // 消せなくても次のログインで上書きされる
  }
}

/** 状態照会・表示要求の結果から、次に要求してよい日時を覚える */
export function setNextEligibleAt(eligible: boolean, nextEligibleAt?: string | null) {
  if (eligible) {
    blockedUntil = null
    return
  }
  const at = nextEligibleAt ? Date.parse(nextEligibleAt) : Number.NaN
  blockedUntil = Number.isNaN(at) ? Number.POSITIVE_INFINITY : at
}

export function canRequestPresentation(now: number = Date.now()): boolean {
  return blockedUntil === null || now >= blockedUntil
}

/** ログイン成功・ログアウト・認証切れで呼び、前の利用者の状態を残さない */
export function clearFeedbackState() {
  clearOpenedArticles()
  blockedUntil = null
}

/** すべての通信が成功か失敗で終わるか、ms が過ぎるまで待つ */
export function settleWithin(
  promises: Promise<unknown>[],
  ms: number = CLICK_SETTLE_TIMEOUT_MS,
): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    void Promise.allSettled(promises).then(() => {
      clearTimeout(timer)
      resolve()
    })
  })
}
