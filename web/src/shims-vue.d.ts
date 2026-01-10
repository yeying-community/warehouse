import { ComponentCustomProperties } from 'vue'
import { t } from '@yeying-community/yeying-i18n'

// 声明所有 .vue 文件的类型
declare module '*.vue' {
    import { DefineComponent } from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

// 扩展 Vue 的 ComponentCustomProperties 接口
declare module '@vue/runtime-core' {
    interface ComponentCustomProperties {
        $isMobile: boolean
        $t: (key: string) => string
    }
}
