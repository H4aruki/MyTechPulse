import type { Article } from '@/api/types'
import { sourceBadgeClass, sourceLabel } from '@/lib/articleSource'
import { SaveButton } from './SaveButton'

interface ArticleCardProps {
  article: Article
  onOpen: (article: Article) => void
  /** 保存済みか。省略すると保存していない扱い */
  saved?: boolean
  /** 保存の切り替え。省略するか、記事にidが無いときは保存ボタンを出さない */
  onToggleSave?: (id: number, save: boolean) => Promise<unknown>
}

function formatPublishedAt(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('ja-JP', { dateStyle: 'medium' }).format(date)
}

function Likes({ count }: { count: number }) {
  return (
    <span className="flex items-center gap-1" aria-label={`${count} いいね`}>
      <svg aria-hidden="true" width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z" />
      </svg>
      {count.toLocaleString('ja-JP')}
    </span>
  )
}

/**
 * 旧実装は innerHTML + テンプレート文字列で組み立てていたため escapeHtml による
 * 手動のXSS対策が必要だったが、Reactが既定でエスケープするため不要になった。
 * 形はトップページの見本カード（タグ、提供元の色、いいね）に揃えている。
 */
export function ArticleCard({ article, onOpen, saved = false, onToggleSave }: ArticleCardProps) {
  // Go版APIはタグの無い記事を null で返すことがある
  const tags = article.tags ?? []
  const articleId = article.id
  const toggle =
    articleId !== undefined && onToggleSave
      ? (save: boolean) => onToggleSave(articleId, save)
      : undefined
  const canSave = toggle !== undefined

  return (
    <li className="relative">
      <button
        type="button"
        onClick={() => onOpen(article)}
        className={`card-glass ring-focus flex h-full w-full cursor-pointer flex-col gap-3 p-5 text-left transition duration-300 hover:-translate-y-1${canSave ? ' pr-14' : ''}`}
      >
        <div className="flex items-start justify-between gap-2">
          <ul className="flex min-w-0 flex-wrap gap-1.5">
            {tags.map((tag) => (
              <li key={tag} className="tag-pill text-accent-ink">
                {tag}
              </li>
            ))}
          </ul>
          <span
            className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${sourceBadgeClass(article.source)}`}
          >
            {sourceLabel(article.source)}
          </span>
        </div>

        <h3 className="text-base leading-snug font-semibold text-ink">{article.title}</h3>

        <div className="mt-auto flex items-center justify-between gap-3 text-xs text-ink-70">
          <span>{formatPublishedAt(article.published_at)}</span>
          <Likes count={article.likes} />
        </div>
      </button>
      {toggle && <SaveButton saved={saved} onToggle={toggle} />}
    </li>
  )
}
