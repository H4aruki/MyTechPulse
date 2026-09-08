import { afterEach, describe, expect, test } from 'vitest'
import { tokenStorage } from './auth'

describe('tokenStorage', () => {
  afterEach(() => {
    localStorage.clear()
  })

  test('トークンを保存して取得できる', () => {
    tokenStorage.set('test-token')

    expect(tokenStorage.get()).toBe('test-token')
  })

  test('トークンを削除できる', () => {
    tokenStorage.set('test-token')

    tokenStorage.clear()

    expect(tokenStorage.get()).toBeNull()
  })
})
