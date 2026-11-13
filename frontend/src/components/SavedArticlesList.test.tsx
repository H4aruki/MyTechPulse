import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { fetchSavedArticles, saveArticle, unsaveArticle } from '@/api/endpoints'
import type { SavedArticleItem } from '@/api/types'
import { SavedArticlesList } from './SavedArticlesList'

vi.mock('@/api/endpoints', () => ({
  fetchSavedArticles: vi.fn(),
  saveArticle: vi.fn(),
  unsaveArticle: vi.fn(),
}))

function item(id: number, source = 'qiita'): SavedArticleItem {
  return {
    id,
    title: `記事${id}`,
    url: `https://example.com/${id}`,
    source,
    author_name: 'alice',
    likes: 0,
    published_at: '2026-09-01T00:00:00Z',
    saved_at: '2026-09-02T00:00:00Z',
  }
}

function renderList() {
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <SavedArticlesList />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

describe('SavedArticlesList', () => {
  beforeEach(() => {
    vi.mocked(fetchSavedArticles)
      .mockReset()
      .mockResolvedValue({ items: [], total: 0, limit: 20, offset: 0 })
    vi.mocked(saveArticle).mockReset().mockResolvedValue(undefined)
    vi.mocked(unsaveArticle).mockReset().mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  test('保存の新しい順に、タイトルとリンクを出す', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(3), item(1), item(2)],
      total: 3,
      limit: 20,
      offset: 0,
    })
    renderList()

    const links = await screen.findAllByRole('link', { name: /^記事\d$/ })

    expect(links.map((link) => link.textContent)).toEqual(['記事3', '記事1', '記事2'])
    expect(links[0]).toHaveAttribute('href', 'https://example.com/3')
    expect(links[0]).toHaveAttribute('target', '_blank')
    expect(links[0]).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText('3件 / 500件')).toBeInTheDocument()
    expect(fetchSavedArticles).toHaveBeenCalledWith({ limit: 20, offset: 0 })
  })

  test('「もっと見る」で次のページを読み足し、最後のページではボタンが消える', async () => {
    const total = 25
    vi.mocked(fetchSavedArticles).mockImplementation(async ({ limit, offset }) => {
      const count = Math.max(0, Math.min(limit, total - offset))
      return {
        items: Array.from({ length: count }, (_, i) => item(offset + i + 1)),
        total,
        limit,
        offset,
      }
    })
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: 'もっと見る' }))

    expect(await screen.findByRole('link', { name: '記事25' })).toBeInTheDocument()
    expect(fetchSavedArticles).toHaveBeenNthCalledWith(2, { limit: 20, offset: 20 })
    expect(screen.getAllByRole('link', { name: /^記事\d+$/ })).toHaveLength(25)
    expect(screen.queryByRole('button', { name: 'もっと見る' })).not.toBeInTheDocument()
  })

  test('「保存を外す」でAPIを呼び、その項目が消える', async () => {
    let items = [item(1), item(2)]
    vi.mocked(fetchSavedArticles).mockImplementation(async () => ({
      items,
      total: items.length,
      limit: 20,
      offset: 0,
    }))
    vi.mocked(unsaveArticle).mockImplementation(async (id) => {
      items = items.filter((it) => it.id !== id)
    })
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: '保存を外す: 記事1' }))

    await waitFor(() => expect(screen.queryByRole('link', { name: '記事1' })).not.toBeInTheDocument())
    expect(unsaveArticle).toHaveBeenCalledWith(1)
    expect(screen.getByRole('link', { name: '記事2' })).toBeInTheDocument()
  })

  test('外すのに失敗すると、項目を残して失敗を知らせる', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: 1,
      limit: 20,
      offset: 0,
    })
    vi.mocked(unsaveArticle).mockRejectedValue(new ApiError({ status: 500 }))
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: '保存を外す: 記事1' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '保存を外せませんでした。時間をおいて再度お試しください。',
    )
    expect(screen.getByRole('link', { name: '記事1' })).toBeInTheDocument()
  })

  test('0件のときは、保存の仕方を案内する', async () => {
    renderList()

    expect(
      await screen.findByText('まだ保存した記事はありません。記事一覧の栞のボタンで保存できます。'),
    ).toBeInTheDocument()
  })

  test('QiitaとZennは決まった表記にし、それ以外の提供元はそのまま出す', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1, 'zenn'), item(2, 'Note')],
      total: 2,
      limit: 20,
      offset: 0,
    })
    renderList()

    expect(await screen.findByText(/Zenn ・/)).toBeInTheDocument()
    expect(screen.getByText(/Note ・/)).toBeInTheDocument()
    expect(screen.queryByText(/qiita/i)).not.toBeInTheDocument()
  })

  test('次のページを読んでいるあいだは、ボタンが「読み込んでいます…」になり、押せない', async () => {
    let release: () => void = () => {}
    vi.mocked(fetchSavedArticles).mockImplementation(async ({ offset }) => {
      if (offset > 0) {
        await new Promise<void>((resolve) => {
          release = resolve
        })
      }
      const count = offset === 0 ? 20 : 1
      return {
        items: Array.from({ length: count }, (_, i) => item(offset + i + 1)),
        total: 21,
        limit: 20,
        offset,
      }
    })
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: 'もっと見る' }))

    expect(await screen.findByRole('button', { name: '読み込んでいます…' })).toBeDisabled()
    await act(async () => {
      release()
    })

    expect(await screen.findByRole('link', { name: '記事21' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'もっと見る' })).not.toBeInTheDocument()
  })

  test('記事の配列が無く、総数だけ残っているときは、空の案内を出して次のページを読まない', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: null,
      total: 10,
      limit: 20,
      offset: 0,
    })
    renderList()

    expect(await screen.findByText(/まだ保存した記事はありません/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'もっと見る' })).not.toBeInTheDocument()
    expect(fetchSavedArticles).toHaveBeenCalledTimes(1)
  })

  test('総件数が無いときは0件と表示し、次のページへ進まない', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: undefined as unknown as number,
      limit: 20,
      offset: 0,
    })
    renderList()

    expect(await screen.findByText('0件 / 500件')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '記事1' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'もっと見る' })).not.toBeInTheDocument()
  })

  test('ログインが切れていると、ログインへの案内を出す', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: 1,
      limit: 20,
      offset: 0,
    })
    vi.mocked(unsaveArticle).mockRejectedValue(new UnauthorizedError())
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: '保存を外す: 記事1' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('ログインが必要です。')
    expect(screen.getByRole('link', { name: 'ログインする' })).toHaveAttribute('href', '/login')
    expect(screen.getByRole('link', { name: '記事1' })).toBeInTheDocument()
  })

  test('回数制限のときは、待ってからやり直す案内を出す', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: 1,
      limit: 20,
      offset: 0,
    })
    vi.mocked(unsaveArticle).mockRejectedValue(new ApiError({ status: 429 }, undefined, 30))
    renderList()
    fireEvent.click(await screen.findByRole('button', { name: '保存を外す: 記事1' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '操作が多すぎます。30秒ほど待ってからもう一度お試しください。',
    )
    expect(screen.getByRole('link', { name: '記事1' })).toBeInTheDocument()
  })

  test('外しているあいだはボタンを押せなくし、もう一度押しても通信を重ねない', async () => {
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: 1,
      limit: 20,
      offset: 0,
    })
    let release: () => void = () => {}
    vi.mocked(unsaveArticle).mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    renderList()
    const button = await screen.findByRole('button', { name: '保存を外す: 記事1' })
    fireEvent.click(button)

    await waitFor(() => expect(button).toBeDisabled())
    fireEvent.click(button)
    expect(unsaveArticle).toHaveBeenCalledTimes(1)

    await act(async () => {
      release()
    })
    await waitFor(() => expect(button).toBeEnabled())
  })

  test('読み込みに失敗すると知らせ、再試行で読み直せる', async () => {
    vi.mocked(fetchSavedArticles).mockRejectedValueOnce(new ApiError({ status: 500 }))
    vi.mocked(fetchSavedArticles).mockResolvedValue({
      items: [item(1)],
      total: 1,
      limit: 20,
      offset: 0,
    })
    renderList()

    expect(await screen.findByRole('alert')).toHaveTextContent('保存した記事を読み込めませんでした。')
    fireEvent.click(screen.getByRole('button', { name: '再試行' }))

    expect(await screen.findByRole('link', { name: '記事1' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
