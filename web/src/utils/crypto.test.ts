import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { encryptMock, decryptMock } = vi.hoisted(() => ({
  encryptMock: vi.fn(),
  decryptMock: vi.fn()
}))

vi.mock('@yeying-community/web3-bs', () => ({
  encrypt: encryptMock,
  decrypt: decryptMock
}))

import {
  decodeUtf8,
  decryptBlobContent,
  decryptBytes,
  decryptTextContent,
  encodeUtf8,
  encryptBytes,
  encryptFileContent,
  encryptTextContent
} from '@/utils/crypto'

describe('crypto helpers', () => {
  beforeEach(() => {
    encryptMock.mockReset()
    decryptMock.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('encodes and decodes UTF-8 text', () => {
    const text = '夜莺 encrypted directory 🔐'
    expect(decodeUtf8(encodeUtf8(text))).toBe(text)
  })

  it('passes raw Uint8Array to web3-bs encrypt', async () => {
    encryptMock.mockResolvedValue('ciphertext-1')

    const bytes = new Uint8Array([0, 1, 2, 250, 251, 252])
    const ciphertext = await encryptBytes(bytes, 'secret', 'aes-256-gcm')

    expect(ciphertext).toBe('ciphertext-1')
    expect(encryptMock).toHaveBeenCalledTimes(1)
    expect(encryptMock).toHaveBeenCalledWith({
      data: bytes,
      password: 'secret',
      suite: 'aes-256-gcm'
    })
  })

  it('encrypts text content through UTF-8 bytes', async () => {
    encryptMock.mockResolvedValue('ciphertext-2')

    const ciphertext = await encryptTextContent('backup.txt content', 'secret')

    expect(ciphertext).toBe('ciphertext-2')
    const payload = encryptMock.mock.calls[0]?.[0]
    expect(payload.password).toBe('secret')
    expect(payload.data).toBeInstanceOf(Uint8Array)
    expect(decodeUtf8(payload.data)).toBe('backup.txt content')
  })

  it('encrypts file content and stores ciphertext blob as text', async () => {
    encryptMock.mockResolvedValue('ciphertext-file')

    const file = new File([new Uint8Array([1, 2, 3, 4])], 'backup.bin', {
      type: 'application/octet-stream'
    })

    const blob = await encryptFileContent(file, 'secret')

    expect(encryptMock).toHaveBeenCalledTimes(1)
    const payload = encryptMock.mock.calls[0]?.[0]
    expect(payload.data).toBeInstanceOf(Uint8Array)
    expect(Array.from(payload.data)).toEqual([1, 2, 3, 4])
    expect(await blob.text()).toBe('ciphertext-file')
    expect(blob.type).toBe('text/plain;charset=utf-8')
  })

  it('decrypts ciphertext bytes and text content without base64 hop', async () => {
    const bytes = new Uint8Array([72, 101, 108, 108, 111])
    decryptMock.mockResolvedValue(bytes)

    const decryptedBytes = await decryptBytes('ciphertext-3', 'secret')
    const decryptedText = await decryptTextContent('ciphertext-3', 'secret')

    expect(decryptMock).toHaveBeenNthCalledWith(1, {
      ciphertext: 'ciphertext-3',
      password: 'secret'
    })
    expect(decryptMock).toHaveBeenNthCalledWith(2, {
      ciphertext: 'ciphertext-3',
      password: 'secret'
    })
    expect(decryptedBytes).toBe(bytes)
    expect(decryptedText).toBe('Hello')
  })

  it('decrypts blob content by reading ciphertext text first', async () => {
    const bytes = new Uint8Array([1, 3, 5, 7])
    decryptMock.mockResolvedValue(bytes)

    const blob = new Blob(['ciphertext-from-server'], { type: 'text/plain' })
    const decryptedBytes = await decryptBlobContent(blob, 'secret')

    expect(decryptMock).toHaveBeenCalledWith({
      ciphertext: 'ciphertext-from-server',
      password: 'secret'
    })
    expect(Array.from(decryptedBytes)).toEqual([1, 3, 5, 7])
  })
})
