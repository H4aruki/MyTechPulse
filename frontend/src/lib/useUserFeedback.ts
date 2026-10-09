import { useCallback, useEffect, useRef, useState } from 'react'
import { UnauthorizedError } from '@/api/client'
import { fetchFeedbackStatus, requestFeedbackPresentation } from '@/api/endpoints'
import { isShownPresentation, type ShownPresentation } from './feedbackPresentation'
import {
  canRequestPresentation,
  clearOpenedArticles,
  FEEDBACK_ARTICLE_THRESHOLD,
  openedArticleCount,
  recordOpenedArticle,
  setNextEligibleAt,
  settleWithin,
} from './feedbackTracker'

/**
 * 記事一覧でアンケートを出すかを判定する（設計書 第6章・第10章）。
 * 異なる記事を3件開き、数えた記事のクリック学習の通信が確定してから（最大10秒）、表示を要求する。
 */
export function useUserFeedback(onUnauthorized: () => void) {
  const [presentation, setPresentation] = useState<ShownPresentation | null>(null)
  const pendingClicks = useRef<Promise<unknown>[]>([])
  const requesting = useRef(false)
  const showing = useRef(false)
  const unauthorized = useRef(onUnauthorized)

  useEffect(() => {
    unauthorized.current = onUnauthorized
  }, [onUnauthorized])

  const maybeRequest = useCallback(async () => {
    if (requesting.current || showing.current) return
    if (openedArticleCount() < FEEDBACK_ARTICLE_THRESHOLD || !canRequestPresentation()) return
    requesting.current = true
    try {
      await settleWithin(pendingClicks.current)
      pendingClicks.current = []
      const promptId = crypto.randomUUID()
      let result
      try {
        result = await requestFeedbackPresentation(promptId)
      } catch (error) {
        // 結果が分からないので、IDを捨てて3件から数え直す
        clearOpenedArticles()
        if (error instanceof UnauthorizedError) unauthorized.current()
        return
      }
      if (!result.eligible) {
        setNextEligibleAt(false, result.next_eligible_at)
        return
      }
      if (isShownPresentation(result)) {
        clearOpenedArticles()
        showing.current = true
        setPresentation(result)
      }
      // すでに終わった表示の結果だけが返った場合は、履歴を残す
    } finally {
      requesting.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    fetchFeedbackStatus()
      .then((status) => {
        if (cancelled) return
        setNextEligibleAt(status.eligible, status.next_eligible_at)
        void maybeRequest()
      })
      .catch(() => {
        // 状態照会の失敗は記事閲覧を妨げない。表示要求の側でも同じ判定が行われる
      })
    return () => {
      cancelled = true
    }
  }, [maybeRequest])

  const articleOpened = useCallback(
    (url: string, click: Promise<unknown>) => {
      recordOpenedArticle(url)
      pendingClicks.current.push(click)
      void maybeRequest()
    },
    [maybeRequest],
  )

  const close = useCallback(() => {
    showing.current = false
    setPresentation(null)
  }, [])

  return { presentation, articleOpened, close }
}
