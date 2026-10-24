import { describe, expect, test } from 'vitest'
import { loginRedirectState, returnPathFrom } from './returnTo'

describe('returnPathFrom', () => {
  test.each([
    ['/mypage', '/mypage'],
    ['/settings?tab=x#a', '/settings?tab=x#a'],
    ['/articles', '/articles'],
    ['/loginhelp', '/loginhelp'],
    ['/signups', '/signups'],
  ])('画面内のパス %s はそのまま返す', (from, expected) => {
    expect(returnPathFrom({ from })).toBe(expected)
  })

  test.each<[string, unknown]>([
    ['undefined', undefined],
    ['null', null],
    ['文字列', '/mypage'],
    ['fromが無い', {}],
    ['fromが数値', { from: 1 }],
    ['fromが空', { from: '' }],
    ['//で始まる', { from: '//evil.example' }],
    ['/\\で始まる', { from: '/\\evil' }],
    ['途中に\\を含む', { from: '/a\\b' }],
    ['制御文字を含む', { from: '/\t/evil.example' }],
    ['外部URL', { from: 'https://evil.example' }],
    ['javascript:', { from: 'javascript:alert(1)' }],
    ['/login', { from: '/login' }],
    ['/login?x=1', { from: '/login?x=1' }],
    ['/login#a', { from: '/login#a' }],
    ['/login/', { from: '/login/' }],
    ['/signup', { from: '/signup' }],
    ['長すぎる', { from: `/${'a'.repeat(2048)}` }],
  ])('%s は記事一覧にする', (_name, state) => {
    expect(returnPathFrom(state)).toBe('/articles')
  })

  test('長さの上限ちょうどは通す', () => {
    const from = `/${'a'.repeat(2047)}`
    expect(returnPathFrom({ from })).toBe(from)
  })
})

describe('loginRedirectState', () => {
  test('パス・クエリ・ハッシュをつないで from に入れ、returnPathFrom で戻せる', () => {
    const state = loginRedirectState({ pathname: '/settings', search: '?tab=x', hash: '#a' })
    expect(state).toEqual({ from: '/settings?tab=x#a' })
    expect(returnPathFrom(state)).toBe('/settings?tab=x#a')
  })
})
