import localforage from 'localforage'
import { defineStore } from 'pinia'
import type { UploadTask } from '@/views/home/types'

const UPLOAD_TASK_STORAGE_KEY = 'warehouse.uploadTasks.v1'
const UPLOAD_PAYLOAD_STORAGE_PREFIX = 'warehouse.uploadPayload.'
const MAX_PERSISTED_TASKS = 100
const INTERRUPTED_UPLOAD_MESSAGE = '页面已刷新，浏览器已释放本地文件，请重新选择文件后再上传'
const ENCRYPTED_INTERRUPTED_UPLOAD_MESSAGE = '页面已刷新，可继续加密断点上传'

type PersistedUploadTask = Omit<UploadTask, 'file'>
type UploadPayloadRecord = {
  blob: Blob
  size: number
  type: string
  createdAt: number
}

const uploadTaskStorage = localforage.createInstance({
  name: 'warehouse',
  storeName: 'upload_tasks'
})

let persistTimer: ReturnType<typeof setTimeout> | null = null

function toPersistedTask(task: UploadTask): PersistedUploadTask {
  const { file: _file, ...persisted } = task
  return persisted
}

function normalizePersistedTask(task: PersistedUploadTask): UploadTask | null {
  if (!task || typeof task.id !== 'string' || typeof task.name !== 'string') return null
  const status = task.status === 'queued' || task.status === 'uploading' ? 'failed' : task.status
  const interrupted = task.status === 'queued' || task.status === 'uploading'
  const canResumeEncryptedPayload = Boolean(task.encryptedRoot && task.uploadPayloadStorageKey)
  return {
    ...task,
    status,
    progress: Number.isFinite(task.progress) ? task.progress : 0,
    size: Number.isFinite(task.size) ? task.size : 0,
    uploadedBytes: Number.isFinite(task.uploadedBytes) ? task.uploadedBytes : undefined,
    uploadSpeed: status === 'success' && Number.isFinite(task.uploadSpeed) ? task.uploadSpeed : undefined,
    error: interrupted ? (canResumeEncryptedPayload ? ENCRYPTED_INTERRUPTED_UPLOAD_MESSAGE : INTERRUPTED_UPLOAD_MESSAGE) : task.error,
    file: undefined
  }
}

async function savePersistedTasks(tasks: UploadTask[]) {
  const persisted = tasks.slice(0, MAX_PERSISTED_TASKS).map(toPersistedTask)
  await uploadTaskStorage.setItem(UPLOAD_TASK_STORAGE_KEY, persisted)
}

export const useUploadTaskStore = defineStore('uploadTask', {
  state: () => ({
    tasks: [] as UploadTask[],
    dialogVisible: false,
    addSignal: 0,
    restored: false
  }),
  getters: {
    summary(state) {
      return {
        total: state.tasks.length,
        queued: state.tasks.filter(task => task.status === 'queued').length,
        uploading: state.tasks.filter(task => task.status === 'uploading').length,
        success: state.tasks.filter(task => task.status === 'success').length,
        failed: state.tasks.filter(task => task.status === 'failed').length
      }
    },
    hasActiveTasks(state) {
      return state.tasks.some(task => task.status === 'queued' || task.status === 'uploading')
    }
  },
  actions: {
    async restorePersistedTasks() {
      if (this.restored) return
      this.restored = true
      try {
        const persisted = await uploadTaskStorage.getItem<PersistedUploadTask[]>(UPLOAD_TASK_STORAGE_KEY)
        if (!Array.isArray(persisted)) return
        this.tasks = persisted
          .map(normalizePersistedTask)
          .filter((task): task is UploadTask => task !== null)
          .slice(0, MAX_PERSISTED_TASKS)
        this.schedulePersist()
      } catch (error) {
        console.warn('恢复上传任务失败:', error)
      }
    },
    openDialog() {
      this.dialogVisible = true
    },
    closeDialog() {
      this.dialogVisible = false
    },
    addTasks(tasks: UploadTask[]) {
      if (!tasks.length) return
      this.tasks = [...tasks, ...this.tasks].slice(0, MAX_PERSISTED_TASKS)
      this.addSignal += 1
      this.schedulePersist()
    },
    updateTask(task: UploadTask, patch: Partial<UploadTask>) {
      const index = this.tasks.findIndex(item => item.id === task.id)
      const updatedAt = Date.now()
      if (index === -1) {
        Object.assign(task, patch, { updatedAt })
        this.schedulePersist()
        return
      }
      const current = this.tasks[index]
      const next = {
        ...current,
        ...patch,
        updatedAt
      }
      this.tasks[index] = next
      Object.assign(task, next)
      this.schedulePersist()
    },
    clearFinished() {
      const payloadKeys = this.tasks
        .filter(task => task.status === 'success')
        .map(task => task.uploadPayloadStorageKey)
        .filter((key): key is string => Boolean(key))
      this.tasks = this.tasks.filter(task => task.status !== 'success')
      for (const key of payloadKeys) {
        void this.removeUploadPayload(key)
      }
      this.schedulePersist()
    },
    async saveUploadPayload(taskId: string, blob: Blob) {
      const key = `${UPLOAD_PAYLOAD_STORAGE_PREFIX}${taskId}`
      const record: UploadPayloadRecord = {
        blob,
        size: blob.size,
        type: blob.type,
        createdAt: Date.now()
      }
      await uploadTaskStorage.setItem(key, record)
      return key
    },
    async getUploadPayload(key?: string) {
      if (!key) return null
      const payload = await uploadTaskStorage.getItem<UploadPayloadRecord | Blob>(key)
      if (payload instanceof Blob) return payload
      if (payload && payload.blob instanceof Blob) return payload.blob
      return null
    },
    async removeUploadPayload(key?: string) {
      if (!key) return
      await uploadTaskStorage.removeItem(key)
    },
    schedulePersist() {
      if (persistTimer !== null) {
        clearTimeout(persistTimer)
      }
      persistTimer = setTimeout(() => {
        persistTimer = null
        void this.flushPersistedTasks()
      }, 500)
    },
    async flushPersistedTasks() {
      try {
        await savePersistedTasks(this.tasks)
      } catch (error) {
        console.warn('保存上传任务失败:', error)
      }
    }
  }
})
