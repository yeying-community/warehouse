declare module 'element-plus' {
  import type { Plugin } from 'vue'

  type MessageType = 'success' | 'warning' | 'info' | 'error'

  interface MessageOptions {
    message: string
    type?: MessageType
    offset?: number
    showClose?: boolean
  }

  interface MessageBoxOptions {
    confirmButtonText?: string
    cancelButtonText?: string
    type?: MessageType
    closeOnClickModal?: boolean
    inputValue?: string
    inputType?: string
    inputPlaceholder?: string
    inputPattern?: RegExp
    inputErrorMessage?: string
  }

  interface MessageBoxPromptResult {
    value: string
  }

  export const ElLoading: Plugin

  export function ElMessage(options: MessageOptions): void

  export const ElMessageBox: {
    alert(message: string, title?: string, options?: MessageBoxOptions): Promise<void>
    confirm(message: string, title?: string, options?: MessageBoxOptions): Promise<void>
    prompt(message: string, title?: string, options?: MessageBoxOptions): Promise<MessageBoxPromptResult>
  }
}
