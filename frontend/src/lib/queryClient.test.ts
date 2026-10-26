import { afterEach, describe, expect, test, vi } from 'vitest'
import { ApiError, UnauthorizedError } from '@/api/client'
import { authQueryKey } from '@/lib/auth'
import { createQueryClient, shouldRecheckAuth } from './queryClient'

describe('shouldRecheckAuth', () => {
  test.each([
    { name: '認証確認以外の401', error: new UnauthorizedError(), key: ['feed'], expected: true },
    { name: '認証確認自体の401', error: new UnauthorizedError(), key: authQueryKey, expected: false },
    { name: 'ApiError', error: new ApiError({ status: 500 }), key: ['feed'], expected: false },
    { name: '普通のError', error: new Error('boom'), key: ['feed'], expected: false },
    { name: 'Error以外', error: 'unauthorized', key: ['feed'], expected: false },
  ])('$name → $expected', ({ error, key, expected }) => {
    expect(shouldRecheckAuth(error, key)).toBe(expected)
  })
})

describe('createQueryClient', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function failWith(queryKey: readonly unknown[], error: Error) {
    const client = createQueryClient()
    const spy = vi.spyOn(client, 'invalidateQueries')
    await client
      .fetchQuery({ queryKey, queryFn: () => Promise.reject(error), retry: false })
      .catch(() => {})
    client.clear()
    return spy
  }

  test('認証確認以外が401で失敗したら、認証確認を1回だけ無効にする', async () => {
    const spy = await failWith(['feed'], new UnauthorizedError())

    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy).toHaveBeenCalledWith({ queryKey: authQueryKey })
  })

  test('認証確認自体が401で失敗しても、再確認しない', async () => {
    const spy = await failWith([...authQueryKey], new UnauthorizedError())

    expect(spy).not.toHaveBeenCalled()
  })

  test('401以外のApiErrorでは再確認しない', async () => {
    const spy = await failWith(['feed'], new ApiError({ status: 500 }))

    expect(spy).not.toHaveBeenCalled()
  })

  test('普通のErrorでは再確認しない', async () => {
    const spy = await failWith(['feed'], new Error('boom'))

    expect(spy).not.toHaveBeenCalled()
  })

  test('成功したクエリでは再確認しない', async () => {
    const client = createQueryClient()
    const spy = vi.spyOn(client, 'invalidateQueries')

    await client.fetchQuery({ queryKey: ['feed'], queryFn: () => Promise.resolve([]), retry: false })

    expect(spy).not.toHaveBeenCalled()
    client.clear()
  })
})
