import { describe, expect, test } from 'vitest'
import { sourceBadgeClass, sourceLabel } from './articleSource'

describe('sourceLabel', () => {
  test.each([
    ['qiita', 'Qiita'],
    ['Qiita', 'Qiita'],
    ['QIITA', 'Qiita'],
    ['zenn', 'Zenn'],
    ['Zenn', 'Zenn'],
    ['ZENN', 'Zenn'],
  ])('%s は %s と表示する', (source, label) => {
    expect(sourceLabel(source)).toBe(label)
  })

  test('未知の提供元は、送られてきた名前をそのまま返す', () => {
    expect(sourceLabel('Hatena')).toBe('Hatena')
  })
})

describe('sourceBadgeClass', () => {
  test.each(['qiita', 'Qiita', 'QIITA'])('%s は Qiita の色にする', (source) => {
    expect(sourceBadgeClass(source)).toBe('bg-qiita-soft text-qiita-ink')
  })

  test.each(['zenn', 'Zenn', 'ZENN'])('%s は Zenn の色にする', (source) => {
    expect(sourceBadgeClass(source)).toBe('bg-zenn-soft text-zenn-ink')
  })

  test('未知の提供元は、薄い下地にする', () => {
    expect(sourceBadgeClass('Hatena')).toBe('bg-surface-2 text-ink-70')
  })
})
