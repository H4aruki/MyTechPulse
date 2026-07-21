import type { Article } from '@/api/types'

interface ArticleCardProps {
  article: Article
  onOpen: (article: Article) => void
}

function formatPublishedAt(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium' }).format(date)
}

/**
 * 旧実装は innerHTML + テンプレート文字列で組み立てていたため escapeHtml による
 * 手動のXSS対策が必要だったが、Reactが既定でエスケープするため不要になった。
 */
export function ArticleCard({ article, onOpen }: ArticleCardProps) {
  return (
    <li>
      <button
        type="button"
        onClick={() => onOpen(article)}
        className="flex h-full w-full cursor-pointer flex-col gap-3 rounded-xl border border-slate-200 bg-white p-5 text-left shadow-sm transition hover:-translate-y-0.5 hover:border-brand-500 hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
      >
        <h3 className="text-base leading-snug font-bold text-ink">{article.title}</h3>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-ink-muted">
          <span className="font-bold">{article.source}</span>
          <span aria-label={`${article.likes} いいね`}>♥ {article.likes}</span>
          <span>{formatPublishedAt(article.published_at)}</span>
        </div>

        {article.tags.length > 0 && (
          <ul className="mt-auto flex flex-wrap gap-1.5">
            {article.tags.map((tag) => (
              <li
                key={tag}
                className="rounded-full bg-brand-50 px-2.5 py-0.5 text-xs font-bold text-brand-700"
              >
                {tag}
              </li>
            ))}
          </ul>
        )}
      </button>
    </li>
  )
}
