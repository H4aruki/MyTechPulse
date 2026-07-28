const TOKEN_KEY = 'access_token'

/**
 * JWTの保管を1箇所に閉じ込めるためのラッパー。
 *
 * 現状は旧 js/main.js と同じく localStorage を使う。localStorage はXSS時に
 * トークンを窃取されうるため、httpOnly Cookie への移行を Issue #46 で予定している。
 * 移行時にアプリ側の呼び出しを書き換えずに済むよう、ここに集約しておく。
 */
export const tokenStorage = {
  get(): string | null {
    return localStorage.getItem(TOKEN_KEY)
  },
  set(token: string): void {
    localStorage.setItem(TOKEN_KEY, token)
  },
  clear(): void {
    localStorage.removeItem(TOKEN_KEY)
  },
}
