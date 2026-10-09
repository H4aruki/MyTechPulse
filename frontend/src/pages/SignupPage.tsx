import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { z } from 'zod'
import { ApiError } from '@/api/client'
import { signup } from '@/api/endpoints'
import { AppLayout } from '@/components/AppLayout'
import { TAG_CATEGORIES } from '@/constants/tags'
import { authQueryKey } from '@/lib/auth'
import { clearFeedbackState } from '@/lib/feedbackTracker'

// サーバー側の入力規則（利用者名は前後の空白を除いて1〜50文字、パスワードはUTF-8で1〜72バイト、
// タグは1〜128件で各1〜50文字）と同じ範囲を画面でも先に確認する。
const USERNAME_MAX_LENGTH = 50
const PASSWORD_MAX_BYTES = 72
const TAGS_MAX_COUNT = 128
const TAG_MAX_LENGTH = 50

const accountSchema = z.object({
  username: z
    .string()
    .refine((value) => value.trim().length >= 1, 'ユーザー名を入力してください')
    .refine(
      (value) => Array.from(value.trim()).length <= USERNAME_MAX_LENGTH,
      `ユーザー名は${USERNAME_MAX_LENGTH}文字以内で入力してください`,
    ),
  password: z
    .string()
    .min(1, 'パスワードを入力してください')
    .refine(
      (value) => new TextEncoder().encode(value).length <= PASSWORD_MAX_BYTES,
      `パスワードは${PASSWORD_MAX_BYTES}バイト以内で入力してください`,
    ),
})

function validateTags(tags: string[]): string | null {
  if (tags.length < 1) return 'タグを1つ以上選択してください。'
  if (tags.length > TAGS_MAX_COUNT) return `タグは${TAGS_MAX_COUNT}件までです。`
  const invalid = tags.some((tag) => {
    const length = Array.from(tag.trim()).length
    return length < 1 || length > TAG_MAX_LENGTH
  })
  return invalid ? `タグは1〜${TAG_MAX_LENGTH}文字で指定してください。` : null
}

type AccountFormValues = z.infer<typeof accountSchema>

/**
 * 旧実装と同じく1画面内の2ステップ構成。パスワードをブラウザストレージへ
 * 一時保存せず、アカウント情報とタグをまとめて1リクエストで登録する。
 */
export function SignupPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [step, setStep] = useState<1 | 2>(1)
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set())
  const [formError, setFormError] = useState('')

  const {
    register,
    handleSubmit,
    getValues,
    formState: { errors },
  } = useForm<AccountFormValues>({ resolver: zodResolver(accountSchema) })

  const mutation = useMutation({
    mutationFn: signup,
    // 登録は繰り返し送ると二重登録の疑いが出るため、失敗しても自動で再送しない
    retry: false,
    onSuccess: (data) => {
      // 同じブラウザで前に使っていた利用者のアンケート判定の記録を残さない
      clearFeedbackState()
      queryClient.setQueryData(authQueryKey, data.user)
      navigate('/articles', { replace: true })
    },
    onError: (error: Error) => {
      const status = error instanceof ApiError ? error.problem.status : undefined
      if (status === 409) {
        setFormError('このユーザー名は既に使用されています。別のユーザー名でお試しください。')
      } else if (status === 422) {
        setFormError(
          '入力内容に誤りがあります。ユーザー名・パスワード・タグを確認して、もう一度お試しください。',
        )
      } else {
        setFormError(error.message)
      }
      setStep(1)
    },
  })

  const toggleTag = (tag: string) => {
    setSelectedTags((prev) => {
      const next = new Set(prev)
      if (next.has(tag)) next.delete(tag)
      else next.add(tag)
      return next
    })
  }

  const toggleCategory = (tags: string[], checked: boolean) => {
    setSelectedTags((prev) => {
      const next = new Set(prev)
      for (const tag of tags) {
        if (checked) next.add(tag)
        else next.delete(tag)
      }
      return next
    })
  }

  const goToStep2 = () => {
    setFormError('')
    setStep(2)
  }

  const handleRegister = () => {
    const tags = [...selectedTags]
    const tagError = validateTags(tags)
    if (tagError) {
      setFormError(tagError)
      return
    }
    setFormError('')
    const { username, password } = getValues()
    mutation.mutate({ username: username.trim(), password, favorite_tags: tags })
  }

  return (
    <AppLayout>
      <div className="mx-auto w-full max-w-3xl px-4 py-16">
        {step === 1 ? (
          <>
            <h1 className="mb-8 text-center text-2xl font-bold text-ink">新規登録</h1>
            <form
              onSubmit={handleSubmit(goToStep2)}
              noValidate
              className="mx-auto max-w-md space-y-5 rounded-2xl border border-slate-200 bg-white p-8 shadow-sm"
            >
              <div>
                <label htmlFor="username" className="mb-1.5 block text-sm font-bold text-ink">
                  ユーザー名
                </label>
                <input
                  id="username"
                  type="text"
                  autoComplete="username"
                  {...register('username')}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100"
                />
                {errors.username && (
                  <p className="mt-1 text-sm text-red-600">{errors.username.message}</p>
                )}
              </div>

              <div>
                <label htmlFor="password" className="mb-1.5 block text-sm font-bold text-ink">
                  パスワード
                </label>
                <input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  {...register('password')}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100"
                />
                {errors.password && (
                  <p className="mt-1 text-sm text-red-600">{errors.password.message}</p>
                )}
              </div>

              {formError && (
                <p role="alert" className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
                  {formError}
                </p>
              )}

              <button
                type="submit"
                className="w-full rounded-lg bg-brand-500 px-4 py-2.5 font-bold text-white transition hover:bg-brand-700"
              >
                次へ：興味のあるタグを選ぶ
              </button>

              <p className="text-center text-sm text-ink-muted">
                すでにアカウントをお持ちの方は{' '}
                <Link to="/login" className="font-bold text-brand-500 hover:underline">
                  ログイン
                </Link>
              </p>
            </form>
          </>
        ) : (
          <>
            <h1 className="mb-2 text-center text-2xl font-bold text-ink">
              興味のあるタグを選択してください
            </h1>
            <p className="mb-8 text-center text-sm text-ink-muted">
              選んだタグをもとに、Qiita / Zenn からあなた向けの記事を集めます（1つ以上）
            </p>

            <div className="space-y-4">
              {TAG_CATEGORIES.map((category) => {
                const allChecked = category.tags.every((tag) => selectedTags.has(tag))
                return (
                  <fieldset
                    key={category.id}
                    className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"
                  >
                    <legend className="px-1">
                      <label className="flex cursor-pointer items-center gap-2 text-sm font-bold text-ink">
                        <input
                          type="checkbox"
                          checked={allChecked}
                          onChange={(e) => toggleCategory(category.tags, e.target.checked)}
                          className="size-4 accent-brand-500"
                        />
                        <span aria-hidden="true">{category.emoji}</span>
                        {category.label}
                      </label>
                    </legend>

                    <div className="mt-3 flex flex-wrap gap-2">
                      {category.tags.map((tag) => {
                        const checked = selectedTags.has(tag)
                        return (
                          <label
                            key={tag}
                            className={`cursor-pointer rounded-full border px-3 py-1.5 text-sm transition ${
                              checked
                                ? 'border-brand-500 bg-brand-500 font-bold text-white'
                                : 'border-slate-300 bg-white text-ink-muted hover:border-brand-500 hover:text-brand-700'
                            }`}
                          >
                            <input
                              type="checkbox"
                              name="tags"
                              value={tag}
                              checked={checked}
                              onChange={() => toggleTag(tag)}
                              className="sr-only"
                            />
                            {tag}
                          </label>
                        )
                      })}
                    </div>
                  </fieldset>
                )
              })}
            </div>

            {formError && (
              <p role="alert" className="mt-6 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
                {formError}
              </p>
            )}

            <div className="sticky bottom-0 mt-6 flex items-center gap-3 border-t border-slate-200 bg-surface/95 py-4 backdrop-blur">
              <button
                type="button"
                onClick={() => setStep(1)}
                className="rounded-lg border border-slate-300 px-4 py-2.5 font-bold text-ink-muted transition hover:bg-slate-100"
              >
                戻る
              </button>
              <button
                type="button"
                onClick={handleRegister}
                disabled={selectedTags.size === 0 || mutation.isPending}
                className="flex-1 rounded-lg bg-brand-500 px-4 py-2.5 font-bold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:bg-slate-400"
              >
                {mutation.isPending ? '登録中…' : `登録する（${selectedTags.size}件選択中）`}
              </button>
            </div>
          </>
        )}
      </div>
    </AppLayout>
  )
}
