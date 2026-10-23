import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { fetchSavedArticles } from '@/api/endpoints'
import type { SavedArticleItem } from '@/api/types'
import { useSavedArticleIds } from './savedArticles'

vi.mock('@/api/endpoints', () => ({
  fetchSavedArticles: vi.fn(),
  saveArticle: vi.fn(),
  unsaveArticle: vi.fn(),
}))

function item(id: number): SavedArticleItem {
  return {
    id,
    title: `記事${id}`,
    url: `https://example.com/${id}`,
    source: 'qiita',
    author_name: 'alice',
    likes: 0,
    published_at: '2026-09-01T00:00:00Z',
    saved_at: '2026-09-02T00:00:00Z',
  }
}

describe('useSavedArticleIds', () => {
  beforeEach(() => {
    // モック関数を返さない。beforeEach の戻り値の関数は、後始末として引数なしで呼ばれる
    vi.mocked(fetchSavedArticles).mockReset()
  })
  afterEach(() => cleanup())

  test('複数ページをたどって、保存済みの記事IDを全件集める', async () => {
    const total = 150
    vi.mocked(fetchSavedArticles).mockImplementation(async ({ limit, offset }) => {
      const count = Math.max(0, Math.min(limit, total - offset))
      return {
        items: Array.from({ length: count }, (_, i) => item(offset + i + 1)),
        total,
        limit,
        offset,
      }
    })
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children)

    const { result } = renderHook(() => useSavedArticleIds(), { wrapper })

    await waitFor(() => expect(result.current.size).toBe(150))
    expect(result.current.has(1)).toBe(true)
    expect(result.current.has(150)).toBe(true)
    expect(fetchSavedArticles).toHaveBeenCalledTimes(2)
    expect(fetchSavedArticles).toHaveBeenNthCalledWith(1, { limit: 100, offset: 0 })
    expect(fetchSavedArticles).toHaveBeenNthCalledWith(2, { limit: 100, offset: 100 })
  })
})
