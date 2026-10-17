import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { UnauthorizedError } from '@/api/client'
import {
  beginPasskeyRegistration,
  deletePasskey,
  fetchPasskeys,
  finishPasskeyRegistration,
  renamePasskey,
} from '@/api/endpoints'
import type { PasskeyItem } from '@/api/types'
import {
  createPasskeyCredential,
  isPasskeySupported,
  PasskeyCancelledError,
  passkeysQueryKey,
} from '@/lib/passkey'

const MAX_PASSKEYS = 10

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('ja-JP', { dateStyle: 'medium', timeStyle: 'short' })
}

interface Props {
  /** 401（セッション切れ）のときに呼ぶ。ログイン画面へ戻す */
  onUnauthorized: () => void
}

/**
 * 設定画面のパスキーの項目。非対応のブラウザでは何も出さない。
 * ログイン直後の誘導はしない（この画面を開いた人にだけ見せる）。
 */
export function PasskeySettings({ onUnauthorized }: Props) {
  if (!isPasskeySupported()) return null
  return <PasskeySection onUnauthorized={onUnauthorized} />
}

function PasskeySection({ onUnauthorized }: Props) {
  const queryClient = useQueryClient()
  const [newName, setNewName] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const list = useQuery({ queryKey: passkeysQueryKey, queryFn: fetchPasskeys, retry: false })
  const sessionExpired = list.error instanceof UnauthorizedError
  useEffect(() => {
    if (sessionExpired) onUnauthorized()
  }, [sessionExpired, onUnauthorized])
  const refresh = () => queryClient.invalidateQueries({ queryKey: passkeysQueryKey })

  const fail = (e: unknown) => {
    if (e instanceof UnauthorizedError) {
      onUnauthorized()
      return
    }
    setNotice(null)
    setError(e instanceof Error ? e.message : '失敗しました。時間をおいてもう一度お試しください。')
  }

  const addMutation = useMutation({
    retry: false,
    mutationFn: async (name: string) => {
      // 開始で受け取った challengeToken は、この手続きの完了にだけ使う（保存しない）
      const { publicKey, challengeToken } = await beginPasskeyRegistration()
      const credential = await createPasskeyCredential(publicKey)
      return finishPasskeyRegistration({ challengeToken, credential, name: name.trim() || undefined })
    },
    onSuccess: () => {
      setNewName('')
      setError(null)
      setNotice('パスキーを追加しました。')
      void refresh()
    },
    onError: (e: Error) => {
      if (e instanceof PasskeyCancelledError) {
        setNotice(null)
        setError(e.message)
        return
      }
      fail(e)
    },
  })

  const renameMutation = useMutation({
    retry: false,
    mutationFn: ({ id, name }: { id: number; name: string }) => renamePasskey(id, name),
    onSuccess: () => {
      setEditingId(null)
      setError(null)
      setNotice('名前を変更しました。')
      void refresh()
    },
    onError: fail,
  })

  const deleteMutation = useMutation({
    retry: false,
    mutationFn: (id: number) => deletePasskey(id),
    onSuccess: () => {
      setError(null)
      setNotice('パスキーを削除しました。')
      void refresh()
    },
    onError: (e: Error) => {
      void refresh()
      fail(e)
    },
  })

  const passkeys: PasskeyItem[] = list.data?.passkeys ?? []
  const full = passkeys.length >= MAX_PASSKEYS
  const busy = addMutation.isPending

  const onAdd = (e: FormEvent) => {
    e.preventDefault()
    if (busy || full) return
    setNotice(null)
    setError(null)
    addMutation.mutate(newName)
  }

  const onRename = (e: FormEvent, id: number) => {
    e.preventDefault()
    const name = editName.trim()
    if (!name) {
      setError('名前を入力してください。')
      return
    }
    renameMutation.mutate({ id, name })
  }

  const onDelete = (item: PasskeyItem) => {
    if (!window.confirm(`「${item.name}」を削除しますか？ 削除すると、このパスキーではログインできなくなります。`)) {
      return
    }
    deleteMutation.mutate(item.id)
  }

  return (
    <section aria-labelledby="passkey-heading" className="rounded-xl border border-slate-200 bg-white p-6">
      <h2 id="passkey-heading" className="mb-2 font-display text-xl font-bold text-ink">
        パスキー
      </h2>
      <p className="mb-4 text-sm text-ink-muted">
        指紋・顔・PINなど、端末の本人確認で使える認証方法です。最大{MAX_PASSKEYS}件まで登録できます。パスワードでのログインもこれまでどおり使えます。
      </p>

      {error && (
        <p role="alert" className="mb-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="mb-4 rounded-lg bg-green-50 px-3 py-2 text-sm text-green-700">
          {notice}
        </p>
      )}

      <form onSubmit={onAdd} className="mb-6 flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="passkey-name" className="text-sm font-bold text-ink">
            名前（任意）
          </label>
          <input
            id="passkey-name"
            value={newName}
            maxLength={64}
            placeholder="例: iPhone"
            onChange={(e) => setNewName(e.target.value)}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm"
          />
        </div>
        <button
          type="submit"
          disabled={busy || full}
          className="rounded-lg bg-brand-500 px-4 py-2 text-sm font-bold text-white transition hover:bg-brand-700 disabled:opacity-50"
        >
          {busy ? '追加しています…' : 'パスキーを追加'}
        </button>
        {full && <p className="text-sm text-ink-muted">上限に達しています。不要なものを削除してください。</p>}
      </form>

      {list.isPending && <p className="text-sm text-ink-muted">読み込んでいます…</p>}
      {list.isError && !(list.error instanceof UnauthorizedError) && (
        <p role="alert" className="text-sm text-red-700">
          パスキーの一覧を取得できませんでした。
        </p>
      )}
      {list.data && passkeys.length === 0 && (
        <p className="text-sm text-ink-muted">登録されたパスキーはまだありません。</p>
      )}

      {passkeys.length > 0 && (
        <ul className="divide-y divide-slate-200">
          {passkeys.map((item) => (
            <li key={item.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
              {editingId === item.id ? (
                <form onSubmit={(e) => onRename(e, item.id)} className="flex flex-wrap items-center gap-2">
                  <label htmlFor={`passkey-edit-${item.id}`} className="sr-only">
                    新しい名前
                  </label>
                  <input
                    id={`passkey-edit-${item.id}`}
                    value={editName}
                    maxLength={64}
                    onChange={(e) => setEditName(e.target.value)}
                    className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm"
                  />
                  <button type="submit" className="rounded-lg bg-brand-500 px-3 py-1.5 text-sm font-bold text-white">
                    保存
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditingId(null)}
                    className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm text-ink-muted"
                  >
                    キャンセル
                  </button>
                </form>
              ) : (
                <div>
                  <p className="font-bold text-ink">{item.name}</p>
                  <p className="text-xs text-ink-muted">登録: {formatDate(item.created_at)}</p>
                  <p className="text-xs text-ink-muted">
                    最後に使った日時: {item.last_used_at ? formatDate(item.last_used_at) : 'まだ使っていません'}
                  </p>
                </div>
              )}
              {editingId !== item.id && (
                <div className="flex gap-2">
                  <button
                    type="button"
                    aria-label={`${item.name}の名前を変更`}
                    onClick={() => {
                      setEditingId(item.id)
                      setEditName(item.name)
                      setError(null)
                      setNotice(null)
                    }}
                    className="rounded-lg border border-slate-300 px-3 py-1.5 text-sm text-ink-muted hover:bg-slate-100"
                  >
                    名前を変更
                  </button>
                  <button
                    type="button"
                    aria-label={`${item.name}を削除`}
                    onClick={() => onDelete(item)}
                    className="rounded-lg border border-red-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50"
                  >
                    削除
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
