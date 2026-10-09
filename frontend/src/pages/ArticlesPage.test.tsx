import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import {
  fetchFeed,
  fetchFeedbackStatus,
  logout,
  recordArticleClick,
  requestFeedbackPresentation,
} from '@/api/endpoints'
import type { Article, FeedResponse } from '@/api/types'
import { authQueryKey } from '@/lib/auth'
import { clearFeedbackState } from '@/lib/feedbackTracker'
import { ArticlesPage } from './ArticlesPage'

vi.mock('@/api/endpoints', () => ({
  fetchFeed: vi.fn(),
  recordArticleClick: vi.fn(),
  logout: vi.fn(),
  fetchFeedbackStatus: vi.fn(),
  requestFeedbackPresentation: vi.fn(),
  submitFeedbackOverall: vi.fn(),
  completeFeedbackFollowup: vi.fn(),
  dismissFeedback: vi.fn(),
}))

function article(source: 'Qiita' | 'Zenn', title: string, tags: string[] | null): Article {
  return {
    source,
    title,
    url: `https://example.com/${title}`,
    likes: 3,
    published_at: '2026-09-01T00:00:00Z',
    tags,
  }
}

function feed(overrides: Partial<FeedResponse> = {}): FeedResponse {
  return { qiita_articles: [], zenn_articles: [], warnings: null, ...overrides }
}

function renderArticles() {
  const client = new QueryClient()
  client.setQueryData(authQueryKey, { id: 1, username: 'alice', role: 'member' })
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/articles']}>
        <Routes>
          <Route path="/articles" element={<ArticlesPage />} />
          <Route path="/login" element={<h1>ログイン</h1>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return client
}

describe('ArticlesPage', () => {
  beforeEach(() => {
    vi.mocked(fetchFeed).mockReset()
    vi.mocked(recordArticleClick).mockReset()
    vi.mocked(logout).mockReset()
    vi.spyOn(window, 'open').mockImplementation(() => null)
    clearFeedbackState()
    window.sessionStorage.clear()
    vi.mocked(fetchFeedbackStatus).mockReset().mockResolvedValue({ eligible: true })
    vi.mocked(requestFeedbackPresentation)
      .mockReset()
      .mockResolvedValue({ eligible: false, next_eligible_at: '2099-01-01T00:00:00Z' })
    vi.mocked(recordArticleClick).mockResolvedValue(undefined)
  })
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  test('QiitaとZennの記事を既存の見出しの下へ表示する', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({
        qiita_articles: [article('Qiita', 'Q記事', ['Go'])],
        zenn_articles: [article('Zenn', 'Z記事', ['React'])],
      }),
    )
    renderArticles()

    expect(await screen.findByText('Q記事')).toBeInTheDocument()
    expect(screen.getByText('Z記事')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Qiita Articles' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Zenn Articles' })).toBeInTheDocument()
  })

  test('記事一覧がnullでも空として扱い、案内文を出す', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(feed({ qiita_articles: null, zenn_articles: null }))
    renderArticles()

    expect(await screen.findAllByText('おすすめの記事はまだありません。')).toHaveLength(2)
  })

  test('タグがnullの記事も表示できる', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({ qiita_articles: [article('Qiita', 'タグなし記事', null)] }),
    )
    renderArticles()

    expect(await screen.findByText('タグなし記事')).toBeInTheDocument()
  })

  test('一部の提供元の失敗は注意として出し、取得できた記事は表示する', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({
        qiita_articles: [article('Qiita', 'Q記事', ['Go'])],
        warnings: [{ provider: 'zenn', code: 'provider_unavailable' }],
      }),
    )
    renderArticles()

    expect(await screen.findByText('Q記事')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('zenn')
  })

  test('記事を押すと別タブで開き、タグをクリック学習として送る', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({ qiita_articles: [article('Qiita', 'Q記事', ['Go', 'React'])] }),
    )
    vi.mocked(recordArticleClick).mockResolvedValue(undefined)
    renderArticles()

    fireEvent.click(await screen.findByRole('button', { name: /Q記事/ }))

    expect(window.open).toHaveBeenCalledWith(
      'https://example.com/Q記事',
      '_blank',
      'noopener,noreferrer',
    )
    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(1))
    expect(vi.mocked(recordArticleClick).mock.calls[0][0]).toEqual(['Go', 'React'])
  })

  test('タグがnullの記事は空のタグ配列で学習を送る', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({ zenn_articles: [article('Zenn', 'Z記事', null)] }),
    )
    vi.mocked(recordArticleClick).mockResolvedValue(undefined)
    renderArticles()

    fireEvent.click(await screen.findByRole('button', { name: /Z記事/ }))

    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(1))
    expect(vi.mocked(recordArticleClick).mock.calls[0][0]).toEqual([])
  })

  test('学習の送信に失敗しても記事は開き、再送しない', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(
      feed({ qiita_articles: [article('Qiita', 'Q記事', ['Go'])] }),
    )
    vi.mocked(recordArticleClick).mockRejectedValue(new ApiError({ status: 503 }))
    renderArticles()

    fireEvent.click(await screen.findByRole('button', { name: /Q記事/ }))

    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(1))
    expect(window.open).toHaveBeenCalledTimes(1)
  })

  test('記事取得が401ならログイン画面へ移動する', async () => {
    vi.mocked(fetchFeed).mockRejectedValue(new UnauthorizedError())
    renderArticles()

    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
  })

  test('記事取得のその他の失敗は再読み込みを出す', async () => {
    vi.mocked(fetchFeed).mockRejectedValueOnce(
      new ApiError({ status: 503 }, '一時的に使えません。'),
    )
    renderArticles()

    expect(await screen.findByText('一時的に使えません。')).toBeInTheDocument()

    vi.mocked(fetchFeed).mockResolvedValue(
      feed({ qiita_articles: [article('Qiita', 'Q記事', ['Go'])] }),
    )
    fireEvent.click(screen.getByRole('button', { name: '再読み込み' }))

    expect(await screen.findByText('Q記事')).toBeInTheDocument()
  })

  test('ログアウトに成功するとauthとfeedを消してログイン画面へ移動する', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(feed())
    vi.mocked(logout).mockResolvedValue(undefined)
    const client = renderArticles()
    await screen.findByRole('heading', { name: 'Qiita Articles' })

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(logout).toHaveBeenCalledTimes(1)
    expect(client.getQueryData(authQueryKey)).toBeUndefined()
    expect(client.getQueryData(['feed'])).toBeUndefined()
  })

  test('ログアウトが401でもauthとfeedを消してログイン画面へ移動する', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(feed())
    vi.mocked(logout).mockRejectedValue(new UnauthorizedError())
    const client = renderArticles()
    await screen.findByRole('heading', { name: 'Qiita Articles' })

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toBeUndefined()
    expect(client.getQueryData(['feed'])).toBeUndefined()
  })

  test('ログアウトのその他の失敗は画面に残り、ログイン状態が残る可能性を伝える', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(feed())
    vi.mocked(logout).mockRejectedValue(new ApiError({ status: 503 }))
    const client = renderArticles()
    await screen.findByRole('heading', { name: 'Qiita Articles' })

    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('ログイン状態が残っている可能性')
    expect(screen.queryByRole('heading', { name: 'ログイン' })).not.toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toBeDefined()
    expect(logout).toHaveBeenCalledTimes(1)

    // 再試行できる
    vi.mocked(logout).mockResolvedValue(undefined)
    fireEvent.click(screen.getByRole('button', { name: 'ログアウト' }))
    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
  })

  function threeArticlesFeed() {
    return feed({
      qiita_articles: [article('Qiita', 'a', ['go']), article('Qiita', 'b', ['go'])],
      zenn_articles: [article('Zenn', 'c', ['go'])],
    })
  }

  test('異なる記事を3件開くまでは表示を要求せず、同じ記事は1件として数える', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('b'))
    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(3))
    expect(requestFeedbackPresentation).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('c'))
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    expect(vi.mocked(requestFeedbackPresentation).mock.calls[0][0]).toMatch(/^[0-9a-f-]{36}$/)
  })

  test('表示が許可されたらポップアップを出し、開封履歴を消す', async () => {
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    vi.mocked(requestFeedbackPresentation).mockImplementation(async (promptId: string) => ({
      eligible: true,
      status: 'shown',
      stage: 'overall',
      prompt_id: promptId,
      form: {
        title: 'おすすめ記事についてのアンケート',
        version: 1,
        questions: [
          { id: 10, key: 'overall', text: '今日のおすすめ記事は役に立ちましたか？', sort_order: 1, required: true },
        ],
      },
    }))
    renderArticles()
    await screen.findByText('a')
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    expect(
      await screen.findByRole('dialog', { name: 'おすすめ記事についてのアンケート' }),
    ).toBeInTheDocument()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  test('次回表示可能日時より前は、記事を開いても再要求しない', async () => {
    vi.mocked(fetchFeedbackStatus).mockResolvedValue({
      eligible: false,
      next_eligible_at: '2099-01-01T00:00:00Z',
    })
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    await waitFor(() => expect(fetchFeedbackStatus).toHaveBeenCalled())
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    await waitFor(() => expect(recordArticleClick).toHaveBeenCalledTimes(3))
    expect(requestFeedbackPresentation).not.toHaveBeenCalled()
  })

  test('状態照会に失敗しても記事は読める', async () => {
    vi.mocked(fetchFeedbackStatus).mockRejectedValue(new ApiError({ status: 500 }))
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    expect(await screen.findByText('a')).toBeInTheDocument()
  })

  test('認証切れでログイン画面へ戻すときは、前の利用者の取得結果と判定記録を捨てる', async () => {
    vi.mocked(fetchFeed).mockRejectedValue(new UnauthorizedError())
    window.sessionStorage.setItem('mtp.feedback.openedArticles', '["https://a"]')
    const client = renderArticles()
    expect(await screen.findByRole('heading', { name: 'ログイン' })).toBeInTheDocument()
    expect(client.getQueryData(authQueryKey)).toBeUndefined()
    expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull()
  })

  test('表示要求の結果が分からないときは開封履歴を消し、3件から数え直す', async () => {
    vi.mocked(requestFeedbackPresentation).mockRejectedValue(new ApiError({ status: 0 }))
    vi.mocked(fetchFeed).mockResolvedValue(threeArticlesFeed())
    renderArticles()
    await screen.findByText('a')
    for (const title of ['a', 'b', 'c']) fireEvent.click(screen.getByText(title))
    await waitFor(() => expect(requestFeedbackPresentation).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(window.sessionStorage.getItem('mtp.feedback.openedArticles')).toBeNull(),
    )
  })
})
