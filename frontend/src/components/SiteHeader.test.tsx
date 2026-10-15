import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test } from 'vitest'
import { SiteHeader } from './SiteHeader'

function renderHeader(authenticated: boolean) {
  render(
    <MemoryRouter initialEntries={['/login']}>
      <SiteHeader authenticated={authenticated} />
    </MemoryRouter>,
  )
}

describe('SiteHeader', () => {
  afterEach(() => {
    cleanup()
  })

  test('未ログイン時のロゴはLP（/）へ移動する', () => {
    renderHeader(false)

    expect(screen.getByRole('link', { name: 'MyTechPulse' })).toHaveAttribute('href', '/')
  })

  test('ログイン後のロゴは記事一覧へ移動する', () => {
    renderHeader(true)

    expect(screen.getByRole('link', { name: 'MyTechPulse' })).toHaveAttribute('href', '/articles')
  })
})
