import { decrypt, encrypt } from '@yeying-community/web3-bs'
import type { CipherPasswordSource } from '@yeying-community/web3-bs'

const textEncoder = new TextEncoder()
const textDecoder = new TextDecoder()

export function encodeUtf8(input: string): Uint8Array {
  return textEncoder.encode(input)
}

export function decodeUtf8(input: Uint8Array): string {
  return textDecoder.decode(input)
}

export type CryptoPasswordOptions = {
  password?: string
  passwordSource?: CipherPasswordSource
  passwordContext?: string
  suite?: string
}

function normalizeCryptoOptions(passwordOrOptions?: string | CryptoPasswordOptions, suite?: string): CryptoPasswordOptions {
  if (typeof passwordOrOptions === 'object' && passwordOrOptions !== null) {
    return passwordOrOptions
  }
  return {
    password: passwordOrOptions || '',
    passwordSource: 'manual',
    suite
  }
}

export async function encryptBytes(
  data: Uint8Array,
  passwordOrOptions?: string | CryptoPasswordOptions,
  suite?: string
): Promise<string> {
  const options = normalizeCryptoOptions(passwordOrOptions, suite)
  return encrypt({
    data,
    password: options.password,
    passwordSource: options.passwordSource,
    passwordContext: options.passwordContext,
    suite: options.suite
  })
}

export async function decryptBytes(
  ciphertext: string,
  passwordOrOptions?: string | CryptoPasswordOptions
): Promise<Uint8Array> {
  const options = normalizeCryptoOptions(passwordOrOptions)
  return decrypt({
    ciphertext,
    password: options.password,
    passwordSource: options.passwordSource,
    passwordContext: options.passwordContext
  })
}

export async function encryptTextContent(
  content: string,
  passwordOrOptions?: string | CryptoPasswordOptions,
  suite?: string
): Promise<string> {
  return encryptBytes(encodeUtf8(content), passwordOrOptions, suite)
}

export async function decryptTextContent(
  ciphertext: string,
  passwordOrOptions?: string | CryptoPasswordOptions
): Promise<string> {
  const bytes = await decryptBytes(ciphertext, passwordOrOptions)
  return decodeUtf8(bytes)
}

export async function encryptFileContent(
  file: File,
  passwordOrOptions?: string | CryptoPasswordOptions,
  suite?: string
): Promise<Blob> {
  const bytes = new Uint8Array(await file.arrayBuffer())
  const ciphertext = await encryptBytes(bytes, passwordOrOptions, suite)
  return new Blob([ciphertext], { type: 'text/plain;charset=utf-8' })
}

export async function decryptBlobContent(
  blob: Blob,
  passwordOrOptions?: string | CryptoPasswordOptions
): Promise<Uint8Array> {
  const ciphertext = await blob.text()
  return decryptBytes(ciphertext, passwordOrOptions)
}
