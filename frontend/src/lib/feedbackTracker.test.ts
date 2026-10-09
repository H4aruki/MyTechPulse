import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import {
  canRequestPresentation,
  clearFeedbackState,
  clearOpenedArticles,
  openedArticleCount,
  recordOpenedArticle,
  setNextEligibleAt,
  settleWithin,
} from './feedbackTracker'

describe('feedbackTracker', () => {
  beforeEach(() => {
    window.sessionStorage.clear()
    clearFeedbackState()
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  test('同じ記事は1件として数え、異なる記事だけを数える', () => {
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://b')).toBe(2)
    expect(openedArticleCount()).toBe(2)
  })

  test('開いた記事は利用者IDを含まないキーでsessionStorageに残る', () => {
    recordOpenedArticle('https://a')
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBe('["https://a"]')
  })

  test('ログイン・ログアウト時の消去で、前の利用者の状態を引き継がない', () => {
    recordOpenedArticle('https://a')
    setNextEligibleAt(false, '2099-01-01T00:00:00Z')
    clearFeedbackState()
    expect(openedArticleCount()).toBe(0)
    expect(canRequestPresentation()).toBe(true)
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  // 保存領域が使えなくても閲覧を壊さず、メモリで数え続ける
  test('sessionStorageが例外を出してもメモリで数える', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(recordOpenedArticle('https://a')).toBe(1)
    expect(recordOpenedArticle('https://b')).toBe(2)
    expect(() => clearOpenedArticles()).not.toThrow()
  })

  test('次回表示可能日時より前は要求せず、過ぎたら要求できる', () => {
    const next = Date.parse('2026-12-01T00:00:00Z')
    setNextEligibleAt(false, '2026-12-01T00:00:00Z')
    expect(canRequestPresentation(next - 1)).toBe(false)
    expect(canRequestPresentation(next)).toBe(true)
    setNextEligibleAt(false, null) // 有効なフォームが無い: このログイン中は要求しない
    expect(canRequestPresentation(next * 2)).toBe(false)
    setNextEligibleAt(true)
    expect(canRequestPresentation()).toBe(true)
  })

  // クリック学習の通信が返らなくても10秒で先へ進む
  test('settleWithinは全部終わるか、上限時間で返る', async () => {
    vi.useFakeTimers()
    let done = false
    const never = new Promise(() => {})
    const p = settleWithin([never, Promise.reject(new Error('x'))], 10_000).then(() => {
      done = true
    })
    await vi.advanceTimersByTimeAsync(9_999)
    expect(done).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    await p
    expect(done).toBe(true)
  })

  test('settleWithinは失敗した通信も確定として扱う', async () => {
    await expect(
      settleWithin([Promise.reject(new Error('x')), Promise.resolve()]),
    ).resolves.toBeUndefined()
  })
})
