import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link, useNavigate } from 'react-router-dom'
import { z } from 'zod'
import { ApiError, UnauthorizedError } from '@/api/client'
import { beginPasskeyLogin, finishPasskeyLogin, login } from '@/api/endpoints'
import type { AuthResponse } from '@/api/types'
import { AppLayout } from '@/components/AppLayout'
import { authQueryKey } from '@/lib/auth'
import { clearFeedbackState } from '@/lib/feedbackTracker'
import {
  getPasskeyAssertion,
  isPasskeyLoginSupported,
  PasskeyLoginCancelledError,
} from '@/lib/passkey'

const PASSKEY_LOGIN_FAILED = 'パスキーでログインできませんでした。もう一度お試しください。'

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

  // 対応していないブラウザでは、ボタンを出さない
  const passkeySupported = isPasskeyLoginSupported()

  // パスワードとパスキーで、ログイン成功後の処理をそろえる
  const onLoggedIn = (data: AuthResponse) => {
    // 同じブラウザで前に使っていた利用者のアンケート判定の記録を残さない
    clearFeedbackState()
    queryClient.setQueryData(authQueryKey, data.user)
    navigate('/articles', { replace: true })
  }

  const mutation = useMutation({
    mutationFn: login,
    // 認証失敗を自動で繰り返すと、試行回数制限に余計にかかるため再送しない
    retry: false,
    onSuccess: onLoggedIn,
    onError: (error: Error) => {
      // ユーザー名列挙攻撃を防ぐため、利用者不存在とパスワード不一致は
      // 同じ401で返る。表示も1種類に統一する。
      if (error instanceof UnauthorizedError) {
        setFormError('ユーザー名またはパスワードが間違っています。')
        return
      }
      if (error instanceof ApiError && error.problem.status === 429) {
        setFormError('ログインの失敗が続いたため、しばらく操作できません。時間をおいてからもう一度お試しください。')
        return
      }
      setFormError(error.message)
    },
  })

  const passkeyMutation = useMutation({
    mutationFn: async () => {
      const { publicKey } = await beginPasskeyLogin()
      const credential = await getPasskeyAssertion(publicKey)
      return finishPasskeyLogin({ credential })
    },
    retry: false,
    onSuccess: onLoggedIn,
    onError: (error: Error) => {
      // 利用者自身が取り消したので、エラーとして扱わず、パスワードの入力へ戻れるようにする
      if (error instanceof PasskeyLoginCancelledError) return
      // 利用者の有無や失敗の理由は区別せず、固定の文言にする
      if (error instanceof UnauthorizedError) {
        setFormError(PASSKEY_LOGIN_FAILED)
        return
      }
      if (error instanceof ApiError) {
        setFormError(
          error.problem.status === 429
            ? 'ログインの失敗が続いたため、しばらく操作できません。時間をおいてからもう一度お試しください。'
            : error.message,
        )
        return
      }
      // 端末側の想定外の失敗は、内部の文言を出さない
      setFormError(PASSKEY_LOGIN_FAILED)
    },
  })

  // 二重送信を防ぐため、どちらかの処理中は両方のボタンを止める
  const busy = mutation.isPending || passkeyMutation.isPending

  const onSubmit = (values: LoginFormValues) => {
    if (busy) return
    setFormError('')
    mutation.mutate(values)
  }

  const onPasskeyLogin = () => {
    if (busy) return
    setFormError('')
    passkeyMutation.mutate()
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
            disabled={busy}
            className="w-full rounded-lg bg-brand-500 px-4 py-2.5 font-bold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:bg-slate-400"
          >
            {mutation.isPending ? 'ログイン中…' : 'ログイン'}
          </button>

          {passkeySupported && (
            <button
              type="button"
              onClick={onPasskeyLogin}
              disabled={busy}
              className="w-full rounded-lg border border-brand-500 bg-white px-4 py-2.5 font-bold text-brand-500 transition hover:bg-brand-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400"
            >
              {passkeyMutation.isPending ? '確認中…' : 'パスキーでログイン'}
            </button>
          )}

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
