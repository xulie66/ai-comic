import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken } from '@/api/http'

// 根地址直接进入创作台，同时兼容旧 /ui/ 链接；静态资源仍由 /ui/assets 提供。
const router = createRouter({
  history: createWebHashHistory(location.pathname.startsWith('/ui') ? '/ui/' : '/'),
  routes: [
    { path: '/', redirect: '/projects' },
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    { path: '/projects', name: 'projects', component: () => import('@/views/ProjectsView.vue') },
    { path: '/wallet', name: 'wallet', component: () => import('@/views/WalletView.vue') },
    { path: '/project/:id', name: 'wizard', component: () => import('@/views/WizardView.vue') },
    { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue') }
  ]
})

router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!getToken()) return { name: 'login', query: { to: to.fullPath } }
  return true
})

export default router
