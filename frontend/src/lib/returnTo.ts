const DEFAULT_RETURN_PATH = '/articles'
const MAX_RETURN_PATH_LENGTH = 2048

/** ログイン画面自体（と新規登録）へは戻さない。`/loginhelp` のような別のパスは対象外 */
const AUTH_PAGE_PATTERN = /^\/(login|signup)([?#/]|$)/

/**
 * ログインが要る画面からログイン画面へ送るとき、元の場所を渡すための state を作る。
 */
export function loginRedirectState(location: { pathname: string; search: string; hash: string }): {
  from: string
} {
  return { from: location.pathname + location.search + location.hash }
}

function hasControlCharacter(path: string): boolean {
  for (let i = 0; i < path.length; i += 1) {
    const code = path.charCodeAt(i)
    if (code <= 0x1f || code === 0x7f) return true
  }
  return false
}

/**
 * ログイン成功後に戻る場所を、ログイン画面が受け取った state から取り出す。
 * 開いたリダイレクト（外部サイトへ誘導される攻撃）を避けるため、画面内のパスだけを受け、
 * 外部URL・`//` や `/\` で始まるもの・ログイン画面自体・長すぎるものは記事一覧にする。
 */
export function returnPathFrom(state: unknown): string {
  if (typeof state !== 'object' || state === null) return DEFAULT_RETURN_PATH
  const from = (state as { from?: unknown }).from
  if (typeof from !== 'string') return DEFAULT_RETURN_PATH
  if (from.length > MAX_RETURN_PATH_LENGTH) return DEFAULT_RETURN_PATH
  if (!from.startsWith('/') || from.startsWith('//') || from.startsWith('/\\')) {
    return DEFAULT_RETURN_PATH
  }
  if (from.includes('\\') || hasControlCharacter(from)) return DEFAULT_RETURN_PATH
  if (AUTH_PAGE_PATTERN.test(from)) return DEFAULT_RETURN_PATH
  return from
}
