import { defineStore } from 'pinia'
import { isMobile } from '@/utils/currency'

export const useStore = defineStore('useStore', {
  state: () => {
    return {
      isMobile: isMobile()
    }
  }
})