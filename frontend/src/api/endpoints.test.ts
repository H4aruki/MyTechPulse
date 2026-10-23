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

  test('アンケートの窓口を正しい方式とパスで呼ぶ', async () => {
    const e = await loadEndpoints()
    const id = '11111111-1111-4111-8111-111111111111'
    await e.fetchFeedbackStatus()
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/status`, method: 'GET', body: undefined })
    await e.requestFeedbackPresentation(id)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/presentations`, method: 'POST', body: `{"prompt_id":"${id}"}` })
    await e.submitFeedbackOverall({ prompt_id: id, question_id: 10, score: 2 })
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/submissions`, method: 'POST', body: `{"prompt_id":"${id}","question_id":10,"score":2}` })
    await e.completeFeedbackFollowup(id, [{ question_id: 11, score: 3 }])
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/submissions/${id}`, method: 'PUT', body: '{"answers":[{"question_id":11,"score":3}]}' })
    await e.dismissFeedback(id)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/user-feedback/dismissals`, method: 'POST', body: `{"prompt_id":"${id}"}` })
  })

  test('パスキーのログインは、beginは本文なし・finishはchallengeTokenとcredentialを、POSTで送る', async () => {
    const e = await loadEndpoints()
    await e.beginPasskeyLogin()
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/auth/passkeys/login/begin`, method: 'POST', body: undefined })
    await e.finishPasskeyLogin({ challengeToken: 'tok', credential: { id: 'abc' } })
    expect(lastCall()).toEqual({
      url: `${BASE}/api/v1/auth/passkeys/login/finish`,
      method: 'POST',
      body: '{"challengeToken":"tok","credential":{"id":"abc"}}',
    })
    // 更新系の要求なので、CSRF対策のヘッダーと、Cookieの送信を付ける
    const [, init] = fetchMock.mock.calls.at(-1)!
    expect(init.headers['X-MTP-CSRF']).toBe('1')
    expect(init.credentials).toBe('include')
  })

  test('保存した記事は、保存がPUT・解除がDELETE・一覧がGET（limitとoffset付き）', async () => {
    const e = await loadEndpoints()
    await e.saveArticle(7)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/saved-articles/7`, method: 'PUT', body: undefined })
    await e.unsaveArticle(7)
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/saved-articles/7`, method: 'DELETE', body: undefined })
    await e.fetchSavedArticles({ limit: 20, offset: 40 })
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/saved-articles?limit=20&offset=40`, method: 'GET', body: undefined })
  })
})
