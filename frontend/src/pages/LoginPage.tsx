import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { z } from 'zod'
import { UnauthorizedError } from '@/api/client'
import { login } from '@/api/endpoints'
import { AppLayout } from '@/components/AppLayout'
import { authQueryKey } from '@/lib/auth'

const loginSchema = z.object({
  username: z.string().min(1, 'ユーザー名を入力してください'),
  password: z.string().min(1, 'パスワードを入力してください'),
})

type LoginFormValues = z.infer<typeof loginSchema>

export function LoginPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [formError, setFormError] = useState('')

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginFormValues>({ resolver: zodResolver(loginSchema) })

  const mutation = useMutation({
    mutationFn: login,
    // 認証失敗を自動で繰り返すと、試行回数制限に余計にかかるため再送しない
    retry: false,
    onSuccess: (data) => {
      queryClient.setQueryData(authQueryKey, data.user)
      navigate('/articles', { replace: true })
    },
    onError: (error: Error) => {
      // ユーザー名列挙攻撃を防ぐため、利用者不存在とパスワード不一致は
      // 同じ401で返る。表示も1種類に統一する。
      if (error instanceof UnauthorizedError) {
        setFormError('ユーザー名またはパスワードが間違っています。')
        return
      }
      setFormError(error.message)
    },
  })

  const onSubmit = (values: LoginFormValues) => {
    setFormError('')
    mutation.mutate(values)
  }

  return (
    <AppLayout>
      <div className="mx-auto w-full max-w-md px-4 py-16">
        <h1 className="mb-8 text-center text-2xl font-bold text-ink">ログイン</h1>

        <form
          onSubmit={handleSubmit(onSubmit)}
          noValidate
          className="space-y-5 rounded-2xl border border-slate-200 bg-white p-8 shadow-sm"
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
              autoComplete="current-password"
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
            disabled={mutation.isPending}
            className="w-full rounded-lg bg-brand-500 px-4 py-2.5 font-bold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:bg-slate-400"
          >
            {mutation.isPending ? 'ログイン中…' : 'ログイン'}
          </button>

          <p className="text-center text-sm text-ink-muted">
            アカウントをお持ちでない方は{' '}
            <Link to="/signup" className="font-bold text-brand-500 hover:underline">
              新規登録
            </Link>
          </p>
        </form>
      </div>
    </AppLayout>
  )
}
