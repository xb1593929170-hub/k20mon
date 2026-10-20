import 'element-plus/dist/index.css'
import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import App from './App.vue'
import client from './api/client'

// k20mon 的 api 层通过这个全局句柄拿 axios 实例，避免每个视图各自 import，
// 也让同一份 plugin 代码可以复用到别的宿主里。
globalThis.__K20MON_REQUEST__ = client

createApp(App)
  .use(ElementPlus, { locale: zhCn })
  .mount('#app')
