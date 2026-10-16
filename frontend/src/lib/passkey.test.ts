import { afterEach, describe, expect, test, vi } from 'vitest'
import {
  assertionToJSON,
  base64urlToBuffer,
  bufferToBase64url,
  createPasskeyCredential,
  getPasskeyAssertion,
  isPasskeyLoginSupported,
  isPasskeySupported,
  PasskeyAlreadyRegisteredError,
  PasskeyCancelledError,
  PasskeyLoginCancelledError,
  toCreationOptions,
  toRequestOptions,
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

describe('isPasskeyLoginSupported', () => {
  test('PublicKeyCredential が無ければ非対応', () => {
    vi.stubGlobal('PublicKeyCredential', undefined)
    expect(isPasskeyLoginSupported()).toBe(false)
  })

  test('credentials.get が無ければ非対応（create だけでは対応としない）', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    vi.stubGlobal('navigator', { credentials: { create: vi.fn() } })
    expect(isPasskeyLoginSupported()).toBe(false)
  })

  test('両方あれば対応', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {})
    vi.stubGlobal('navigator', { credentials: { get: vi.fn() } })
    expect(isPasskeyLoginSupported()).toBe(true)
  })
})

const requestKey = {
  rpId: 'localhost',
  challenge: 'AQID',
  allowCredentials: null,
  userVerification: 'required',
}

describe('toRequestOptions', () => {
  test('challengeをバイト列にして、他の項目は保つ', () => {
    const o = toRequestOptions(requestKey)
    expect(bytes(o.challenge as ArrayBuffer)).toEqual([1, 2, 3])
    expect(o.rpId).toBe('localhost')
    expect(o.userVerification).toBe('required')
  })

  test('allowCredentialsが無い（null・未指定）ときは空配列にする', () => {
    expect(toRequestOptions(requestKey).allowCredentials).toEqual([])
    const { allowCredentials, ...withoutAllow } = requestKey
    expect(allowCredentials).toBeNull()
    expect(toRequestOptions(withoutAllow).allowCredentials).toEqual([])
  })

  test('allowCredentialsがあれば、idをバイト列にする', () => {
    const o = toRequestOptions({
      ...requestKey,
      allowCredentials: [{ type: 'public-key', id: 'BwgJ' }],
    })
    expect(o.allowCredentials).toHaveLength(1)
    expect(o.allowCredentials![0].type).toBe('public-key')
    expect(bytes(o.allowCredentials![0].id as ArrayBuffer)).toEqual([7, 8, 9])
  })
})

function fakeAssertion(userHandle: ArrayBuffer | null) {
  return {
    id: 'Y3JlZA',
    rawId: new Uint8Array([1, 2]).buffer,
    type: 'public-key',
    authenticatorAttachment: 'platform',
    response: {
      clientDataJSON: new Uint8Array([3]).buffer,
      authenticatorData: new Uint8Array([4]).buffer,
      signature: new Uint8Array([251, 255]).buffer,
      userHandle,
    },
    getClientExtensionResults: () => ({}),
  } as unknown as PublicKeyCredential
}

describe('assertionToJSON', () => {
  test('応答の各バイト列をbase64urlにする', () => {
    expect(assertionToJSON(fakeAssertion(new Uint8Array([6]).buffer))).toEqual({
      id: 'Y3JlZA',
      rawId: 'AQI',
      type: 'public-key',
      authenticatorAttachment: 'platform',
      response: {
        clientDataJSON: 'Aw',
        authenticatorData: 'BA',
        signature: '-_8',
        userHandle: 'Bg',
      },
      clientExtensionResults: {},
    })
  })

  test('userHandleが無い（null）応答では、その項目を含めない', () => {
    const json = assertionToJSON(fakeAssertion(null))
    expect(json.response).toEqual({ clientDataJSON: 'Aw', authenticatorData: 'BA', signature: '-_8' })
  })
})

describe('getPasskeyAssertion', () => {
  test('navigator.credentials.get に変換した設定を渡し、結果をJSON表現へ変える', async () => {
    const get = vi.fn().mockResolvedValue(fakeAssertion(new Uint8Array([6]).buffer))
    vi.stubGlobal('navigator', { credentials: { get } })

    const json = await getPasskeyAssertion(requestKey)

    expect(get).toHaveBeenCalledOnce()
    const passed = get.mock.calls[0][0].publicKey
    expect(bytes(passed.challenge)).toEqual([1, 2, 3])
    expect(passed.allowCredentials).toEqual([])
    expect(json.rawId).toBe('AQI')
    expect((json.response as Record<string, unknown>).userHandle).toBe('Bg')
  })

  test.each(['NotAllowedError', 'AbortError'])(
    '%s は PasskeyLoginCancelledError（登録用の取り消しとは別）',
    async (name) => {
      const get = vi.fn().mockRejectedValue(new DOMException('x', name))
      vi.stubGlobal('navigator', { credentials: { get } })
      const result = getPasskeyAssertion(requestKey)
      await expect(result).rejects.toBeInstanceOf(PasskeyLoginCancelledError)
      await expect(result).rejects.not.toBeInstanceOf(PasskeyCancelledError)
    },
  )

  test('結果が null のときも取り消しとして扱う', async () => {
    const get = vi.fn().mockResolvedValue(null)
    vi.stubGlobal('navigator', { credentials: { get } })
    await expect(getPasskeyAssertion(requestKey)).rejects.toBeInstanceOf(PasskeyLoginCancelledError)
  })

  test('それ以外の失敗は取り消しにせず、そのまま投げる', async () => {
    const error = new DOMException('x', 'SecurityError')
    const get = vi.fn().mockRejectedValue(error)
    vi.stubGlobal('navigator', { credentials: { get } })
    await expect(getPasskeyAssertion(requestKey)).rejects.toBe(error)
  })
})
