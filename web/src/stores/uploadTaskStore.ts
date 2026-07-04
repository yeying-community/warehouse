import { defineStore } from 'pinia'
import type { UploadTask } from '@/views/home/types'

export const useUploadTaskStore = defineStore('uploadTask', {
  state: () => ({
    tasks: [] as UploadTask[],
    dialogVisible: false,
    addSignal: 0
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
    }
  },
  actions: {
    openDialog() {
      this.dialogVisible = true
    },
    closeDialog() {
      this.dialogVisible = false
    },
    addTasks(tasks: UploadTask[]) {
      if (!tasks.length) return
      this.tasks = [...tasks, ...this.tasks]
      this.addSignal += 1
    },
    updateTask(task: UploadTask, patch: Partial<UploadTask>) {
      const index = this.tasks.findIndex(item => item.id === task.id)
      const updatedAt = Date.now()
      if (index === -1) {
        Object.assign(task, patch, { updatedAt })
        return
      }
      const current = this.tasks[index]
      this.tasks[index] = {
        ...current,
        ...patch,
        updatedAt
      }
    },
    clearFinished() {
      this.tasks = this.tasks.filter(task => task.status !== 'success')
    }
  }
})
