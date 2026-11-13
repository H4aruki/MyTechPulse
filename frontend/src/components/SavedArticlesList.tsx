import { useInfiniteQuery } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, rateLimitMessage, UnauthorizedError } from '@/api/client'
import { fetchSavedArticles } from '@/api/endpoints'
import type { SavedArticleItem } from '@/api/types'
import { sourceBadgeClass, sourceLabel } from '@/lib/articleSource'
import { SAVED_ARTICLES_QUERY_KEY, useToggleSavedArticle } from '@/lib/savedArticles'

const PAGE_LIMIT = 20
/** 保存できる上限。サーバー側の上限と合わせる */
const SAVE_LIMIT = 500

const dateFormat = new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium' })

function unsaveFailureMessage(error: unknown): ReactNode {
  if (error instanceof UnauthorizedError) {
    return (
      <>
        ログインが必要です。
        <Link to="/login" className="font-semibold text-accent-ink underline">
          ログインする
        </Link>
      </>
    )
  }
  if (error instanceof ApiError && error.problem.status === 429) {
    return rateLimitMessage(error.retryAfterSeconds)
  }
  return '保存を外せませんでした。時間をおいて再度お試しください。'
}

/** マイページの「保存した記事」。保存の新しい順に、20件ずつ読み足す */
export function SavedArticlesList() {
  const query = useInfiniteQuery({
    queryKey: [...SAVED_ARTICLES_QUERY_KEY, 'list'],
    queryFn: ({ pageParam }) => fetchSavedArticles({ limit: PAGE_LIMIT, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, _pages, lastPageParam) => {
      const loaded = lastPageParam + (lastPage.items?.length ?? 0)
      return loaded < lastPage.total && (lastPage.items?.length ?? 0) > 0 ? loaded : undefined
    },
    refetchOnWindowFocus: false,
    retry: false,
  })
  const toggle = useToggleSavedArticle()
  const [pendingIds, setPendingIds] = useState<ReadonlySet<number>>(new Set())
  const [failures, setFailures] = useState<Readonly<Record<number, unknown>>>({})

  const unsave = async (id: number) => {
    if (pendingIds.has(id)) return
    setPendingIds((prev) => new Set(prev).add(id))
    setFailures((prev) => {
      const next = { ...prev }
      delete next[id]
      return next
    })
    try {
      await toggle.mutateAsync({ id, save: false })
    } catch (error) {
      setFailures((prev) => ({ ...prev, [id]: error }))
    } finally {
      setPendingIds((prev) => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }
  }

  if (query.isPending) {
    return <p className="text-sm text-ink-70">読み込んでいます…</p>
  }
  if (query.isError) {
    return (
      <div role="alert" className="space-y-2 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
        <p>保存した記事を読み込めませんでした。</p>
        <button
          type="button"
          onClick={() => void query.refetch()}
          className="ring-focus min-h-11 cursor-pointer rounded-[10px] border border-red-300 px-4 font-semibold hover:bg-red-100"
        >
          再試行
        </button>
      </div>
    )
  }

  const items: SavedArticleItem[] = query.data.pages.flatMap((page) => page.items ?? [])
  const total = query.data.pages[query.data.pages.length - 1]?.total ?? 0

  if (items.length === 0) {
    return (
      <p className="rounded-2xl border border-dashed border-line bg-surface px-4 py-6 text-sm text-ink-70">
        まだ保存した記事はありません。記事一覧の栞のボタンで保存できます。
      </p>
    )
  }

  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-70">
        {total}件 / {SAVE_LIMIT}件
      </p>
      <ul className="space-y-3">
        {items.map((item) => (
          <li key={item.id} className="card-glass p-4">
            <div className="flex items-start gap-3">
              <div className="min-w-0 flex-1 space-y-2">
                <span
                  className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${sourceBadgeClass(item.source)}`}
                >
                  {sourceLabel(item.source)}
                </span>
                <a
                  href={item.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="ring-focus block rounded-sm font-semibold break-words text-accent-ink underline"
                >
                  {item.title}
                </a>
                <p className="text-xs text-ink-70">
                  {dateFormat.format(new Date(item.saved_at))}に保存
                </p>
              </div>
              <button
                type="button"
                aria-label={`保存を外す: ${item.title}`}
                disabled={pendingIds.has(item.id)}
                onClick={() => void unsave(item.id)}
                className="btn-ghost ring-focus min-h-11 shrink-0 px-3 py-2 text-sm disabled:cursor-not-allowed"
              >
                外す
              </button>
            </div>
            {item.id in failures && (
              <p role="alert" className="mt-2 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">
                {unsaveFailureMessage(failures[item.id])}
              </p>
            )}
          </li>
        ))}
      </ul>
      {query.hasNextPage && (
        <button
          type="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
          className="btn-ghost ring-focus min-h-11 px-4 py-2 text-sm disabled:cursor-not-allowed"
        >
          {query.isFetchingNextPage ? '読み込んでいます…' : 'もっと見る'}
        </button>
      )}
    </div>
  )
}
