import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { UnauthorizedError } from '@/api/client'
import { fetchFeed, logout, recordArticleClick } from '@/api/endpoints'
import type { Article } from '@/api/types'
import { AppLayout } from '@/components/AppLayout'
import { ArticleCard } from '@/components/ArticleCard'
import { authQueryKey } from '@/lib/auth'

const FEED_QUERY_KEY = ['feed'] as const

interface ArticleSectionProps {
  title: string
  articles: Article[] | null | undefined
  onOpen: (article: Article) => void
}

function ArticleSection({ title, articles, onOpen }: ArticleSectionProps) {
  return (
    <section>
      <h2 className="mb-4 font-display text-xl font-bold text-ink">{title}</h2>
      {articles && articles.length > 0 ? (
        <ul className="grid gap-4 sm:grid-cols-2">
          {articles.map((article) => (
            <ArticleCard key={article.url} article={article} onOpen={onOpen} />
          ))}
        </ul>
      ) : (
        <p className="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-ink-muted">
          おすすめの記事はまだありません。
        </p>
      )}
    </section>
  )
}

export function ArticlesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: FEED_QUERY_KEY,
    queryFn: fetchFeed,
    // 記事は5日以内のものを外部APIから集約するため、再フォーカスのたびに叩かない
    refetchOnWindowFocus: false,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  const sessionExpired = error instanceof UnauthorizedError
  useEffect(() => {
    if (sessionExpired) navigate('/login', { replace: true })
  }, [sessionExpired, navigate])

  // クリック学習の送信。失敗しても閲覧体験を妨げないため、UIには出さない。
  // 二重に学習させないよう再送しない
  const clickMutation = useMutation({
    mutationFn: recordArticleClick,
    retry: false,
    onError: (clickError: Error) => {
      if (clickError instanceof UnauthorizedError) navigate('/login', { replace: true })
    },
  })

  const leaveSession = () => {
    // 利用者ごとの情報が次にログインする人へ見えないよう、ブラウザ内の取得結果を捨てる
    queryClient.removeQueries({ queryKey: authQueryKey })
    queryClient.removeQueries({ queryKey: FEED_QUERY_KEY })
    navigate('/login', { replace: true })
  }

  // サーバーのセッションを終わらせる。すでに401（期限切れ）なら終わっているのでログイン画面へ移る。
  // それ以外の失敗ではセッションが残っている可能性があるため、画面に残して再試行できるようにする
  const logoutMutation = useMutation({
    mutationFn: logout,
    retry: false,
    onSuccess: leaveSession,
    onError: (logoutError: Error) => {
      if (logoutError instanceof UnauthorizedError) leaveSession()
    },
  })

  const handleOpen = (article: Article) => {
    // ポップアップブロックを避けるため、クリック直後に同期的に開く
    window.open(article.url, '_blank', 'noopener,noreferrer')
    clickMutation.mutate(article.tags ?? [])
  }

  const handleLogout = () => {
    if (logoutMutation.isPending) return
    logoutMutation.mutate()
  }

  const logoutFailed = logoutMutation.isError && !(logoutMutation.error instanceof UnauthorizedError)

  return (
    <AppLayout authenticated onLogout={handleLogout}>
      <div className="mx-auto w-full max-w-6xl px-4 py-10">
        <h1 className="mb-8 text-2xl font-bold text-ink">あなたにおすすめの記事</h1>

        {logoutFailed && (
          <p role="alert" className="mb-6 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
            ログアウトに失敗しました。ログイン状態が残っている可能性があります。もう一度ログアウトをお試しください。
          </p>
        )}

        {isPending && <p className="py-16 text-center text-ink-muted">記事を読み込んでいます…</p>}

        {isError && !sessionExpired && (
          <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-8 text-center">
            <p className="mb-4 text-sm text-red-700">
              {error instanceof Error
                ? error.message
                : '記事の取得に失敗しました。時間をおいて再度お試しください。'}
            </p>
            <button
              type="button"
              onClick={() => refetch()}
              className="rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white transition hover:bg-brand-700"
            >
              再読み込み
            </button>
          </div>
        )}

        {data && data.warnings && data.warnings.length > 0 && (
          <div
            role="status"
            className="mb-8 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800"
          >
            <p className="font-bold">一部の記事を取得できませんでした。</p>
            <ul className="mt-1 list-inside list-disc">
              {data.warnings.map((warning) => (
                <li key={`${warning.provider}-${warning.code}`}>
                  {warning.provider} の記事が取得できていないため、表示が少なくなっています。
                </li>
              ))}
            </ul>
          </div>
        )}

        {data && (
          <div className="space-y-12">
            <ArticleSection
              title="Qiita Articles"
              articles={data.qiita_articles}
              onOpen={handleOpen}
            />
            <ArticleSection
              title="Zenn Articles"
              articles={data.zenn_articles}
              onOpen={handleOpen}
            />
          </div>
        )}
      </div>
    </AppLayout>
  )
}
