<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAppStore } from '@/stores/app'
import { auth, renderPause, renderResume, renderStop } from '@/api/endpoints'
import { setToken } from '@/api/http'
import { applyTheme } from '@/theme'

const store = useAppStore()
const route = useRoute()
const router = useRouter()

const isLogin = computed(() => route.name === 'login')
const activeName = computed(() => String(route.name || ''))

/* ---------- 后台出图：不管在哪个页面都能看见、能暂停 ---------- */
const renderState = computed(() => store.render)
const renderVisible = computed(() => ['running', 'paused', 'stopped', 'done'].includes(renderState.value.state) && renderState.value.total > 0)
const renderLabel = computed(() => {
  const r = renderState.value
  if (r.state === 'running') return `后台生成中 ${r.done + r.failed}/${r.total}`
  if (r.state === 'paused') return r.autoPaused ? `已自动暂停 ${r.done + r.failed}/${r.total}` : `已暂停 ${r.done + r.failed}/${r.total}`
  if (r.state === 'stopped') return `已停止 ${r.done + r.failed}/${r.total}`
  return `生成完成 ${r.done}/${r.total}`
})
const renderPercent = computed(() => {
  const r = renderState.value
  if (!r.total) return 0
  return Math.min(100, Math.round(((r.done + r.failed) / r.total) * 100))
})

async function toggleRenderPause() {
  try {
    const res: any = renderState.value.state === 'running' ? await renderPause() : await renderResume()
    if (res?.status) store.render = { ...store.render, ...res.status }
    if (res?.message && res.success === false) ElMessage.info(res.message)
    if (res?.status?.state === 'running') void store.syncRender()
  } catch (err: any) {
    ElMessage.error(err?.message || '操作失败')
  }
}

async function stopRender() {
  try {
    const res: any = await renderStop()
    if (res?.status) store.render = { ...store.render, ...res.status }
    ElMessage.success('已停止后台生成（进行中的图会跑完）')
  } catch (err: any) {
    ElMessage.error(err?.message || '停止失败')
  }
}

function goRender() {
  const id = renderState.value.projectId || store.projects.activeId
  if (id) router.push({ path: `/project/${id}`, query: { step: 'render' } })
}

/* ---------- 创作入口 ---------- */
const creatingProject = ref(false)
const createModeOpen = ref(false)
const selectedCreateMode = ref<'short' | 'long'>('short')

function nextProjectName() {
  const items = store.projects.items || []
  const stamp = new Date()
  const hh = String(stamp.getHours()).padStart(2, '0')
  const mm = String(stamp.getMinutes()).padStart(2, '0')
  return `未命名 ${items.length + 1} · ${stamp.getMonth() + 1}-${stamp.getDate()} ${hh}:${mm}`
}

async function startCreate(mode: 'short' | 'long' = 'short') {
  if (creatingProject.value) return
  if (store.render.state === 'running' || store.render.state === 'paused') {
    try {
      await ElMessageBox.confirm(
        '还有后台出图没跑完。新建项目会自动暂停它（回到原项目点「继续」就能接着跑，已出的图不会丢）。',
        '先暂停后台出图？',
        { confirmButtonText: '新建并暂停', cancelButtonText: '先不新建', type: 'warning' }
      )
    } catch { return }
  }
  creatingProject.value = true
  try {
    const name = nextProjectName()
    const res: any = await store.createProject(name)
    const project: any = store.project || {}
    project.createMode = mode
    project.episodes = mode === 'long' ? [{ id: 'ep-1', title: '第一章', originalText: '', storyboard: null, status: 'draft' }] : []
    project.activeEpisodeId = mode === 'long' ? 'ep-1' : ''
    project.characterLibrary = []
    project.pageDensity = project.pageDensity || 'balanced'
    project.artStyle = project.artStyle || 'guoman'
    project.aspectRatio = project.aspectRatio || '9:16'
    project.targetPageCountManual = false
    store.project = project
    try { await store.saveNow() } catch { /* 存不上也能继续写 */ }
    const id = (res?.project && (res.project.id || res.project.item?.id)) || store.projects.activeId
    ElMessage.success(`已新建项目：${name}`)
    if (id) router.push({ path: `/project/${id}`, query: { step: 'import' } })
  } catch (err: any) {
    ElMessage.error(err?.message || '新建项目失败')
  } finally {
    creatingProject.value = false
  }
}

onMounted(async () => {
  applyTheme('mono')
  if (isLogin.value) return
  // ?nosse=1 只在自动化截图时用：SSE 长连接会让无头浏览器一直"有网络活动"，截不了图
  if (!new URLSearchParams(location.search).get('nosse')) store.bindEvents()
  try {
    await store.load()
    await store.loadMe()
    await store.syncRender()
  } catch (err: any) {
    if (err?.status === 401) router.replace({ name: 'login' })
    else ElMessage.error(err?.message || '加载失败')
  }
})

async function logout() {
  try { await auth.logout() } catch { /* 忽略 */ }
  setToken('')
  store.user = null
  router.replace({ name: 'login' })
}

const savedText = computed(() => {
  if (store.saving) return '保存中…'
  if (!store.lastSavedAt) return ''
  const s = Math.round((Date.now() - store.lastSavedAt) / 1000)
  return s < 5 ? '刚刚保存' : `${s} 秒前保存`
})
</script>

<template>
  <div v-if="isLogin" class="login-shell">
    <router-view />
  </div>

  <div v-else class="shell">
    <aside class="side">
      <div class="side-brand">
        <span class="logo">漫</span>
        <div class="side-brand-text">
          <strong>AI 漫画</strong>
          <small>创作台</small>
        </div>
      </div>

      <div class="side-group">工作区</div>
      <nav class="side-nav">
        <button class="side-item side-item-btn" :class="{ active: activeName === 'wizard' }" :disabled="creatingProject" @click="createModeOpen = true">
          <span class="ico">✎</span><span>创作</span>
          <span v-if="creatingProject" class="side-busy">…</span>
        </button>
        <router-link to="/projects" class="side-item" :class="{ active: activeName === 'projects' }">
          <span class="ico">◧</span><span>我的项目</span>
        </router-link>
        <router-link to="/wallet" class="side-item" :class="{ active: activeName === 'wallet' }">
          <span class="ico">◈</span><span>钱包 / 充值</span>
        </router-link>
      </nav>

      <div class="side-group">系统</div>
      <nav class="side-nav">
        <router-link to="/settings" class="side-item" :class="{ active: activeName === 'settings' }">
          <span class="ico">⚙</span><span>偏好设置</span>
        </router-link>
        <a v-if="store.user?.isAdmin" href="/admin/" target="_blank" class="side-item">
          <span class="ico">▤</span><span>管理后台</span>
        </a>
      </nav>

      <div class="side-block">
        <div class="side-label">当前项目</div>
        <div class="side-project" :title="store.activeProject?.absPath || ''">
          {{ store.activeProject?.name || '未选择' }}
        </div>
        <div class="side-stats">
          <span>{{ store.pages.length }} 页</span>
          <span>{{ store.characters.length }} 角色</span>
          <span>{{ store.scenes.length }} 场景</span>
        </div>
      </div>

      <div class="side-foot">
        <div class="side-user">
          <span>{{ store.user?.username || '' }}</span>
          <el-button text size="small" @click="logout">退出</el-button>
        </div>
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <div class="crumb">
          <router-link to="/projects">项目</router-link>
          <span v-if="store.activeProject">/ {{ store.activeProject.name }}</span>
        </div>
        <div class="topbar-right">
          <span v-if="!store.eventsOnline" class="offline-hint" title="SSE 长连接断了，浏览器会自动重连；这期间进度可能不准">
            实时进度重连中…
          </span>
          <div v-if="renderVisible" class="render-chip" :class="renderState.state" :title="renderState.lastError || '后台出图队列：切换页面也不会停'">
            <button class="rc-main" @click="goRender">
              <span class="rc-dot" />
              <span class="rc-text">{{ renderLabel }}</span>
              <span class="rc-bar"><i :style="{ width: renderPercent + '%' }" /></span>
            </button>
            <button v-if="renderState.state === 'running' || renderState.state === 'paused'" class="rc-btn" @click="toggleRenderPause">
              {{ renderState.state === 'running' ? '暂停' : '继续' }}
            </button>
            <button v-if="renderState.state !== 'done'" class="rc-btn" @click="stopRender">停止</button>
          </div>
          <span v-if="savedText" class="muted">{{ savedText }}</span>
          <el-button size="small" text @click="store.saveNow()">立即保存</el-button>
          <el-button size="small" text @click="store.load()">刷新</el-button>
        </div>
      </header>

      <section class="content">
        <router-view />
      </section>
    <el-dialog v-model="createModeOpen" title="开始创作" width="460px">
      <p class="muted" style="margin:0 0 14px">选择作品篇幅，短篇直接进入创作，长篇可按章节连续制作。</p>
      <el-radio-group v-model="selectedCreateMode" class="create-mode-options">
        <el-radio-button value="short">短篇</el-radio-button>
        <el-radio-button value="long">长篇 · 分章节</el-radio-button>
      </el-radio-group>
      <template #footer>
        <el-button @click="createModeOpen = false">取消</el-button>
        <el-button type="primary" :loading="creatingProject" @click="createModeOpen = false; startCreate(selectedCreateMode)">开始创作</el-button>
      </template>
    </el-dialog>
    </div>
  </div>
</template>

<style scoped>
.create-mode-options { display: grid; grid-template-columns: 1fr 1fr; width: 100%; }
.create-mode-options :deep(.el-radio-button) { width: 100%; }
.create-mode-options :deep(.el-radio-button__inner) { width: 100%; }
.shell { display: flex; min-height: 100vh; background: var(--page); }

/* ---------- 左侧导航（颜色全靠变量，深浅主题共用一套结构）---------- */
.side {
  width: 236px; flex: 0 0 236px;
  background: var(--side-bg);
  color: var(--side-text);
  border-right: 1px solid var(--side-border);
  display: flex; flex-direction: column;
  padding: 20px 14px 14px;
  position: sticky; top: 0; height: 100vh;
  transition: background-color .22s ease;
}
.side-brand { display: flex; align-items: center; gap: 10px; padding: 0 6px 6px; }
.logo {
  width: 32px; height: 32px; border-radius: 9px; display: grid; place-items: center;
  background: var(--accent); color: var(--on-accent);
  font-weight: 650; font-size: 14px; letter-spacing: .5px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, .16), 0 6px 14px -10px rgba(0, 0, 0, .7);
}
.side-brand-text { display: flex; flex-direction: column; line-height: 1.28; }
.side-brand-text strong { font-size: 14px; letter-spacing: -.15px; color: var(--side-strong); font-weight: 640; }
.side-brand-text small { color: var(--side-dim); font-size: 10.5px; letter-spacing: .9px; text-transform: uppercase; }

.side-nav { display: flex; flex-direction: column; gap: 1px; }
.side-item {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 11px; border-radius: 8px; color: var(--side-text);
  font-size: 13px; letter-spacing: .1px;
  transition: background .18s ease, color .18s ease;
}
/* 「创作」是个按钮（点一下直接新建项目），视觉上和导航项一致 */
.side-item-btn {
  width: 100%; border: 0; background: transparent; cursor: pointer;
  font-family: inherit; text-align: left;
}
.side-item-btn:disabled { opacity: .6; cursor: progress; }
.side-busy { margin-left: auto; font-size: 12px; }
.side-item:hover { background: var(--side-hover); color: var(--side-active-text); }
.side-item.active { background: var(--side-active); color: var(--side-active-text); font-weight: 600; }
.side-item .ico { width: 15px; text-align: center; opacity: .7; font-size: 12px; }
.side-item.active .ico { opacity: .95; }

.side-block {
  margin-top: 20px; padding: 12px 13px; border-radius: 10px;
  border: 1px solid var(--side-block-line);
  background: var(--side-block-bg);
}
.side-label { font-size: 10px; color: var(--side-dim); letter-spacing: 1px; text-transform: uppercase; margin-bottom: 6px; font-weight: 600; }
.side-project { font-size: 12.5px; font-weight: 620; color: var(--side-strong); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.side-stats { display: flex; gap: 10px; margin-top: 7px; font-size: 11px; color: var(--side-dim); flex-wrap: wrap; font-variant-numeric: tabular-nums; }

.shell{display:flex;min-height:100vh;background:var(--page)}
.side{width:236px;flex:0 0 236px;background:var(--side-bg);color:var(--side-text);border-right:1px solid var(--side-border);display:flex;flex-direction:column;padding:20px 14px 14px;position:sticky;top:0;height:100vh;transition:background-color .22s ease}
.side-brand{display:flex;align-items:center;gap:10px;padding:0 6px 6px}
.logo{width:32px;height:32px;border-radius:9px;display:grid;place-items:center;background:var(--accent);color:var(--on-accent);font-weight:650;font-size:14px;letter-spacing:.5px;box-shadow:inset 0 1px #ffffff29,0 6px 14px -10px #000000b3}
.side-brand-text{display:flex;flex-direction:column;line-height:1.28}
.side-brand-text strong{font-size:14px;letter-spacing:-.15px;color:var(--side-strong);font-weight:640}
.side-brand-text small{color:var(--side-dim);font-size:10.5px;letter-spacing:.9px;text-transform:uppercase}
.side-nav{display:flex;flex-direction:column;gap:1px}
.side-item{display:flex;align-items:center;gap:10px;padding:8px 11px;border-radius:8px;color:var(--side-text);font-size:13px;letter-spacing:.1px;transition:background .18s ease,color .18s ease}
.side-item-btn{width:100%;border:0;background:transparent;cursor:pointer;font-family:inherit;text-align:left}
.side-item-btn:disabled{opacity:.6;cursor:progress}
.side-busy{margin-left:auto;font-size:12px}
.side-item:hover{background:var(--side-hover);color:var(--side-active-text)}
.side-item.active{background:var(--side-active);color:var(--side-active-text);font-weight:600}
.side-item .ico{width:15px;text-align:center;opacity:.7;font-size:12px}
.side-item.active .ico{opacity:.95}
.side-block{margin-top:20px;padding:12px 13px;border-radius:10px;border:1px solid var(--side-block-line);background:var(--side-block-bg)}
.side-label{font-size:10px;color:var(--side-dim);letter-spacing:1px;text-transform:uppercase;margin-bottom:6px;font-weight:600}
.side-project{font-size:12.5px;font-weight:620;color:var(--side-strong);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.side-stats{display:flex;gap:10px;margin-top:7px;font-size:11px;color:var(--side-dim);flex-wrap:wrap;font-variant-numeric:tabular-nums}
.offline-hint{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;font-size:11.5px;color:var(--err);background:var(--surface-2);border:1px solid var(--line)}
.offline-hint:before{content:"";width:6px;height:6px;border-radius:50%;background:var(--err);animation:pulse-f9288bcc 1.4s ease-in-out infinite}
.side-foot{margin-top:auto;padding:12px 2px 0}
.side-user{display:flex;align-items:center;justify-content:space-between;font-size:11.5px;color:var(--side-dim)}
.side-user .el-button{color:var(--side-text)}
.side-user .el-button:hover{color:var(--side-active-text)}
.main{flex:1 1 auto;min-width:0;display:flex;flex-direction:column}
.topbar{display:flex;align-items:center;gap:12px;padding:12px 28px;background:color-mix(in srgb,var(--surface) 86%,transparent);-webkit-backdrop-filter:saturate(130%) blur(10px);backdrop-filter:saturate(130%) blur(10px);border-bottom:1px solid var(--line);position:sticky;top:0;z-index:15}
.crumb{font-size:12.5px;color:var(--text-3);letter-spacing:.2px}
.crumb a{color:var(--text-2)}
.crumb a:hover{color:var(--text)}
.topbar-right{margin-left:auto;display:flex;align-items:center;gap:10px;font-size:12.5px}
.render-chip{display:flex;align-items:center;gap:2px;padding:2px 2px 2px 0;border:1px solid var(--line);border-radius:999px;background:var(--card);box-shadow:var(--shadow-s);overflow:hidden}
.render-chip .rc-main{display:flex;align-items:center;gap:7px;padding:4px 10px 4px 9px;border:0;background:transparent;color:inherit;cursor:pointer;font:inherit}
.render-chip .rc-dot{width:7px;height:7px;border-radius:50%;background:var(--gold, #b08a3e);flex:none}
.render-chip.running .rc-dot{animation:rc-pulse-f9288bcc 1.4s ease-in-out infinite}
.render-chip.paused .rc-dot{background:#d08a2c}
.render-chip.stopped .rc-dot{background:#b0a79a}
.render-chip.done .rc-dot{background:#4f8f55}
.render-chip .rc-text{white-space:nowrap;color:var(--ink)}
.render-chip .rc-bar{width:54px;height:4px;border-radius:3px;background:var(--line);overflow:hidden;display:inline-block}
.render-chip .rc-bar i{display:block;height:100%;background:var(--gold, #b08a3e);transition:width .4s ease}
.render-chip .rc-btn{border:0;border-left:1px solid var(--line);background:transparent;color:var(--muted);padding:4px 9px;cursor:pointer;font:inherit;white-space:nowrap}
.render-chip .rc-btn:hover{color:var(--ink);background:var(--hover, rgba(0, 0, 0, .04))}
.content{padding:26px 28px 72px;max-width:1440px;width:100%}
.login-shell{min-height:100vh;background:var(--page)}
</style>
