import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  ENCRYPTED_DIRECTORY_MARKER,
  buildEncryptedDirectoryMetadata,
  buildEncryptedDirectoryMetadataPath,
  getEncryptedDirectoryPassword,
  isEncryptedDirectoryMetadataFileName,
  isPathInsideEncryptedRoot,
  normalizeDirectoryPath,
  normalizeDirectoryRoot,
  resolveEncryptedRoot,
  setEncryptedDirectoryPassword
} from '@/utils/encryptedDirectory'

class SessionStorageMock {
  private store = new Map<string, string>()

  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null
  }

  setItem(key: string, value: string): void {
    this.store.set(key, value)
  }

  removeItem(key: string): void {
    this.store.delete(key)
  }

  clear(): void {
    this.store.clear()
  }
}

describe('encryptedDirectory helpers', () => {
  const originalSessionStorage = globalThis.sessionStorage

  beforeEach(() => {
    vi.stubGlobal('sessionStorage', new SessionStorageMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    if (originalSessionStorage) {
      vi.stubGlobal('sessionStorage', originalSessionStorage)
    }
  })

  it('normalizes directory paths consistently', () => {
    expect(normalizeDirectoryPath('docs')).toBe('/docs/')
    expect(normalizeDirectoryPath('/docs//nested///')).toBe('/docs/nested/')
    expect(normalizeDirectoryPath('\\docs\\nested\\')).toBe('/docs/nested/')
    expect(normalizeDirectoryPath('/')).toBe('/')
  })

  it('normalizes directory roots without trailing slash', () => {
    expect(normalizeDirectoryRoot('docs')).toBe('/docs')
    expect(normalizeDirectoryRoot('/docs//nested///')).toBe('/docs/nested')
    expect(normalizeDirectoryRoot('/')).toBe('/')
  })

  it('builds encrypted metadata and metadata file path', () => {
    const metadata = buildEncryptedDirectoryMetadata('aes-256-gcm')

    expect(metadata.version).toBe(1)
    expect(metadata.encrypted).toBe(true)
    expect(metadata.cipherSuite).toBe('aes-256-gcm')
    expect(new Date(metadata.createdAt).toString()).not.toBe('Invalid Date')

    expect(buildEncryptedDirectoryMetadataPath('/secure')).toBe(`/secure/${ENCRYPTED_DIRECTORY_MARKER}`)
    expect(buildEncryptedDirectoryMetadataPath('/')).toBe(`/${ENCRYPTED_DIRECTORY_MARKER}`)
  })

  it('identifies metadata filenames and encrypted root membership', () => {
    expect(isEncryptedDirectoryMetadataFileName(ENCRYPTED_DIRECTORY_MARKER)).toBe(true)
    expect(isEncryptedDirectoryMetadataFileName('notes.txt')).toBe(false)

    expect(isPathInsideEncryptedRoot('/secure/file.txt', '/secure')).toBe(true)
    expect(isPathInsideEncryptedRoot('/secure/nested/file.txt', '/secure')).toBe(true)
    expect(isPathInsideEncryptedRoot('/plain/file.txt', '/secure')).toBe(false)
    expect(isPathInsideEncryptedRoot('/anything', '/')).toBe(true)
  })

  it('resolves the nearest encrypted root for nested paths', () => {
    const roots = ['/', '/secure', '/secure/nested', '/archive']

    expect(resolveEncryptedRoot('/secure/nested/file.txt', roots)).toBe('/secure/nested')
    expect(resolveEncryptedRoot('/secure/other/file.txt', roots)).toBe('/secure')
    expect(resolveEncryptedRoot('/archive/file.txt', roots)).toBe('/archive')
    expect(resolveEncryptedRoot('/unknown/file.txt', ['/secure', '/archive'])).toBeNull()
  })

  it('caches encrypted directory passwords by normalized root', () => {
    setEncryptedDirectoryPassword('/secure/', 'pass-1')
    expect(getEncryptedDirectoryPassword('/secure')).toBe('pass-1')
    expect(getEncryptedDirectoryPassword('/secure/')).toBe('pass-1')

    setEncryptedDirectoryPassword('/secure', '')
    expect(getEncryptedDirectoryPassword('/secure')).toBe('')
  })
})
