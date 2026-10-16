import { afterEach, describe, expect, test, vi } from 'vitest'
import {
  base64urlToBuffer,
  bufferToBase64url,
  createPasskeyCredential,
  isPasskeySupported,
  PasskeyAlreadyRegisteredError,
  PasskeyCancelledError,
  toCreationOptions,
} from './passkey'

afterEach(() => {
  vi.unstubAllGlobals()
})

const bytes = (b: ArrayBuffer) => Array.from(new Uint8Array(b))

describe('base64url', () => {
  test('往復で元に戻り、+ / = を使わない', () => {
    const src = new Uint8Array([251, 255, 254, 0, 1]).buffer
    const text = bufferToBase64url(src)
    expect(text).toBe('-__-AAE')
    expect(bytes(base64urlToBuffer(text))).toEqual([251, 255, 254, 0, 1])
  })
})

describe('isPasskeySupported', () => {
  test('PublicKeyCredential が無ければ非対応', () => {
    vi.stubGlobal('PublicKeyCredential', undefined)
    expect(isPasskeySupported()).toBe(false)
  })

  test('credentials.create が無ければ非対応', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    vi.stubGlobal('navigator', { credentials: {} })
    expect(isPasskeySupported()).toBe(false)
  })

  test('両方あれば対応', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    vi.stubGlobal('navigator', { credentials: { create: vi.fn() } })
    expect(isPasskeySupported()).toBe(true)
  })
})

const publicKey = {
  rp: { id: 'localhost', name: 'MyTechPulse' },
  challenge: 'AQID',
  user: { id: 'BAUG', name: 'alice', displayName: 'alice' },
  excludeCredentials: [{ type: 'public-key', id: 'BwgJ' }],
  authenticatorSelection: { userVerification: 'required', residentKey: 'required' },
}

describe('toCreationOptions', () => {
  test('challenge・user.id・excludeCredentialsのidをバイト列にして、他の項目は保つ', () => {
    const o = toCreationOptions(publicKey)
    expect(bytes(o.challenge as ArrayBuffer)).toEqual([1, 2, 3])
    expect(bytes(o.user.id as ArrayBuffer)).toEqual([4, 5, 6])
    expect(bytes(o.excludeCredentials![0].id as ArrayBuffer)).toEqual([7, 8, 9])
    expect(o.authenticatorSelection).toEqual(publicKey.authenticatorSelection)
  })

  test('excludeCredentialsが無くても空配列にする', () => {
    const o = toCreationOptions({ ...publicKey, excludeCredentials: null })
    expect(o.excludeCredentials).toEqual([])
  })
})

describe('createPasskeyCredential', () => {
  function fakeCredential() {
    return {
      id: 'Y3JlZA',
      rawId: new Uint8Array([1, 2]).buffer,
      type: 'public-key',
      authenticatorAttachment: 'platform',
      response: {
        clientDataJSON: new Uint8Array([3]).buffer,
        attestationObject: new Uint8Array([4]).buffer,
        getTransports: () => ['internal'],
      },
      getClientExtensionResults: () => ({}),
    }
  }

  test('navigator.credentials.create の結果をJSON表現へ変える', async () => {
    const create = vi.fn().mockResolvedValue(fakeCredential())
    vi.stubGlobal('navigator', { credentials: { create } })

    const json = await createPasskeyCredential(publicKey)

    expect(create).toHaveBeenCalledOnce()
    expect(create.mock.calls[0][0].publicKey.authenticatorSelection.userVerification).toBe('required')
    expect(json).toEqual({
      id: 'Y3JlZA',
      rawId: 'AQI',
      type: 'public-key',
      authenticatorAttachment: 'platform',
      response: { clientDataJSON: 'Aw', attestationObject: 'BA', transports: ['internal'] },
      clientExtensionResults: {},
    })
  })

  test('取り消し(NotAllowedError)は PasskeyCancelledError', async () => {
    const create = vi.fn().mockRejectedValue(new DOMException('x', 'NotAllowedError'))
    vi.stubGlobal('navigator', { credentials: { create } })
    await expect(createPasskeyCredential(publicKey)).rejects.toBeInstanceOf(PasskeyCancelledError)
  })

  test('登録済みの認証器(InvalidStateError)は PasskeyAlreadyRegisteredError', async () => {
    const create = vi.fn().mockRejectedValue(new DOMException('x', 'InvalidStateError'))
    vi.stubGlobal('navigator', { credentials: { create } })
    await expect(createPasskeyCredential(publicKey)).rejects.toBeInstanceOf(PasskeyAlreadyRegisteredError)
  })
})
