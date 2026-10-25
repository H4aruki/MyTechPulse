import { QueryCache, QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { UnauthorizedError } from '@/api/client'
import {
  identifyUser,
  initAnalytics,
  resetAnalytics,
  resetAnalyticsStateForTest,
  resetOnSessionEnd,
} from './analytics'

const posthog = vi.hoisted(() => ({
  init: vi.fn(),
  identify: vi.fn(),
  reset: vi.fn(),
}))

vi.mock('posthog-js', () => ({ default: posthog }))

const KEY = 'phc_dummy'
const HOST = 'https://us.i.posthog.com'

beforeEach(() => {
  vi.clearAllMocks()
  resetAnalyticsStateForTest()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('initAnalytics', () => {
  test('KEYが未設定なら初期化しない', async () => {
    await initAnalytics({ VITE_POSTHOG_HOST: HOST })
    await initAnalytics({ VITE_POSTHOG_KEY: '  ', VITE_POSTHOG_HOST: HOST })

    expect(posthog.init).not.toHaveBeenCalled()
  })

  test('KEYがあってもHOSTが未設定なら、警告を出して初期化しない', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)

    await initAnalytics({ VITE_POSTHOG_KEY: KEY })

    expect(posthog.init).not.toHaveBeenCalled()
    expect(warn).toHaveBeenCalledOnce()
  })

  test('KEYとHOSTがあれば初期化し、入力欄を伏せ、画面遷移も記録する', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })

    expect(posthog.init).toHaveBeenCalledOnce()
    const [key, options] = posthog.init.mock.calls[0]
    expect(key).toBe(KEY)
    expect(options).toMatchObject({
      api_host: HOST,
      capture_pageview: 'history_change',
      autocapture: true,
      session_recording: { maskAllInputs: true },
    })
    expect(options.session_recording.maskTextSelector).toContain('.ph-no-capture')
  })
})

describe('identifyUser', () => {
  test('内部IDだけで識別し、名前やメールを渡さない', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })

    identifyUser(42)

    expect(posthog.identify).toHaveBeenCalledOnce()
    expect(posthog.identify.mock.calls[0]).toEqual(['42'])
  })

  test('初期化の完了前に呼ばれても、完了後に同じ形で識別する', async () => {
    identifyUser(7)
    expect(posthog.identify).not.toHaveBeenCalled()

    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })

    expect(posthog.identify.mock.calls).toEqual([['7']])
  })

  test('無効のときは何も送らない', () => {
    identifyUser(1)
    resetAnalytics()

    expect(posthog.identify).not.toHaveBeenCalled()
    expect(posthog.reset).not.toHaveBeenCalled()
  })
})

describe('ログアウト', () => {
  test('resetAnalyticsは識別を外す', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })
    identifyUser(1)

    resetAnalytics()

    expect(posthog.reset).toHaveBeenCalledOnce()
  })

  test('ログイン状態のキャッシュが消えるとresetを呼ぶ。別のキャッシュでは呼ばない', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })
    const authKey = ['auth', 'me'] as const
    const queryClient = new QueryClient({ queryCache: new QueryCache() })
    const unsubscribe = resetOnSessionEnd(queryClient.getQueryCache(), authKey)
    queryClient.setQueryData(authKey, { id: 1 })
    queryClient.setQueryData(['feed'], [])

    queryClient.removeQueries({ queryKey: ['feed'] })
    expect(posthog.reset).not.toHaveBeenCalled()

    queryClient.removeQueries({ queryKey: authKey })
    expect(posthog.reset).toHaveBeenCalledOnce()

    unsubscribe()
  })

  test('/auth/meが401になったときもresetを呼ぶ。通信障害などでは呼ばない', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })
    const authKey = ['auth', 'me'] as const
    const queryClient = new QueryClient({ queryCache: new QueryCache() })
    const unsubscribe = resetOnSessionEnd(queryClient.getQueryCache(), authKey)

    await queryClient
      .fetchQuery({ queryKey: authKey, queryFn: () => Promise.reject(new Error('network')), retry: false })
      .catch(() => undefined)
    await queryClient
      .fetchQuery({ queryKey: ['feed'], queryFn: () => Promise.reject(new UnauthorizedError()), retry: false })
      .catch(() => undefined)
    expect(posthog.reset).not.toHaveBeenCalled()

    await queryClient
      .fetchQuery({ queryKey: authKey, queryFn: () => Promise.reject(new UnauthorizedError()), retry: false })
      .catch(() => undefined)
    expect(posthog.reset).toHaveBeenCalledOnce()

    unsubscribe()
  })

  test('リセット後は同じ利用者でも識別し直す', async () => {
    await initAnalytics({ VITE_POSTHOG_KEY: KEY, VITE_POSTHOG_HOST: HOST })
    identifyUser(1)
    resetAnalytics()
    identifyUser(1)

    expect(posthog.identify).toHaveBeenCalledTimes(2)
  })
})
