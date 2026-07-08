export interface FileItem {
  name: string
  path: string
  isDir: boolean
  size: number
  modified: string
  encrypted?: boolean
  encryptedRoot?: string
}

export interface UploadItem {
  file: File
  relativePath: string
}

export type UploadTaskStatus = 'queued' | 'uploading' | 'success' | 'failed'

export type UploadTask = {
  id: string
  name: string
  relativePath: string
  size: number
  status: UploadTaskStatus
  progress: number
  error?: string
  createdAt: number
  updatedAt: number
  file?: File
  targetPath?: string
  isShared: boolean
  shareId?: string
  sharePath?: string
  encryptedRoot?: string
  cipherSuite?: string
}

export type CipherSuiteOption = {
  name: string
  description: string
  mode: 'hash' | 'symmetric'
}

export type DropEntry = {
  isFile: boolean
  isDirectory: boolean
  name: string
  fullPath?: string
  file?: (success: (file: File) => void, error?: (error: Error) => void) => void
  createReader?: () => {
    readEntries: (success: (entries: DropEntry[]) => void, error?: (error: Error) => void) => void
  }
}
