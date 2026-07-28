import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { z } from 'zod'
import { signup } from '@/api/endpoints'
import { ApiStatus } from '@/api/types'
import { AppLayout } from '@/components/AppLayout'
import { TAG_CATEGORIES } from '@/constants/tags'
import { tokenStorage } from '@/lib/auth'

const accountSchema = z.object({
  newusername: z.string().min(1, 'ユーザー名を入力してください'),
  newpassword: z.string().min(1, 'パスワードを入力してください'),
})

type AccountFormValues = z.infer<typeof accountSchema>

/**
 * 旧実装と同じく1画面内の2ステップ構成。パスワードをブラウザストレージへ
 * 一時保存せず、アカウント情報とタグをまとめて1リクエストで登録する。
 */
export function SignupPage() {
  const navigate = useNavigate()
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
    onSuccess: (data) => {
      if (data.status === ApiStatus.SUCCESS && data.access_token) {
        tokenStorage.set(data.access_token)
        navigate('/articles', { replace: true })
        return
      }
      if (data.status === ApiStatus.FAILURE) {
        setFormError('このユーザー名は既に使用されています。別のユーザー名でお試しください。')
      } else {
        setFormError('登録中にエラーが発生しました。しばらくしてから再度お試しください。')
      }
      setStep(1)
    },
    onError: (error: Error) => {
      setFormError(error.message)
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
    const { newusername, newpassword } = getValues()
    mutation.mutate({
      newusername,
      newpassword,
      favoritetags: [...selectedTags],
    })
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
                <label htmlFor="newusername" className="mb-1.5 block text-sm font-bold text-ink">
                  ユーザー名
                </label>
                <input
                  id="newusername"
                  type="text"
                  autoComplete="username"
                  {...register('newusername')}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100"
                />
                {errors.newusername && (
                  <p className="mt-1 text-sm text-red-600">{errors.newusername.message}</p>
                )}
              </div>

              <div>
                <label htmlFor="newpassword" className="mb-1.5 block text-sm font-bold text-ink">
                  パスワード
                </label>
                <input
                  id="newpassword"
                  type="password"
                  autoComplete="new-password"
                  {...register('newpassword')}
                  className="w-full rounded-lg border border-slate-300 px-3 py-2 outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100"
                />
                {errors.newpassword && (
                  <p className="mt-1 text-sm text-red-600">{errors.newpassword.message}</p>
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
