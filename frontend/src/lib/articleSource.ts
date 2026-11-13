/** 提供元の表示名。APIは "Qiita" / "Zenn" と小文字の両方を返すことがある。 */
export function sourceLabel(source: string): string {
  const lower = source.toLowerCase()
  if (lower === 'qiita') return 'Qiita'
  if (lower === 'zenn') return 'Zenn'
  return source
}

/**
 * 提供元バッジ。下地はトップページの見本カードと同じ薄い緑・青。
 * 見本の文字色は装飾向けで、白に近い下地だと読みにくいため、文字だけ濃い緑・青にする。
 */
export function sourceBadgeClass(source: string): string {
  const lower = source.toLowerCase()
  if (lower === 'qiita') return 'bg-qiita-soft text-qiita-ink'
  if (lower === 'zenn') return 'bg-zenn-soft text-zenn-ink'
  return 'bg-surface-2 text-ink-70'
}
