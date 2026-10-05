import { beforeEach, describe, expect, test, vi } from 'vitest'

const BASE = 'http://api.test'

async function loadEndpoints() {
  vi.resetModules()
  vi.stubEnv('VITE_API_BASE_URL', BASE)
  return import('./endpoints')
}

describe('endpoints', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    fetchMock.mockImplementation(async () => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
  })

  function lastCall() {
    const [url, init] = fetchMock.mock.calls.at(-1)!
    return { url: url as string, method: init.method as string, body: init.body as string | undefined }
  }

  test('loginはPOST /api/v1/auth/login', async () => {
    const { login } = await loadEndpoints()
    await login({ username: 'u', password: 'p' })
    expect(lastCall()).toEqual({
      url: `${BASE}/api/v1/auth/login`,
      method: 'POST',
      body: '{"username":"u","password":"p"}',
    })
  })

  test('signupはfavorite_tagsを含めてPOSTする', async () => {
    const { signup } = await loadEndpoints()
    await signup({ username: 'u', password: 'p', favorite_tags: ['go'] })
    expect(lastCall()).toEqual({
      url: `${BASE}/api/v1/auth/signup`,
      method: 'POST',
      body: '{"username":"u","password":"p","favorite_tags":["go"]}',
    })
  })

  test('currentUserはGET /api/v1/auth/me', async () => {
    const { currentUser } = await loadEndpoints()
    await currentUser()
    expect(lastCall()).toMatchObject({ url: `${BASE}/api/v1/auth/me`, method: 'GET' })
  })

  test('logoutはPOST /api/v1/auth/logout', async () => {
    const { logout } = await loadEndpoints()
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await logout()
    expect(lastCall()).toMatchObject({ url: `${BASE}/api/v1/auth/logout`, method: 'POST' })
  })

  test('fetchFeedはGET /api/v1/feed', async () => {
    const { fetchFeed } = await loadEndpoints()
    await fetchFeed()
    expect(lastCall()).toMatchObject({ url: `${BASE}/api/v1/feed`, method: 'GET' })
  })

  test('recordArticleClickは{tags}をPOSTする', async () => {
    const { recordArticleClick } = await loadEndpoints()
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await recordArticleClick(['go', 'react'])
    expect(lastCall()).toEqual({
      url: `${BASE}/api/v1/feedback/article-clicks`,
      method: 'POST',
      body: '{"tags":["go","react"]}',
    })
  })
})
