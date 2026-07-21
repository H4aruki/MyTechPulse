import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { fetchPersonalNews, recordArticleClick } from '@/api/endpoints'
import type { Article } from '@/api/types'
import { AppLayout } from '@/components/AppLayout'
import { ArticleCard } from '@/components/ArticleCard'
import { tokenStorage } from '@/lib/auth'

interface ArticleSectionProps {
  title: string
  articles: Article[] | undefined
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

  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: ['personal-news'],
    queryFn: fetchPersonalNews,
    // 記事は5日以内のものを外部APIから集約するため、再フォーカスのたびに叩かない
    refetchOnWindowFocus: false,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  // クリック学習の送信。失敗しても閲覧体験を妨げないため、UIには出さない
  const clickMutation = useMutation({ mutationFn: recordArticleClick })

  const handleOpen = (article: Article) => {
    // ポップアップブロックを避けるため、クリック直後に同期的に開く
    window.open(article.url, '_blank', 'noopener,noreferrer')
    clickMutation.mutate(article.tags)
  }

  const handleLogout = () => {
    // サーバー側のトークン失効機構は無いため、クライアント側の破棄のみ
    tokenStorage.clear()
    navigate('/login', { replace: true })
  }

  return (
    <AppLayout authenticated onLogout={handleLogout}>
      <div className="mx-auto w-full max-w-6xl px-4 py-10">
        <h1 className="mb-8 text-2xl font-bold text-ink">あなたにおすすめの記事</h1>

        {isPending && <p className="py-16 text-center text-ink-muted">記事を読み込んでいます…</p>}

        {isError && (
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
