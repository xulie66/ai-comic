import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'

import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler, getToken, setToken } from './api/http'
import { applyTheme, readTheme } from './theme'
import './styles.css'

// 启动参数（先整份抓下来，避免后面改写地址栏时把参数弄丢）
const bootParams = new URLSearchParams(location.search)

// 支持 /ui/?token=xxx 深链登录（内部联调、跨端跳转用）。用完立刻从地址栏抹掉，不留痕迹。
const bootToken = bootParams.get('token')
if (bootToken) {
  setToken(bootToken)
  const url = new URL(location.href)
  url.searchParams.delete('token')
  const keepSearch = url.search ? url.search : ''
  history.replaceState(null, '', (url.pathname + keepSearch + (url.hash || '')) || '/ui/')
}

// token 换一次会话 Cookie：<img> 请求带不了 Authorization 头，没有 Cookie 就全是 401（图全破）。
// 正常登录时服务器已经种过 Cookie，这里只在深链进来时补一刀。
if (getToken()) {
  fetch('/api/auth/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + getToken() },
    body: JSON.stringify({ token: getToken() })
  }).catch(() => { /* 换不到就算了，接口请求还有 Bearer 兜底 */ })
}

// 主题必须在挂载前应用，否则会先闪一下默认配色。
// 也支持 /ui/?theme=indigo 这种直连（截图、分享配色方案时用）。
applyTheme((bootParams.get('theme') as any) || readTheme())

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })

// 任何请求 401 都直接回登录页，避免界面停在半残状态
setUnauthorizedHandler(() => {
  if (!getToken()) router.replace({ name: 'login' })
})

app.mount('#app')
