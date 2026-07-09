export const ENCRYPTED_DIRECTORY_MARKER = '.warehouse-crypto.json'
const PASSWORD_CACHE_KEY = 'warehouse:encryptedDirectoryPasswords'

export interface EncryptedDirectoryMetadata {
  version: 1
  encrypted: true
  cipherSuite: string
  passwordSource?: EncryptedDirectoryPasswordSource
  createdAt: string
}

export type EncryptedDirectoryPasswordSource = 'manual' | 'wallet' | 'wallet+password'

function normalizePath(path: string): string {
  const raw = String(path || '/').trim().replace(/\\/g, '/')
  if (!raw || raw === '/') return '/'
  const withLeadingSlash = raw.startsWith('/') ? raw : `/${raw}`
  return withLeadingSlash.replace(/\/{2,}/g, '/').replace(/\/+$/, '') || '/'
}

export function normalizeDirectoryPath(path: string): string {
  const normalized = normalizePath(path)
  return normalized === '/' ? '/' : `${normalized}/`
}

export function normalizeDirectoryRoot(path: string): string {
  return normalizePath(path)
}

export const DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE = 'aes-256-gcm'
export const DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE: EncryptedDirectoryPasswordSource = 'wallet'

export function normalizeEncryptedDirectoryPasswordSource(
  value: unknown,
  fallback: EncryptedDirectoryPasswordSource = 'manual'
): EncryptedDirectoryPasswordSource {
  const source = String(value || '').trim()
  if (source === 'wallet' || source === 'wallet+password' || source === 'manual') {
    return source
  }
  return fallback
}

export function buildEncryptedDirectoryPasswordContext(rootPath: string): string {
  return normalizeDirectoryRoot(rootPath)
}

export function buildEncryptedDirectoryMetadata(
  cipherSuite = DEFAULT_ENCRYPTED_DIRECTORY_CIPHER_SUITE,
  passwordSource: EncryptedDirectoryPasswordSource = DEFAULT_ENCRYPTED_DIRECTORY_PASSWORD_SOURCE
): EncryptedDirectoryMetadata {
  return {
    version: 1,
    encrypted: true,
    cipherSuite,
    passwordSource,
    createdAt: new Date().toISOString()
  }
}

export function buildEncryptedDirectoryMetadataPath(directoryPath: string): string {
  const normalized = normalizeDirectoryPath(directoryPath)
  return normalized === '/'
    ? `/${ENCRYPTED_DIRECTORY_MARKER}`
    : `${normalized}${ENCRYPTED_DIRECTORY_MARKER}`
}

export function isEncryptedDirectoryMetadataFileName(name: string): boolean {
  return String(name || '').trim() === ENCRYPTED_DIRECTORY_MARKER
}

export function isPathInsideEncryptedRoot(path: string, encryptedRoot: string): boolean {
  const target = normalizeDirectoryRoot(path)
  const root = normalizeDirectoryRoot(encryptedRoot)
  return root === '/' ? true : target === root.slice(0, -1) || target.startsWith(root)
}

export function resolveEncryptedRoot(path: string, roots: string[]): string | null {
  const target = normalizeDirectoryRoot(path)
  const normalizedRoots = roots
    .map(item => normalizeDirectoryRoot(item))
    .sort((left, right) => right.length - left.length)

  for (const root of normalizedRoots) {
    if (root === '/') return '/'
    if (target === root || target.startsWith(root)) {
      return root
    }
  }

  return null
}

function readPasswordCache(): Record<string, string> {
  if (typeof sessionStorage === 'undefined') return {}
  try {
    const raw = sessionStorage.getItem(PASSWORD_CACHE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return {}
    return parsed as Record<string, string>
  } catch {
    return {}
  }
}

function writePasswordCache(cache: Record<string, string>): void {
  if (typeof sessionStorage === 'undefined') return
  sessionStorage.setItem(PASSWORD_CACHE_KEY, JSON.stringify(cache))
}

export function getEncryptedDirectoryPassword(rootPath: string): string {
  const cache = readPasswordCache()
  return cache[normalizeDirectoryRoot(rootPath)] || ''
}

export function setEncryptedDirectoryPassword(rootPath: string, password: string): void {
  const cache = readPasswordCache()
  const key = normalizeDirectoryRoot(rootPath)
  if (!password) {
    delete cache[key]
  } else {
    cache[key] = password
  }
  writePasswordCache(cache)
}
