import { defineStore } from 'pinia'
export const useLocaleStore = defineStore('locale', {
    state: () => ({
        locale: localStorage.getItem('i18nextLng') || 'zh-CN'
    }),
    actions: {
        setLocale(locale: string) {
            if (this.locale !== locale) {
                this.locale = locale
            }
        }
    }
})
