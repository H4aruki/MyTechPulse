import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, test } from 'vitest'
import { SiteHeader } from './SiteHeader'

function renderHeader(authenticated: boolean, path = '/login') {
  render(
    <MemoryRouter initialEntries={[path]}>
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

  test('ログイン後はマイページと設定への入口を出す', () => {
    renderHeader(true)

    expect(screen.getByRole('link', { name: 'マイページ' })).toHaveAttribute('href', '/mypage')
    expect(screen.getByRole('link', { name: '設定' })).toHaveAttribute('href', '/settings')
  })

  test('未ログイン時はマイページへの入口を出さない', () => {
    renderHeader(false)

    expect(screen.queryByRole('link', { name: 'マイページ' })).not.toBeInTheDocument()
  })

  test('ログイン後のメニューは、ほかの操作と同じ最小高さにする', () => {
    renderHeader(true, '/articles')

    for (const name of ['記事一覧', 'マイページ', '設定']) {
      expect(screen.getByRole('link', { name })).toHaveClass(
        'inline-flex',
        'min-h-11',
        'items-center',
      )
    }
    expect(screen.getByRole('button', { name: 'ログアウト' })).toHaveClass('min-h-11')
  })

  test('記事一覧にいるときは、記事一覧のリンクにだけ aria-current を付ける', () => {
    renderHeader(true, '/articles')

    expect(screen.getByRole('link', { name: '記事一覧' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'マイページ' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('link', { name: '設定' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('link', { name: 'MyTechPulse' })).not.toHaveAttribute('aria-current')
  })

  test('マイページにいるときは、マイページのリンクにだけ aria-current を付ける', () => {
    renderHeader(true, '/mypage')

    expect(screen.getByRole('link', { name: 'マイページ' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '記事一覧' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('link', { name: '設定' })).not.toHaveAttribute('aria-current')
  })

  test('設定にいるときは、設定のリンクにだけ aria-current を付ける', () => {
    renderHeader(true, '/settings')

    expect(screen.getByRole('link', { name: '設定' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '記事一覧' })).not.toHaveAttribute('aria-current')
    expect(screen.getByRole('link', { name: 'マイページ' })).not.toHaveAttribute('aria-current')
  })

  test('ログイン画面では、ログインのリンクにだけ aria-current を付ける', () => {
    renderHeader(false, '/login')

    expect(screen.getByRole('link', { name: 'ログイン' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '新規登録' })).not.toHaveAttribute('aria-current')
  })

  test('新規登録画面では、新規登録のリンクにだけ aria-current を付ける', () => {
    renderHeader(false, '/signup')

    expect(screen.getByRole('link', { name: '新規登録' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'ログイン' })).not.toHaveAttribute('aria-current')
  })
})
