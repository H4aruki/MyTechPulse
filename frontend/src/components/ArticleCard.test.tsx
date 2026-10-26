import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, rateLimitMessage, UnauthorizedError } from '@/api/client'
import { fetchSavedArticles, saveArticle, unsaveArticle } from '@/api/endpoints'
import type { Article } from '@/api/types'
import {
  SAVED_ARTICLE_IDS_QUERY_KEY,
  useSavedArticleIds,
  useToggleSavedArticle,
} from '@/lib/savedArticles'
import { ArticleCard } from './ArticleCard'

vi.mock('@/api/endpoints', () => ({
  fetchSavedArticles: vi.fn(),
  saveArticle: vi.fn(),
  unsaveArticle: vi.fn(),
}))

function makeArticle(overrides: Partial<Article> = {}): Article {
  return {
    id: 7,
    source: 'Qiita',
    title: 'Q記事',
    url: 'https://example.com/q',
    likes: 3,
    published_at: '2026-09-01T00:00:00Z',
    tags: ['Go'],
    ...overrides,
  }
}

// ページと同じく、保存済みIDの集合と切り替えのフックをカードへつなぐ
function Harness({ article }: { article: Article }) {
  const savedIds = useSavedArticleIds()
  const toggle = useToggleSavedArticle()
  return (
    <ul>
      <ArticleCard
        article={article}
        onOpen={() => undefined}
        saved={article.id !== undefined && savedIds.has(article.id)}
        onToggleSave={(id, save) => toggle.mutateAsync({ id, save })}
      />
    </ul>
  )
}

function renderCard(article: Article = makeArticle()) {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <Harness article={article} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

describe('ArticleCard の保存ボタン', () => {
  beforeEach(() => {
    vi.mocked(fetchSavedArticles)
      .mockReset()
      .mockResolvedValue({ items: [], total: 0, limit: 100, offset: 0 })
    vi.mocked(saveArticle).mockReset().mockResolvedValue(undefined)
    vi.mocked(unsaveArticle).mockReset().mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  test('押すと保存APIを呼び、保存済みの表示になる', async () => {
    renderCard()

    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))

    const pressed = await screen.findByRole('button', { name: '保存を外す' })
    expect(pressed).toHaveAttribute('aria-pressed', 'true')
    expect(saveArticle).toHaveBeenCalledWith(7)
  })

  test('もう一度押すと保存を外すAPIを呼び、元の表示に戻る', async () => {
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))
    const pressed = await screen.findByRole('button', { name: '保存を外す' })
    await waitFor(() => expect(pressed).toBeEnabled())

    fireEvent.click(pressed)

    const unpressed = await screen.findByRole('button', { name: '保存する' })
    expect(unpressed).toHaveAttribute('aria-pressed', 'false')
    expect(unsaveArticle).toHaveBeenCalledWith(7)
  })

  test('保存に失敗すると、表示を元に戻して失敗を知らせる', async () => {
    vi.mocked(saveArticle).mockRejectedValue(new ApiError({ status: 500 }))
    const client = renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '保存できませんでした。時間をおいて再度お試しください。',
    )
    expect(screen.getByRole('button', { name: '保存する' })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
    expect((client.getQueryData(SAVED_ARTICLE_IDS_QUERY_KEY) as Set<number>).has(7)).toBe(false)
  })

  test('保存を外すのに失敗したときは、外せなかった旨を出し、保存済みのままにする', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [
        {
          id: 7,
          title: 'Q記事',
          url: 'https://example.com/q',
          source: 'qiita',
          author_name: 'alice',
          likes: 3,
          published_at: '2026-09-01T00:00:00Z',
          saved_at: '2026-09-02T00:00:00Z',
        },
      ],
      total: 1,
      limit: 100,
      offset: 0,
    })
    vi.mocked(unsaveArticle).mockRejectedValue(new ApiError({ status: 500 }))
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存を外す' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '保存を外せませんでした。時間をおいて再度お試しください。',
    )
    expect(screen.getByRole('button', { name: '保存を外す' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  test('409なら保存できる件数の上限を知らせる', async () => {
    vi.mocked(saveArticle).mockRejectedValue(new ApiError({ status: 409 }))
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '保存は500件まで。いらない保存を外してください',
    )
  })

  test('429なら回数制限の案内を出す', async () => {
    vi.mocked(saveArticle).mockRejectedValue(
      new ApiError({ status: 429 }, rateLimitMessage(2), 2),
    )
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(rateLimitMessage(2))
  })

  test('401ならログインの案内とリンクを出す', async () => {
    vi.mocked(saveArticle).mockRejectedValue(new UnauthorizedError())
    renderCard()
    fireEvent.click(await screen.findByRole('button', { name: '保存する' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('保存するにはログインが必要です。')
    const link = screen.getByRole('link', { name: 'ログインする' })
    expect(link.getAttribute('href')).toMatch(/\/login$/)
  })

  test('idの無い記事には保存ボタンを出さない', async () => {
    renderCard(makeArticle({ id: undefined }))

    expect(await screen.findByRole('button', { name: /Q記事/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存する' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存を外す' })).not.toBeInTheDocument()
  })
})
