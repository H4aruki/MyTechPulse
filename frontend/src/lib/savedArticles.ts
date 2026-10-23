import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchSavedArticles, saveArticle, unsaveArticle } from '@/api/endpoints'

/** 保存済みの記事IDの集合。カードの「保存」表示に使う */
export const SAVED_ARTICLE_IDS_QUERY_KEY = ['saved-article-ids'] as const

/** 保存した記事の一覧（マイページ）。接頭辞が同じ query をまとめて無効にできる */
export const SAVED_ARTICLES_QUERY_KEY = ['saved-articles'] as const

const PAGE_LIMIT = 100
const EMPTY_IDS: ReadonlySet<number> = new Set()

async function fetchAllSavedArticleIds(): Promise<Set<number>> {
  const ids = new Set<number>()
  for (let offset = 0; ; offset += PAGE_LIMIT) {
    const page = await fetchSavedArticles({ limit: PAGE_LIMIT, offset })
    const items = page.items ?? []
    for (const item of items) ids.add(item.id)
    if (items.length === 0 || offset + PAGE_LIMIT >= page.total) break
  }
  return ids
}

/** 保存済みの記事IDを全件集める。取得できないあいだは、保存していないものとして空の集合を返す */
export function useSavedArticleIds(): ReadonlySet<number> {
  const { data } = useQuery({
    queryKey: SAVED_ARTICLE_IDS_QUERY_KEY,
    queryFn: fetchAllSavedArticleIds,
    refetchOnWindowFocus: false,
    retry: false,
  })
  return data ?? EMPTY_IDS
}

interface ToggleVariables {
  id: number
  save: boolean
}

/** 保存と解除の切り替え。表示を先に切り替え、失敗したら元に戻す */
export function useToggleSavedArticle() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, save }: ToggleVariables) => (save ? saveArticle(id) : unsaveArticle(id)),
    retry: false,
    onMutate: async ({ id, save }) => {
      await queryClient.cancelQueries({ queryKey: SAVED_ARTICLE_IDS_QUERY_KEY })
      const previous = queryClient.getQueryData<Set<number>>(SAVED_ARTICLE_IDS_QUERY_KEY)
      queryClient.setQueryData<Set<number>>(SAVED_ARTICLE_IDS_QUERY_KEY, (old) => {
        const next = new Set(old)
        if (save) next.add(id)
        else next.delete(id)
        return next
      })
      return { previous }
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(SAVED_ARTICLE_IDS_QUERY_KEY, context.previous)
      } else {
        void queryClient.invalidateQueries({ queryKey: SAVED_ARTICLE_IDS_QUERY_KEY })
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: SAVED_ARTICLES_QUERY_KEY }),
  })
}
