import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

type CaughtError = { problem: { status: number; code?: string }; message: string }

const BASE = 'http://api.test'

async function loadClient() {
  vi.resetModules()
  vi.stubEnv('VITE_API_BASE_URL', BASE)
  return import('./client')
}

function problem(status: number, code: string, extra: object = {}) {
  return new Response(
    JSON.stringify({ type: 'about:blank', title: 'x', status, code, detail: '詳細', ...extra }),
    { status, headers: { 'Content-Type': 'application/problem+json' } },
  )
}

describe('request', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  test('GETはCookieを送り、CSRFヘッダーとAuthorizationを付けない', async () => {
    const { request } = await loadClient()
    fetchMock.mockResolvedValue(new Response('{"ok":1}', { status: 200 }))

    const data = await request<{ ok: number }>('GET', '/api/v1/feed')

    expect(data).toEqual({ ok: 1 })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe(`${BASE}/api/v1/feed`)
    expect(init.credentials).toBe('include')
    expect(init.headers['X-MTP-CSRF']).toBeUndefined()
    expect(init.headers.Authorization).toBeUndefined()
    expect(init.body).toBeUndefined()
  })

  test('POSTはCSRFヘッダーとJSON本文を付ける', async () => {
    const { request } = await loadClient()
    fetchMock.mockResolvedValue(new Response('{}', { status: 200 }))

    await request('POST', '/api/v1/auth/login', { username: 'a' })

    const [, init] = fetchMock.mock.calls[0]
    expect(init.credentials).toBe('include')
    expect(init.headers['X-MTP-CSRF']).toBe('1')
    expect(init.headers['Content-Type']).toBe('application/json')
    expect(init.headers.Authorization).toBeUndefined()
    expect(init.body).toBe('{"username":"a"}')
  })

  test('204は本文を読まずundefinedを返す', async () => {
    const { request } = await loadClient()
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))

    await expect(request('POST', '/api/v1/auth/logout')).resolves.toBeUndefined()
  })

  test('401はUnauthorizedErrorになる', async () => {
    const { request, UnauthorizedError } = await loadClient()
    fetchMock.mockResolvedValue(problem(401, 'unauthorized'))

    await expect(request('GET', '/api/v1/auth/me')).rejects.toBeInstanceOf(UnauthorizedError)
  })

  test('Problem DetailsはApiErrorとして業務コードと状態を保つ', async () => {
    const { request, ApiError } = await loadClient()
    fetchMock.mockResolvedValue(problem(409, 'username_taken', { detail: '使用済み' }))

    const error = await request('POST', '/api/v1/auth/signup', {}).catch((e: unknown) => e) as CaughtError

    expect(error).toBeInstanceOf(ApiError)
    expect(error.problem.status).toBe(409)
    expect(error.problem.code).toBe('username_taken')
    expect(error.message).toBe('使用済み')
  })

  test('HTMLなど想定外の失敗応答は内容を出さず安全な文言にする', async () => {
    const { request, ApiError } = await loadClient()
    fetchMock.mockResolvedValue(new Response('<html>secret</html>', { status: 502 }))

    const error = await request('GET', '/api/v1/feed').catch((e: unknown) => e) as CaughtError

    expect(error).toBeInstanceOf(ApiError)
    expect(error.problem.status).toBe(502)
    expect(error.message).not.toContain('secret')
  })

  test('通信失敗と不正なJSONは安全な日本語ApiErrorにする', async () => {
    const { request, ApiError } = await loadClient()
    fetchMock.mockRejectedValueOnce(new TypeError('boom'))
    await expect(request('GET', '/x')).rejects.toBeInstanceOf(ApiError)

    fetchMock.mockResolvedValueOnce(new Response('not json', { status: 200 }))
    await expect(request('GET', '/x')).rejects.toBeInstanceOf(ApiError)
  })
})
