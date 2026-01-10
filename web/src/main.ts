import { createApp } from 'vue'
import App from './App.vue'
import { createPinia } from 'pinia'
import { routes } from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './assets/css/index.scss'
import { createRouter, createWebHistory } from 'vue-router'

const app = createApp(App)

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [...routes]
})

app.config.globalProperties.$t = function(key: string) { return key }

app.use(createPinia())
app.use(ElementPlus)
app.use(router)

app.mount('#app')