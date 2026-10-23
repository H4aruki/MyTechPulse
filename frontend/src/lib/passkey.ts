// パスキー(WebAuthn)のブラウザ側の処理。サーバーとはbase64url文字列のJSONでやり取りし、
// ここでブラウザのAPIが求めるArrayBufferへ変換する。

export const passkeysQueryKey = ['auth', 'passkeys'] as const

/** 利用者が端末の確認を取り消した、または時間切れになった */
export class PasskeyCancelledError extends Error {
  constructor() {
    super('パスキーの登録が取り消されたか、時間切れになりました。')
    this.name = 'PasskeyCancelledError'
  }
}

/** ログインの本人確認を、利用者が取り消した、または時間切れになった */
export class PasskeyLoginCancelledError extends Error {
  constructor() {
    super('パスキーでのログインが取り消されたか、時間切れになりました。')
    this.name = 'PasskeyLoginCancelledError'
  }
}

/** 画面側が自分で中止した（AbortController）。利用者の取り消しと同じく静かに扱うため、派生クラスにする */
export class PasskeyLoginAbortedError extends PasskeyLoginCancelledError {
  constructor() {
    super()
    this.name = 'PasskeyLoginAbortedError'
  }
}

export const PASSKEY_LOGIN_INTERRUPTED =
  'ほかのアプリに切り替えたため、パスワードマネージャーの本人確認が中断されました。もう一度お試しください。確認コードが必要な場合は、先にブラウザの設定（Chrome では Google パスワード マネージャー）で本人確認を済ませてから、もう一度お試しください。'
export const PASSKEY_REGISTRATION_INTERRUPTED =
  'ほかのアプリに切り替えたため、パスワードマネージャーの本人確認が中断され、パスキーの追加ができませんでした。もう一度お試しください。確認コードが必要な場合は、先にブラウザの設定（Chrome では Google パスワード マネージャー）で本人確認を済ませてから、もう一度お試しください。'

/**
 * ログインの本人確認が、別のアプリへ切り替えている間に中断された。
 * 確認コードを見に行く間に OS がシートを閉じる。取り消しとは区別して案内するための派生クラス
 */
export class PasskeyLoginInterruptedError extends PasskeyLoginCancelledError {
  constructor() {
    super()
    this.message = PASSKEY_LOGIN_INTERRUPTED
    this.name = 'PasskeyLoginInterruptedError'
  }
}

/** パスキーの追加が、別のアプリへ切り替えている間に中断された */
export class PasskeyInterruptedError extends PasskeyCancelledError {
  constructor() {
    super()
    this.message = PASSKEY_REGISTRATION_INTERRUPTED
    this.name = 'PasskeyInterruptedError'
  }
}

/** この端末のパスキーがすでに登録されている */
export class PasskeyAlreadyRegisteredError extends Error {
  constructor() {
    super('この端末のパスキーはすでに登録されています。')
    this.name = 'PasskeyAlreadyRegisteredError'
  }
}

/** ブラウザがパスキーの登録に対応しているか。非対応なら画面の項目ごと隠す */
export function isPasskeySupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.PublicKeyCredential === 'function' &&
    typeof navigator !== 'undefined' &&
    typeof navigator.credentials?.create === 'function'
  )
}

/** ブラウザがパスキーでのログインに対応しているか。非対応ならボタンごと隠す */
export function isPasskeyLoginSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.PublicKeyCredential === 'function' &&
    typeof navigator !== 'undefined' &&
    typeof navigator.credentials?.get === 'function'
  )
}

export function base64urlToBuffer(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer
}

export function bufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

interface DescriptorJSON {
  id: string
  [key: string]: unknown
}

/** サーバーのJSON表現を navigator.credentials.create() の引数へ変える */
export function toCreationOptions(
  publicKey: Record<string, unknown>,
): PublicKeyCredentialCreationOptions {
  const user = publicKey.user as { id: string } & Record<string, unknown>
  const exclude = (publicKey.excludeCredentials as DescriptorJSON[] | null | undefined) ?? []
  return {
    ...publicKey,
    challenge: base64urlToBuffer(publicKey.challenge as string),
    user: { ...user, id: base64urlToBuffer(user.id) },
    excludeCredentials: exclude.map((c) => ({ ...c, id: base64urlToBuffer(c.id) })),
  } as unknown as PublicKeyCredentialCreationOptions
}

/** 端末の応答を、サーバーへ送るJSON表現へ変える */
export function credentialToJSON(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse
  const json: Record<string, unknown> = {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      attestationObject: bufferToBase64url(response.attestationObject),
      transports: typeof response.getTransports === 'function' ? response.getTransports() : [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
  if (credential.authenticatorAttachment) json.authenticatorAttachment = credential.authenticatorAttachment
  return json
}

/** サーバーのJSON表現を navigator.credentials.get() の引数へ変える */
export function toRequestOptions(
  publicKey: Record<string, unknown>,
): PublicKeyCredentialRequestOptions {
  const allow = (publicKey.allowCredentials as DescriptorJSON[] | null | undefined) ?? []
  return {
    ...publicKey,
    challenge: base64urlToBuffer(publicKey.challenge as string),
    allowCredentials: allow.map((c) => ({ ...c, id: base64urlToBuffer(c.id) })),
  } as unknown as PublicKeyCredentialRequestOptions
}

/** 端末のログイン応答を、サーバーへ送るJSON表現へ変える */
export function assertionToJSON(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAssertionResponse
  const json: Record<string, unknown> = {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      authenticatorData: bufferToBase64url(response.authenticatorData),
      signature: bufferToBase64url(response.signature),
      // 利用者名なしのログインでは利用者の特定に使う。無い応答は、サーバー側で失敗になる
      ...(response.userHandle ? { userHandle: bufferToBase64url(response.userHandle) } : {}),
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
  if (credential.authenticatorAttachment) json.authenticatorAttachment = credential.authenticatorAttachment
  return json
}

/**
 * ブラウザが conditional UI（利用者名欄の autofill にパスキーを出す）に対応しているか。
 * 関数が無い・false・reject はどれも false にする
 */
export async function isConditionalPasskeyLoginAvailable(): Promise<boolean> {
  if (!isPasskeyLoginSupported()) return false
  const check = (window.PublicKeyCredential as { isConditionalMediationAvailable?: () => Promise<boolean> })
    .isConditionalMediationAvailable
  if (typeof check !== 'function') return false
  try {
    return (await check.call(window.PublicKeyCredential)) === true
  } catch {
    return false
  }
}

/** 呼び出しの間に、ページが非表示になったかを記録する。終わったら必ず stop() でリスナーを外す */
function watchPageHidden(): { wasHidden: () => boolean; stop: () => void } {
  let hidden = false
  const onChange = () => {
    if (document.visibilityState === 'hidden') hidden = true
  }
  document.addEventListener('visibilitychange', onChange)
  return {
    wasHidden: () => hidden,
    stop: () => document.removeEventListener('visibilitychange', onChange),
  }
}

export interface PasskeyAssertionOptions {
  signal?: AbortSignal
  /** 省略時は端末の確認画面を直接出す（modal） */
  mediation?: 'conditional'
}

/** 端末の本人確認を開始してログイン用の応答を得て、サーバーへ送る形で返す */
export async function getPasskeyAssertion(
  publicKey: Record<string, unknown>,
  options: PasskeyAssertionOptions = {},
): Promise<Record<string, unknown>> {
  const { signal, mediation } = options
  let credential: Credential | null
  const watcher = watchPageHidden()
  try {
    credential = await navigator.credentials.get({
      publicKey: toRequestOptions(publicKey),
      ...(mediation ? { mediation } : {}),
      ...(signal ? { signal } : {}),
    })
  } catch (error) {
    // 新しいブラウザは signal.reason をそのまま投げるため、エラー名ではなく signal で判定する
    if (signal?.aborted) throw new PasskeyLoginAbortedError()
    if (
      error instanceof DOMException &&
      (error.name === 'NotAllowedError' || error.name === 'AbortError')
    ) {
      throw watcher.wasHidden() ? new PasskeyLoginInterruptedError() : new PasskeyLoginCancelledError()
    }
    throw error
  } finally {
    watcher.stop()
  }
  // 解決と中止が競合した場合も、中止を優先して結果を使わせない
  if (signal?.aborted) throw new PasskeyLoginAbortedError()
  if (!credential) throw new PasskeyLoginCancelledError()
  return assertionToJSON(credential as PublicKeyCredential)
}

/** 端末の本人確認を開始してパスキーを作り、サーバーへ送る形で返す */
export async function createPasskeyCredential(
  publicKey: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  let credential: Credential | null
  const watcher = watchPageHidden()
  try {
    credential = await navigator.credentials.create({ publicKey: toCreationOptions(publicKey) })
  } catch (error) {
    if (error instanceof DOMException) {
      if (error.name === 'InvalidStateError') throw new PasskeyAlreadyRegisteredError()
      if (error.name === 'NotAllowedError' || error.name === 'AbortError') {
        throw watcher.wasHidden() ? new PasskeyInterruptedError() : new PasskeyCancelledError()
      }
    }
    throw error
  } finally {
    watcher.stop()
  }
  if (!credential) throw new PasskeyCancelledError()
  return credentialToJSON(credential as PublicKeyCredential)
}
