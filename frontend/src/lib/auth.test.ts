import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { describe, expect, test, vi } from 'vitest'
import { authQueryKey, useCurrentUser } from './auth'

vi.mock('@/api/endpoints', () => ({
  currentUser: vi.fn().mockResolvedValue({ id: 1, username: 'u', role: 'member' }),
}))

describe('useCurrentUser', () => {
  test('/auth/meの結果をauthQueryKeyで保持する', async () => {
    const client = new QueryClient()
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children)

    const { result } = renderHook(() => useCurrentUser(), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(client.getQueryData(authQueryKey)).toEqual({ id: 1, username: 'u', role: 'member' })
  })
})
