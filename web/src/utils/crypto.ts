import { decrypt, encrypt } from '@yeying-community/web3-bs'

const textEncoder = new TextEncoder()
const textDecoder = new TextDecoder()

export function encodeUtf8(input: string): Uint8Array {
  return textEncoder.encode(input)
}

export function decodeUtf8(input: Uint8Array): string {
  return textDecoder.decode(input)
}

export async function encryptBytes(data: Uint8Array, password: string, suite?: string): Promise<string> {
  return encrypt({
    data,
    password,
    suite
  })
}

export async function decryptBytes(ciphertext: string, password: string): Promise<Uint8Array> {
  return decrypt({
    ciphertext,
    password
  })
}

export async function encryptTextContent(content: string, password: string, suite?: string): Promise<string> {
  return encryptBytes(encodeUtf8(content), password, suite)
}

export async function decryptTextContent(ciphertext: string, password: string): Promise<string> {
  const bytes = await decryptBytes(ciphertext, password)
  return decodeUtf8(bytes)
}

export async function encryptFileContent(file: File, password: string, suite?: string): Promise<Blob> {
  const bytes = new Uint8Array(await file.arrayBuffer())
  const ciphertext = await encryptBytes(bytes, password, suite)
  return new Blob([ciphertext], { type: 'text/plain;charset=utf-8' })
}

export async function decryptBlobContent(blob: Blob, password: string): Promise<Uint8Array> {
  const ciphertext = await blob.text()
  return decryptBytes(ciphertext, password)
}
