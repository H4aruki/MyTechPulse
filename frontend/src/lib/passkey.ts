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

/** 端末の本人確認を開始してパスキーを作り、サーバーへ送る形で返す */
export async function createPasskeyCredential(
  publicKey: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  let credential: Credential | null
  try {
    credential = await navigator.credentials.create({ publicKey: toCreationOptions(publicKey) })
  } catch (error) {
    if (error instanceof DOMException) {
      if (error.name === 'InvalidStateError') throw new PasskeyAlreadyRegisteredError()
      if (error.name === 'NotAllowedError' || error.name === 'AbortError') {
        throw new PasskeyCancelledError()
      }
    }
    throw error
  }
  if (!credential) throw new PasskeyCancelledError()
  return credentialToJSON(credential as PublicKeyCredential)
}
