<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAppStore } from '@/stores/app'
import * as api from '@/api/endpoints'
import PromptDrawer from '@/components/PromptDrawer.vue'
import { fileUrl, downloadUrl, pagesZipUrl, projectExportUrl, zipPlan, ZIP_PARTS } from '@/api/endpoints'
import { stageLabel } from '@/api/events'
import { ART_STYLES, ASPECT_RATIOS, PAGE_DENSITY, densityOf, suggestPages, countChars } from '@/lib/createOptions'
import { move as moveInPlace, duplicate as duplicateInPlace, remove as removeInPlace, isFailedPage, pageImageOf } from '@/lib/pages'
import type { CharacterCard, Page, Panel, SceneCard } from '@/api/types'

const store = useAppStore()
const route = useRoute()
const router = useRouter()

const STEP_KEYS = ['import', 'cast', 'storyboard', 'render', 'export'] as const
type StepKey = (typeof STEP_KEYS)[number]

const STEP_LIST: { key: StepKey; title: string; hint: string }[] = [
  { key: 'import', title: '导入原文', hint: '贴正文 · 定页数' },
  { key: 'cast', title: '角色与场景', hint: '锁脸 · 锁空间' },
  { key: 'storyboard', title: '分镜', hint: '逐格编辑' },
  { key: 'render', title: '逐页出图', hint: '缺图 / 全部' },
  { key: 'export', title: '导出', hint: '整本打包' }
]

function stepDone(key: StepKey) {
  if (key === 'import') return store.hasStoryboard
  if (key === 'cast') {
    return store.characters.some((c: any) => c.referenceImagePath) && store.scenes.some((x: any) => x.refImagePath)
  }
  if (key === 'storyboard') return store.pages.length > 0
  if (key === 'render') return store.pages.some((p: Page) => pageImageOf(p))
  return false
}

const step = ref<StepKey>('import')

/* ---------- 生成进度反馈 ---------- */
const liveText = computed(() => {
  const ev = store.lastEvent
  if (!ev) return ''
  const bits = [stageLabel(ev.stage), ev.pageNumber ? `第 ${ev.pageNumber} 页` : '', ev.message || ''].filter(Boolean)
  return bits.join(' · ')
})
const castTab = ref<'characters' | 'scenes'>(
  new URLSearchParams(location.search).get('cast') === 'scenes' ? 'scenes' : 'characters'
)

// 复制图片在服务器上的绝对路径：老工作台的「复制图片」在 web 里没有剪贴板图像，
// 但「这张图在哪」这个信息同样有用（贴进 SSH / 脚本里直接用）
async function copyImagePath(page: any) {
  const path = pageImageOf(page)
  if (!path) return
  try {
    await navigator.clipboard.writeText(path)
    ElMessage.success('图片路径已复制')
  } catch {
    ElMessage.warning(path)
  }
  try {
    await api.copyImage(path)
  } catch {
    /* 服务端只记一条日志，失败不影响复制 */
  }
}

/* ---------- 生成队列（并发上限 2，超出的排队）---------- */
const MAX_CONC = 2
const queue = reactive({
  running: 0,
  pending: 0,
  done: 0,
  failed: 0,
  startedAt: 0,
  finishedAt: 0,
  label: '',
  active: {} as Record<string, 'queued' | 'running'>
})
const jobs: { key: string; label: string; run: () => Promise<boolean> }[] = []
// 失败的任务留一份，界面上可以一键重跑（不然只能一页页找回来点）
const failedJobs = ref<{ key: string; label: string; run: () => Promise<boolean> }[]>([])
let ticker: number | undefined
const nowTick = ref(Date.now())

const elapsedText = computed(() => {
  const from = queue.startedAt || nowTick.value
  const to = queue.finishedAt || nowTick.value
  const total = Math.max(0, Math.round((to - from) / 1000))
  const m = Math.floor(total / 60)
  return m ? `${m} 分 ${total % 60} 秒` : `${total} 秒`
})
const busy = computed(() => queue.running > 0 || queue.pending > 0)
const liveVisible = computed(() => busy.value || queue.done + queue.failed > 0)

/* ---------- 生成成本：每张耗时 / 本轮统计 / 剩余预估 ---------- */
// 服务端只在成功事件里给页码，不给耗时，所以耗时在前端量（call 前后取时间差就够用）。
const cost = reactive({ images: 0, totalMs: 0, minMs: 0, maxMs: 0 })
const pageMs = reactive<Record<string, number>>({})

function pageKey(page: Page) {
  return 'page:' + (page.page_index ?? page.index)
}
function resetCost() {
  cost.images = 0
  cost.totalMs = 0
  cost.minMs = 0
  cost.maxMs = 0
}
function noteCost(ms: number) {
  if (!(ms > 0)) return
  cost.images += 1
  cost.totalMs += ms
  if (!cost.minMs || ms < cost.minMs) cost.minMs = ms
  if (ms > cost.maxMs) cost.maxMs = ms
}
function fmtDur(ms?: number) {
  if (!ms || ms <= 0) return '—'
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)} 秒`
  const m = Math.floor(s / 60)
  return `${m} 分 ${Math.round(s % 60)} 秒`
}
const avgMs = computed(() => (cost.images ? cost.totalMs / cost.images : 0))
const costSentence = computed(() => {
  if (!cost.images) return ''
  const bits = [`本轮 ${cost.images} 张`, `总用时 ${fmtDur(cost.totalMs)}`, `平均 ${fmtDur(avgMs.value)}/张`]
  if (cost.maxMs) bits.push(`最快 ${fmtDur(cost.minMs)} · 最慢 ${fmtDur(cost.maxMs)}`)
  return bits.join(' · ')
})
const remainingPages = computed(() => (store.pages as Page[]).filter((p) => !pageImageOf(p)).length)

// 全本记录：每页出图耗时都写在页上（genSeconds），刷新后仍然在，可以当成本底账看
const historyCost = computed(() => {
  const secs = (store.pages as Page[]).map((p) => Number(p.genSeconds) || 0).filter((n) => n > 0)
  if (!secs.length) return null
  const total = secs.reduce((a, b) => a + b, 0)
  return {
    count: secs.length,
    total,
    avg: total / secs.length,
    min: Math.min(...secs),
    max: Math.max(...secs)
  }
})
const etaText = computed(() => {
  if (!cost.images || !remainingPages.value) return ''
  const queued = queue.pending + queue.running
  const todo = Math.max(0, remainingPages.value - queued)
  if (!todo) return '缺图的页都已经在队列里了'
  return `还剩 ${todo} 页，按当前速度约 ${fmtDur(avgMs.value * todo)}`
})

function stateOf(key: string) {
  const local = queue.active[key]
  if (local) return local
  // 页码状态由服务端队列给（切页面也一样准）
  if (key.startsWith('page:')) {
    const idx = Number(key.slice(5))
    const r = store.render
    if (r.current.includes(idx)) return 'running'
    if (r.queued.includes(idx)) return 'queued'
  }
  return ''
}

function enqueue(key: string, label: string, run: () => Promise<boolean>) {
  if (queue.active[key]) return
  if (!busy.value) {
    queue.done = 0
    queue.failed = 0
    queue.startedAt = Date.now()
    queue.finishedAt = 0
    failedJobs.value = []
    resetCost()
  }
  queue.active[key] = 'queued'
  queue.pending += 1
  jobs.push({ key, label, run })
  void pump()
}

// 重跑上一轮失败的任务
function retryFailed() {
  const list = failedJobs.value.slice()
  if (!list.length) return
  failedJobs.value = []
  list.forEach((job) => {
    if (job.key) delete queue.active[job.key]
    enqueue(job.key, job.label, job.run)
  })
}

async function pump() {
  while (queue.running < MAX_CONC && jobs.length) {
    const job = jobs.shift()!
    queue.running += 1
    queue.pending = Math.max(0, queue.pending - 1)
    queue.active[job.key] = 'running'
    queue.label = job.label
    try {
      const ok = await job.run()
      if (ok) {
        queue.done += 1
      } else {
        queue.failed += 1
        failedJobs.value.push(job)
      }
    } catch (err: any) {
      queue.failed += 1
      failedJobs.value.push(job)
      ElMessage.error(err?.message || '生成失败')
    } finally {
      delete queue.active[job.key]
      queue.running = Math.max(0, queue.running - 1)
      if (!queue.running && !jobs.length) {
        queue.finishedAt = Date.now()
        store.saveSoon()
      }
      void pump()
    }
  }
}

/* ---------- 页面级操作：上移 / 下移 / 复制 / 删除 ---------- */
// 具体逻辑在 src/lib/pages.ts（纯函数 + node 单测），这里只负责保存和提示
function movePage(page: Page, dir: -1 | 1) {
  const res = moveInPlace(store.pages, page, dir)
  if (!res.moved) return
  store.saveSoon()
  ElMessage.success(`已${dir < 0 ? '上移' : '下移'}到第 ${page.page_index} 页`)
}

function duplicatePage(page: Page) {
  const res = duplicateInPlace(store.pages, page)
  if (!res.created) return
  store.saveSoon()
  ElMessage.success(`已插入副本（第 ${res.created.page_index} 页，图片需要重新生成）`)
}

async function removePage(page: Page) {
  const label = `第 ${page.page_index ?? page.index} 页`
  try {
    await ElMessageBox.confirm(`删除${label}？后面的页会重新编号，已出的图跟着页走不会丢。`, '确认删除', {
      confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
    })
  } catch { return }
  const res = removeInPlace(store.pages, page)
  if (!res.removed) return
  await store.saveNow()
  ElMessage.success(`已删除${label}`)
}

function onPageCommand(cmd: string, page: Page) {
  if (cmd === 'up') movePage(page, -1)
  else if (cmd === 'down') movePage(page, 1)
  else if (cmd === 'copy') duplicatePage(page)
  else if (cmd === 'del') void removePage(page)
}

/* ---------- 项目加载 ---------- */
onMounted(async () => {
  ticker = window.setInterval(() => { nowTick.value = Date.now() }, 500)
  const id = String(route.params.id || '')
  try {
    if (id) await store.openProject(id)
    else await store.load()
    step.value = inferStep()
    applyPeekParam()
    syncAutoPages()
  } catch (err: any) {
    ElMessage.error(err?.message || '打开项目失败')
  }
})

// 联调/截图用：?peek=page:27 / ?peek=char:0 / ?peek=scene:0 直接打开对应的编辑抽屉
function applyPeekParam() {
  const params = new URLSearchParams(location.search)
  // ?step=import|storyboard|cast|render|export 直接落到某一步（分享链接、截图脚本用）
  const want = params.get('step')
  if (want && (STEP_KEYS as string[]).includes(want)) step.value = want as StepKey
  const peek = params.get('peek')
  if (!peek) return
  const m = /^(page|char|scene)(?::(\d+))?$/.exec(peek)
  if (!m) return
  const idx = Number(m[2] || 0)
  window.setTimeout(() => {
    if (m[1] === 'page') {
      const page = store.pages[idx]
      if (!page) return
      step.value = 'storyboard'
      openEditor('page', page)
    } else if (m[1] === 'char') {
      const card = store.characters[idx]
      if (!card) return
      step.value = 'cast'
      openEditor('character', card)
    } else {
      const scene = store.scenes[idx]
      if (!scene) return
      step.value = 'cast'
      openEditor('scene', scene)
    }
  }, 600)
}

onBeforeUnmount(() => { if (ticker) window.clearInterval(ticker) })

function inferStep(): StepKey {
  if (!store.hasStoryboard) return store.project?.originalText ? 'cast' : 'import'
  if (store.pages.some((p: Page) => pageImageOf(p))) return 'render'
  return 'storyboard'
}

/* ---------- 第 1 步：导入 ---------- */
const chars = computed(() => String(store.project?.originalText || '').length)
const busyStoryboard = ref(false)
const storyboardStartedAt = ref(0)
const storyboardTick = ref(Date.now())
let storyboardTimer: number | undefined

const storyboardElapsed = computed(() => {
  if (!busyStoryboard.value) return ''
  const total = Math.max(0, Math.round((storyboardTick.value - storyboardStartedAt.value) / 1000))
  const m = Math.floor(total / 60)
  return m ? `${m} 分 ${total % 60} 秒` : `${total} 秒`
})

/* ---------- 第 1 步：页数参考 ---------- */
const densityKey = computed({
  get: () => String((store.project as any).pageDensity || 'balanced'),
  set: (v: string) => { (store.project as any).pageDensity = v; store.saveSoon() }
})

/** 目标页数是"自动"还是"手填"：老工作台的口径——没填就按页数参考自动算 */
const pagesAuto = computed(() => !(store.project as any).targetPageCountManual)

function densityGuess(key: string) {
  return suggestPages(store.project.originalText, key)
}

/** 自动模式下把目标页数按当前正文和口径补上；手填过就不动 */
function syncAutoPages(force = false) {
  if (!force && !pagesAuto.value) return 0
  const guess = suggestPages(store.project.originalText, densityKey.value)
  if (!guess) return 0
  if (store.project.targetPageCount !== guess) {
    store.project.targetPageCount = guess
    ;(store.project as any).targetPageCountManual = false
    store.saveSoon()
  }
  return guess
}

function onPagesChange() {
  const v = Number(store.project.targetPageCount || 0)
  ;(store.project as any).targetPageCountManual = v > 0
  store.saveSoon()
}

function backToAutoPages() {
  ;(store.project as any).targetPageCountManual = false
  const guess = syncAutoPages(true)
  ElMessage.success(guess ? `已按「${densityOf(densityKey.value).label}」自动算成 ${guess} 页` : '贴正文后会自动算页数')
}

function applyDensity(key: string) {
  densityKey.value = key
  const guess = suggestPages(store.project.originalText, key)
  if (guess) {
    store.project.targetPageCount = guess
    ;(store.project as any).targetPageCountManual = false
    store.saveSoon()
    ElMessage.success(`按「${densityOf(key).label}」口径：${countChars(store.project.originalText)} 字 → ${guess} 页`)
  } else {
    ElMessage.info('先贴正文，才能按这个口径算页数')
  }
}

function useSuggestedPages() {
  const guess = syncAutoPages(true)
  if (!guess) { ElMessage.info('先贴正文才能算页数'); return }
  ;(store.project as any).targetPageCountManual = false
  store.saveSoon()
}

// 正文变长/换口径时，只要目标页数还是"自动"，就跟着重算
watch(
  [() => store.project.originalText, () => (store.project as any).pageDensity],
  () => { syncAutoPages() },
  { immediate: true }
)

function activeEpisode() {
  const project: any = store.project || {}
  return (project.episodes || []).find((item: any) => item.id === project.activeEpisodeId) || null
}

async function addEpisode() {
  const project: any = store.project || {}
  const episodes = project.episodes || (project.episodes = [])
  const current = activeEpisode()
  if (current) { current.originalText = String(project.originalText || ''); current.storyboard = project.storyboard || null; current.targetPageCount = project.targetPageCount; current.targetPageCountManual = project.targetPageCountManual }
  const index = episodes.length + 1
  const episode = { id: `ep-${Date.now()}`, number: index, title: `第${index}章`, originalText: '', storyboard: null, targetPageCount: undefined, status: 'draft' }
  episodes.push(episode)
  project.activeEpisodeId = episode.id
  project.originalText = ''
  project.storyboard = { pages: [] }
  project.targetPageCount = undefined
  project.targetPageCountManual = false
  await store.saveNow()
}

async function runStoryboard(force = false) {
  const project: any = store.project || {}
  const episode = activeEpisode()
  if (episode) {
    episode.originalText = String(project.originalText || '').trim()
    if (project.storyboard?.pages?.length) episode.storyboard = project.storyboard
  }
  const text = String(store.project?.originalText || '').trim()
  if (!text) { ElMessage.warning('先把小说正文贴进当前章节'); return }
  busyStoryboard.value = true
  storyboardStartedAt.value = Date.now()
  storyboardTick.value = Date.now()
  storyboardTimer = window.setInterval(() => { storyboardTick.value = Date.now() }, 500)
  try {
    const payload = {
      ...store.project,
      // 后端只认 targetPageCount（用户指定的目标页数），别把 pageCount 当成目标页数
      targetPageCount: store.project.targetPageCount || undefined,
      settings: store.settings,
      forceRestart: force,
      existingCharacters: !force && store.characters.length ? store.characters : undefined,
      canonicalCharacters: (store.project as any).createMode === 'long' && episode?.number > 1
        ? ((store.project as any).characterLibrary || []).filter((character: any) => character.referenceImagePath || ['male_lead', 'female_lead', 'antagonist'].includes(character.role))
        : undefined,
      episodeTitle: episode?.title || ''
    }
    if (payload.canonicalCharacters?.length) payload.extractCharacters = false
    const res = await api.generateStoryboard(payload)
    const sb = (res as any).storyboard || (res as any)
    if (!sb?.pages?.length) throw new Error('服务端没有返回分镜页')
    store.project.storyboard = sb
    if (sb.characterLibrary?.length) {
      store.project.characterLibrary = sb.characterLibrary
      ;(store.project as any).canonicalCharacters = sb.characterLibrary.map((character: any) => ({ ...character }))
    }
    if (sb.sceneLibrary?.length) store.project.sceneLibrary = sb.sceneLibrary
    if (episode) {
      episode.originalText = text
      episode.storyboard = sb
      episode.status = 'storyboard-ready'
    }
    await store.saveNow()
    ElMessage.success(`拆分完成：${sb.pages.length} 页`)
    step.value = 'cast'
  } catch (err: any) {
    ElMessage.error(err?.message || '拆分失败')
  } finally {
    busyStoryboard.value = false
    if (storyboardTimer) window.clearInterval(storyboardTimer)
  }
}

/* ---------- 第 2 步：角色与场景 ---------- */
// 同理：设定图也要按名字回当前 store 里找目标，别写进旧对象
function liveCharacterOf(card: CharacterCard): CharacterCard | null {
  const list = (store.characters || []) as CharacterCard[]
  return list.find((c) => String(c?.name) === String(card?.name)) || null
}
function liveSceneOf(scene: SceneCard): SceneCard | null {
  const list = (store.scenes || []) as SceneCard[]
  return list.find((s) => String(s?.name) === String(scene?.name)) || null
}

function genCharacter(card: CharacterCard) {
  const key = 'char:' + card.name
  enqueue(key, `角色设定图 · ${card.name}`, async () => {
    card.status = 'generating'
    const res = await api.generateCharacterReference({
      settings: store.settings,
      character: card,
      artStyle: store.project.artStyle,
      aspectRatio: '16:9',
      quality: store.project.quality,
      imageQuality: store.project.imageQuality
    })
    const target = liveCharacterOf(card)
    if (res?.imagePath || res?.referenceImagePath) {
      const path = res.imagePath || res.referenceImagePath
      if (target) {
        target.referenceImagePath = path
        target.status = 'done'
        store.saveSoon(300)
      }
      return true
    }
    const reason = res?.userMessage || res?.error || '生成失败'
    if (target) {
      target.status = 'failed'
      target.error = reason
      store.saveSoon(300)
    }
    store.pushEvent({ stage: 'image_generate_error', level: 'error', message: `角色设定图失败 · ${card.name}：${reason}` })
    return false
  })
}

function genScene(scene: SceneCard) {
  const key = 'scene:' + scene.name
  enqueue(key, `场景设定图 · ${scene.name}`, async () => {
    const res = await api.generateSceneReference({
      settings: store.settings,
      scene,
      artStyle: store.project.artStyle,
      quality: store.project.quality,
      imageQuality: store.project.imageQuality
    })
    const target = liveSceneOf(scene)
    if (res?.imagePath || res?.referenceImagePath) {
      const path = res.imagePath || res.referenceImagePath
      if (target) {
        target.refImagePath = path
        target.status = 'done'
        store.saveSoon(300)
      }
      return true
    }
    const reason = res?.userMessage || res?.error || '生成失败'
    if (target) {
      target.status = 'failed'
      target.error = reason
      store.saveSoon(300)
    }
    store.pushEvent({ stage: 'image_generate_error', level: 'error', message: `场景设定图失败 · ${scene.name}：${reason}` })
    return false
  })
}

async function removeCharacter(card: CharacterCard) {
  const lib = store.project.characterLibrary || []
  const idx = lib.findIndex((c: CharacterCard) => c.name === card.name)
  if (idx < 0) return
  try {
    await ElMessageBox.confirm(
      `删除角色「${card.name}」？只是从这个项目里去掉，已经生成的分镜页不受影响。`,
      '确认删除',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  lib.splice(idx, 1)
  await store.saveNow()
  ElMessage.success('已删除')
}

function genAllCharacters() {
  const list = store.characters as CharacterCard[]
  if (!list.length) { ElMessage.warning('还没有角色'); return }
  list.forEach((card) => { if (!queue.active['char:' + card.name]) genCharacter(card) })
}

function genAllScenes() {
  const list = store.scenes as SceneCard[]
  if (!list.length) { ElMessage.warning('场景库是空的，先点「按分镜重整场景名」'); return }
  const todo = list.filter((s) => !s.refImagePath)
  ;(todo.length ? todo : list).forEach((scene) => {
    if (!queue.active['scene:' + scene.name]) genScene(scene)
  })
}

const rebuilding = ref(false)
async function rebuildScenes() {
  rebuilding.value = true
  try {
    const res = await api.rebuildScenes()
    store.project.sceneLibrary = res.library || []
    ElMessage.success(`场景整理完成：${store.project.sceneLibrary.length} 个场景`)
    await store.load()
  } catch (err: any) {
    ElMessage.error(err?.message || '重整失败')
  } finally {
    rebuilding.value = false
  }
}

const extracting = ref<'' | 'characters' | 'scenes' | 'props'>('')
const extractResult = ref('')

async function extractAssets(kind: 'characters' | 'scenes' | 'props') {
  extracting.value = kind
  extractResult.value = ''
  try {
    const res: any = await api.extractAssets(kind)
    const items: any[] = res?.items || []
    let added = 0
    if (kind === 'characters') {
      const lib = (store.project.characterLibrary ||= [])
      items.forEach((item) => {
        if (!item?.name) return
        const hit = lib.find((c: CharacterCard) => c.name === item.name)
        if (hit) { Object.assign(hit, { ...item, ...(hit.referenceImagePath ? { referenceImagePath: hit.referenceImagePath } : {}) }); return }
        lib.push(item)
        added += 1
      })
    } else if (kind === 'scenes') {
      const lib = (store.project.sceneLibrary ||= [])
      items.forEach((item) => {
        if (!item?.name) return
        const hit = lib.find((x: SceneCard) => x.name === item.name)
        if (hit) { if (!hit.description && item.description) hit.description = item.description; return }
        lib.push(item)
        added += 1
      })
    } else {
      const lib = (store.project.propsLibrary ||= [])
      items.forEach((item) => {
        if (!item?.name) return
        if (!lib.find((x: any) => x.name === item.name)) { lib.push(item); added += 1 }
      })
    }
    // 同时写进服务器素材库，下次新项目可以直接导入
    try { await api.extractSave(kind, items) } catch { /* 素材库写失败不影响当前项目 */ }
    store.saveSoon()
    extractResult.value = `提取到 ${items.length} 个${kind === 'characters' ? '角色' : kind === 'scenes' ? '场景' : '道具'}，新增 ${added} 个`
    ElMessage.success(extractResult.value)
  } catch (err: any) {
    ElMessage.error(err?.message || '提取失败')
  } finally {
    extracting.value = ''
  }
}

/* ---------- 第 4 步：出图 ---------- */
// 生成任务跑完时，目标页可能已经不是发请求时那个对象了（切项目/切页面都会让 store 重新 load）。
// 所以统一按 page_index 回当前 store 里找，找不到就记录一条，别把结果写进没人引用的旧对象里。
function livePageOf(page: Page): Page | null {
  const idx = page?.page_index ?? page?.index
  const hit = (store.pages as Page[]).find((p) => (p.page_index ?? p.index) === idx)
  return (hit as Page) || null
}
/* 单页出图也走服务端队列：和批量同一套逻辑，切页面不会中断 */
function genPage(page: Page) {
  const idx = Number(page.page_index ?? page.index)
  if (!Number.isFinite(idx)) return
  void startServerRender('list', [idx])
}

const failedPages = computed(() => (store.pages as Page[]).filter((p) => isFailedPage(p)))

function retryFailedPages() {
  const list = failedPages.value
  if (!list.length) { ElMessage.info('没有失败页'); return }
  void startServerRender('list', list.map((p) => Number(p.page_index ?? p.index)))
}

function renderAll(doneOnlyMissing = true) {
  const pages = store.pages as Page[]
  const todo = doneOnlyMissing ? pages.filter((p) => !pageImageOf(p)) : pages
  if (!todo.length) { ElMessage.info('所有页都有图了，用「重画全部」可以整本重跑'); return }
  void startServerRender(doneOnlyMissing ? 'missing' : 'all')
}

/* ---------- 出图由服务端队列跑：切页面/关标签都不停 ---------- */
const serverRender = computed(() => store.render)
const serverBusy = computed(() => ['running', 'paused'].includes(serverRender.value.state))
const renderPct = computed(() => {
  const r = serverRender.value
  if (!r.total) return 0
  return Math.min(100, Math.round(((r.done + r.failed) / r.total) * 100))
})

async function startServerRender(mode: 'missing' | 'all' | 'list', indices?: number[]) {
  try {
    const wasRunning = store.render.state === 'running' || store.render.state === 'paused'
    const res: any = await api.renderStart({
      mode,
      indices,
      concurrency: 2,
      settings: store.settings,
      aspectRatio: store.project.aspectRatio,
      artStyle: store.project.artStyle,
      quality: store.project.quality,
      imageQuality: store.project.imageQuality
    })
    if (res?.status) store.render = { ...store.render, ...res.status }
    if (res?.success === false) {
      ElMessage.info(res.message || '没有要出的页')
      return false
    }
    // 已经在跑的时候再点，就是"追加到队列"，不是报错
    if (wasRunning) {
      if (res?.appended) ElMessage.success(`已加入队列：${res.appended} 页（后台继续跑）`)
      else ElMessage.info(res?.message || '这些页已经在队列里了')
    } else {
      ElMessage.success(`后台开始出图：${res?.status?.total ?? 0} 页（切到别的页面也会继续）`)
    }
    return true
  } catch (err: any) {
    ElMessage.error(err?.message || '启动失败')
    return false
  }
}

async function pauseServerRender() {
  try {
    const res: any = await api.renderPause()
    if (res?.status) store.render = { ...store.render, ...res.status }
    ElMessage.success('已暂停（正在跑的会先跑完）')
  } catch (err: any) { ElMessage.error(err?.message || '暂停失败') }
}

async function resumeServerRender() {
  try {
    const res: any = await api.renderResume()
    if (res?.status) store.render = { ...store.render, ...res.status }
    ElMessage.success('已继续')
  } catch (err: any) { ElMessage.error(err?.message || '继续失败') }
}

async function stopServerRender() {
  try {
    const res: any = await api.renderStop()
    if (res?.status) store.render = { ...store.render, ...res.status }
    ElMessage.success('已停止后台生成')
  } catch (err: any) { ElMessage.error(err?.message || '停止失败') }
}

/* ---------- 预览 ---------- */
const previewOpen = ref(false)
const previewPage = ref<Page | null>(null)
function openPreview(page: Page) {
  previewPage.value = page
  previewOpen.value = true
}
const zoomOpen = ref(false)
const zoomSrc = ref('')
function zoom(src?: string) {
  if (!src) return
  zoomSrc.value = fileUrl(src)
  zoomOpen.value = true
}

watch(() => route.params.id, async (id) => {
  if (!id) return
  try { await store.openProject(String(id)) } catch { /* 忽略 */ }
  syncAutoPages()
})

function fmt(v: any) {
  return v == null || v === '' ? '—' : String(v)
}

/** 分镜横条里一格显示一行字：场景 / 画面 / 旁白 / 对白，按有内容的部分拼 */
function panelBrief(panel: Panel) {
  const bits: string[] = []
  const scene = panel.scene || panel.scene_raw
  if (scene) bits.push(`场景：${scene}`)
  const action = panel.action || panel.visual_prompt
  if (action) bits.push(`画面：${action}`)
  const narration = panel.narration_text || panel.text || panel.text_overlay
  if (narration) bits.push(`文字：${narration}`)
  if (panel.dialogue_text) bits.push(`对白：${panel.dialogue_text}`)
  return bits.join('　·　')
}

/* ---------- 提示词编辑抽屉 ---------- */
const editor = reactive<{ open: boolean; kind: 'character' | 'page' | 'scene'; target: any }>({
  open: false,
  kind: 'character',
  target: null
})

function openEditor(kind: 'character' | 'page' | 'scene', target: any) {
  if (!target) return
  editor.kind = kind
  editor.target = target
  editor.open = true
}

function onEditorSave() {
  store.saveSoon()
}

function onEditorGenerate(kind: string, draft: any) {
  if (kind === 'character') genCharacter(draft as CharacterCard)
  else if (kind === 'scene') genScene(draft as SceneCard)
  else genPage(draft as Page)
}

/* ---------- 第 5 步：整本导出 ---------- */
const exporting = ref(false)
const exportResult = ref('')

// 打包内容勾选：默认「整页图 + 分格文案 + 项目 JSON」，设定图体积大，按需勾
const zipParts = reactive<Record<string, boolean>>({
  pages: true, script: true, project: true, refs: false, panels: false
})
const chosenParts = computed(() => ZIP_PARTS.filter((p) => zipParts[p.key]).map((p) => p.key))
const zipUrl = computed(() => pagesZipUrl(chosenParts.value))

const plan = ref<any>(null)
const planLoading = ref(false)
let planTimer: number | undefined
async function refreshPlan() {
  if (!chosenParts.value.length) { plan.value = null; return }
  planLoading.value = true
  try { plan.value = await zipPlan(chosenParts.value) } catch { plan.value = null } finally { planLoading.value = false }
}
watch(chosenParts, () => {
  window.clearTimeout(planTimer)
  planTimer = window.setTimeout(() => { void refreshPlan() }, 280)
})
// 切到「导出」这一步时才去问服务端（省一次请求，也避免项目没加载时报错）
watch(step, (value) => { if (value === 'export') void refreshPlan() })
const planMb = computed(() => {
  const bytes = Number(plan.value?.bytes || 0)
  if (!bytes) return ''
  const mb = bytes / 1048576
  return mb >= 1024 ? `${(mb / 1024).toFixed(2)} GB` : `${mb.toFixed(1)} MB`
})
const planBreakdown = computed(() => {
  const c = plan.value?.counts
  if (!c) return ''
  const bits = [`整页图 ${c.pages}`]
  if (zipParts.script) bits.push(`文案 ${c.script}`)
  if (zipParts.refs) bits.push(`设定图 ${c.refs}`)
  if (zipParts.panels) bits.push(`分格图 ${c.panels}`)
  if (zipParts.project) bits.push('项目 JSON')
  return bits.join(' · ')
})
const planMissing = computed(() => {
  const c = plan.value?.counts
  if (!c) return ''
  const bits: string[] = []
  if (c.missingPages) bits.push(`整页图缺 ${c.missingPages} 张`)
  if (c.missingRefs) bits.push(`设定图缺 ${c.missingRefs} 张`)
  if (c.missingPanels) bits.push(`分格图缺 ${c.missingPanels} 张`)
  return bits.join('，')
})

async function exportAll() {
  const pages = (store.pages as Page[])
    .filter((p) => pageImageOf(p))
    .map((p) => ({ page_index: p.page_index ?? p.index, page_image_url: pageImageOf(p) }))
  if (!pages.length) { ElMessage.warning('还没有出图，先去第 4 步'); return }
  exporting.value = true
  exportResult.value = ''
  try {
    const res = await api.exportAllPages({ pages })
    exportResult.value = `已导出 ${res.files?.length || 0} 张到 ${res.directory}`
    ElMessage.success('导出完成')
  } catch (err: any) {
    ElMessage.error(err?.message || '导出失败')
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div v-if="!store.ready" class="panel">
    <p class="panel-sub">正在加载项目…</p>
  </div>

  <div v-else>
    <div class="wz-head">
      <div class="wz-head-left">
        <h2>{{ store.activeProject?.name || '未命名项目' }}</h2>
        <span v-if="store.activeProject?.active" class="tag ok">当前项目</span>
        <span class="muted">已出图 {{ store.pages.filter((p: Page) => pageImageOf(p)).length }} / {{ store.pages.length }} 页</span>
      </div>
      <div class="wz-head-right">
        <el-button size="small" tag="a" :href="projectExportUrl" title="把这个项目的 JSON 下载下来（备份/换机器）">导出项目</el-button>
        <el-button size="small" @click="router.push('/projects')">项目列表</el-button>
      </div>
    </div>

    <div class="steps">
      <div
        v-for="(item, i) in STEP_LIST"
        :key="item.key"
        class="step"
        :class="{ active: step === item.key, done: stepDone(item.key) && step !== item.key }"
        @click="step = item.key"
      >
        <span class="no">{{ i + 1 }}</span>
        <span>{{ item.title }}</span>
        <span class="hint">{{ item.hint }}</span>
      </div>
    </div>

    <div v-if="liveVisible" class="gen-live">
      <b>{{ busy ? '生成中' : '本次生成结束' }}</b>
      <span>完成 {{ queue.done }} · 失败 {{ queue.failed }} · 排队 {{ queue.pending }}</span>
      <span class="muted">{{ busy ? queue.label : '用时 ' + elapsedText }}</span>
      <el-progress
        :percentage="Math.round(((queue.done + queue.failed) / Math.max(1, queue.done + queue.failed + queue.pending + queue.running)) * 100)"
        :stroke-width="8"
        style="width: 220px"
      />
      <span v-if="costSentence" class="live-msg">{{ costSentence }}</span>
      <span v-else-if="liveText" class="live-msg">{{ liveText }}</span>
      <el-button v-if="failedJobs.length" size="small" type="primary" plain @click="retryFailed">
        重跑失败的 {{ failedJobs.length }} 项
      </el-button>
    </div>

    <!-- 第 1 步 -->
    <section v-if="step === 'import'" class="panel">
      <h3 class="panel-title">{{ (store.project as any).createMode === 'long' ? '长篇分集创作' : '第一步：贴原文，定页数' }}</h3>
      <p class="panel-sub">{{ (store.project as any).createMode === 'long' ? '每章独立生成分镜，全书角色档案固定复用，后续章节不重新设计主角。' : '原文越长、页数越多，拆分时间越久（每段几秒到几十秒）。拆分结果会自动保存。' }}</p>
      <div v-if="(store.project as any).createMode === 'long'" class="episode-strip">
        <el-select
          :model-value="(store.project as any).activeEpisodeId"
          style="min-width: 180px"
          @change="async (id: string) => { const p: any = store.project; const old = (p.episodes || []).find((e: any) => e.id === p.activeEpisodeId); if (old) { old.originalText = p.originalText; old.storyboard = p.storyboard; old.targetPageCount = p.targetPageCount; old.targetPageCountManual = p.targetPageCountManual }; const next = p.episodes.find((e: any) => e.id === id); p.activeEpisodeId = id; p.originalText = next.originalText || ''; p.storyboard = next.storyboard || { pages: [] }; p.targetPageCount = next.targetPageCount; p.targetPageCountManual = Boolean(next.targetPageCountManual); await store.saveNow() }"
        >
          <el-option v-for="(ep, index) in (store.project as any).episodes" :key="ep.id" :value="ep.id" :label="ep.title || `第${index + 1}章`" />
        </el-select>
        <el-button @click="addEpisode">＋ 新增章节</el-button>
        <span class="muted">固定角色 {{ ((store.project as any).canonicalCharacters || []).length }} 位</span>
      </div>
      <el-input
        v-model="store.project.originalText"
        type="textarea"
        :rows="14"
        :placeholder="(store.project as any).createMode === 'long' ? '粘贴当前章节正文…' : '把小说正文粘贴到这里…'"
        @input="store.saveSoon()"
      />
      <div class="row" style="margin-top: 10px">
        <span class="muted">{{ chars }} 字</span>
        <div class="grow" />
        <span class="muted">目标页数</span>
        <el-input-number
          v-model="store.project.targetPageCount"
          :min="1"
          :max="200"
          size="default"
          @change="onPagesChange"
        />
        <span v-if="pagesAuto" class="muted pages-auto">按参考自动</span>
        <el-button v-else size="small" text @click="backToAutoPages">改回自动</el-button>
        <el-select v-model="store.project.aspectRatio" style="width: 170px">
          <el-option v-for="[v, l] in ASPECT_RATIOS" :key="v" :value="v" :label="l" />
        </el-select>
        <el-select v-model="store.project.artStyle" style="width: 170px">
          <el-option v-for="[v, l] in ART_STYLES" :key="v" :value="v" :label="l" />
        </el-select>
      </div>
      <!-- 页数参考：按正文字数算建议页数，目标页数留空就自动用它 -->
      <div class="density-row">
        <span class="muted">页数参考</span>
        <button
          v-for="d in PAGE_DENSITY"
          :key="d.key"
          class="density-chip"
          :class="{ on: densityKey === d.key }"
          @click="applyDensity(d.key)"
        >
          {{ d.label }} · {{ d.hint }}<template v-if="densityGuess(d.key)"> · {{ densityGuess(d.key) }} 页</template>
        </button>
        <span class="muted">
          {{ pagesAuto ? '目标页数没填，按这个口径自动算' : '目标页数是你手填的，点一下上面的参考会重算' }}
        </span>
        <el-button
          size="small"
          text
          :disabled="!densityGuess(densityKey)"
          @click="useSuggestedPages"
        >
          用参考页数
        </el-button>
      </div>
      <div class="row" style="margin-top: 14px">
        <el-button v-if="(store.project as any).createMode === 'long'" size="small" @click="addEpisode">＋ 新章节</el-button>
        <el-button type="primary" :loading="busyStoryboard" @click="runStoryboard(false)">
          {{ busyStoryboard ? '正在拆分… ' + storyboardElapsed : '拆分漫画页' }}
        </el-button>
        <el-button v-if="store.hasStoryboard" :disabled="busyStoryboard" @click="runStoryboard(true)">
          强制重新拆分
        </el-button>
        <span v-if="busyStoryboard" class="muted">拆分按段进行，完成后会自动保存分镜，请稍候</span>
      </div>
    </section>

    <!-- 第 2 步 -->
    <section v-if="step === 'cast'" class="panel">
      <h3 class="panel-title">第二步：把形象定下来</h3>
      <p class="panel-sub">
        角色设定图锁脸，场景设定图锁空间结构。出图时会把「本页出场的角色 + 本页场景」的设定图一起发给模型。
      </p>
      <el-tabs v-model="castTab">
        <el-tab-pane label="角色" name="characters">
          <div class="row" style="margin-bottom: 12px">
            <el-button type="primary" @click="genAllCharacters">全部生成设定图</el-button>
            <el-button :loading="extracting === 'characters'" @click="extractAssets('characters')">从原文提取角色</el-button>
            <el-button :loading="extracting === 'props'" @click="extractAssets('props')">提取道具</el-button>
            <span class="muted">{{ store.characters.filter((c: any) => c.referenceImagePath).length }} / {{ store.characters.length }} 已有设定图</span>
          </div>
          <div v-if="!store.characters.length" class="empty">还没有角色，先回第 1 步拆分一次。</div>
          <div v-else class="grid grid-cards">
            <article
              v-for="card in store.characters"
              :key="card.name"
              class="tile"
              :class="{ 'is-done': card.referenceImagePath }"
            >
              <div class="tile-thumb">
                <img
                  v-if="card.referenceImagePath"
                  :src="fileUrl(card.referenceImagePath)"
                  :alt="card.name"
                  loading="lazy"
                  @dblclick="zoom(card.referenceImagePath)"
                />
                <span v-else>无设定图</span>
              </div>
              <div class="tile-body">
                <div class="tile-title">
                  <strong>{{ card.name }}</strong>
                  <span class="tag ghost">{{ card.role || '角色' }}</span>
                </div>
                <p class="tile-meta">{{ [card.gender, card.age].filter(Boolean).join(' · ') || '性别/年龄未定' }}</p>
                <p class="tile-desc">{{ card.appearance || card.identity || '暂无外貌描述' }}</p>
              </div>
              <div class="tile-foot">
                <el-button
                  size="small"
                  :type="card.referenceImagePath ? 'default' : 'primary'"
                  :disabled="stateOf('char:' + card.name) !== ''"
                  @click="genCharacter(card)"
                >
                  {{ stateOf('char:' + card.name) === 'running' ? '生成中…' : stateOf('char:' + card.name) === 'queued' ? '排队中…' : card.referenceImagePath ? '重做' : '生成设定图' }}
                </el-button>
                <el-button size="small" text @click="openEditor('character', card)">编辑提示词</el-button>
                <el-button size="small" text type="danger" @click="removeCharacter(card)">删除</el-button>
              </div>
            </article>
          </div>
        </el-tab-pane>

        <el-tab-pane label="场景" name="scenes">
          <div class="row" style="margin-bottom: 12px; flex-wrap: wrap">
            <el-button type="primary" @click="genAllScenes">全部生成场景图</el-button>
            <el-button :loading="rebuilding" @click="rebuildScenes">按分镜重整场景名</el-button>
            <el-button :loading="extracting === 'scenes'" @click="extractAssets('scenes')">从原文提取场景</el-button>
            <span class="muted">{{ store.scenes.filter((s: any) => s.refImagePath).length }} / {{ store.scenes.length }} 已有场景图</span>
          </div>
          <div v-if="!store.scenes.length" class="empty">
            还没有场景。点「按分镜重整场景名」，把现有分镜里的场景（例如「高中教室 / 高三教室」）归并成规范清单。
          </div>
          <div v-else class="grid grid-cards">
            <article
              v-for="scene in store.scenes"
              :key="scene.name"
              class="tile"
              :class="{ 'is-done': scene.refImagePath }"
            >
              <div class="tile-thumb">
                <img
                  v-if="scene.refImagePath"
                  :src="fileUrl(scene.refImagePath)"
                  :alt="scene.name"
                  loading="lazy"
                  @dblclick="zoom(scene.refImagePath)"
                />
                <span v-else>无场景图</span>
              </div>
              <div class="tile-body">
                <div class="tile-title">
                  <strong>{{ scene.name }}</strong>
                  <span v-if="scene.count" class="tag">{{ scene.count }} 处</span>
                </div>
                <p class="tile-meta">{{ [scene.mood, scene.time_of_day].filter(Boolean).join(' · ') || '氛围未定' }}</p>
                <p class="tile-desc">{{ scene.description || '暂无场景描述' }}</p>
                <p v-if="scene.error" class="tile-err">{{ scene.error }}</p>
              </div>
              <div class="tile-foot">
                <el-button
                  size="small"
                  :type="scene.refImagePath ? 'default' : 'primary'"
                  :disabled="stateOf('scene:' + scene.name) !== ''"
                  @click="genScene(scene)"
                >
                  {{ stateOf('scene:' + scene.name) === 'running' ? '生成中…' : stateOf('scene:' + scene.name) === 'queued' ? '排队中…' : scene.refImagePath ? '重画' : '生成场景图' }}
                </el-button>
                <el-button size="small" text @click="openEditor('scene', scene)">编辑提示词</el-button>
              </div>
            </article>
          </div>
        </el-tab-pane>
      </el-tabs>
    </section>

    <!-- 第 3 步 -->
    <section v-if="step === 'storyboard'" class="panel">
      <h3 class="panel-title">
        第三步：看分镜
        <span class="tag ghost">{{ store.pages.length }} 页</span>
      </h3>
      <p class="panel-sub">点任意一页看每格的场景、旁白、对白；有问题回第 1 步重拆，或者在提示词抽屉里逐格改（含安全改写）。</p>
      <div v-if="!store.pages.length" class="empty">还没有分镜，先回第 1 步拆分。</div>
      <!-- 分镜只看文字：一页一条横条，不掺图 -->
      <div v-else class="sb-list">
        <article v-for="page in store.pages" :key="page.page_index ?? page.index" class="sb-row">
          <div class="sb-no">
            <span>第</span>
            <strong>{{ page.page_index ?? page.index }}</strong>
            <span>页</span>
          </div>
          <div class="sb-main">
            <div class="sb-head">
              <span class="tag ghost">{{ page.panels?.length || 0 }} 格</span>
              <span v-if="page.scene" class="sb-scene">场景：{{ fmt(page.scene) }}</span>
              <span v-if="page.layout" class="sb-layout">{{ page.layout }}</span>
            </div>
            <p class="sb-core">{{ page.page_core || page.summary || '（没有简介）' }}</p>
            <ol v-if="page.panels?.length" class="sb-panels">
              <li v-for="(panel, pi) in page.panels" :key="pi">
                <b>第 {{ pi + 1 }} 格</b>
                <span>{{ panelBrief(panel) || '（空白格）' }}</span>
              </li>
            </ol>
            <p v-else class="sb-empty">这一页还没有分格。</p>
          </div>
          <div class="sb-actions">
            <el-button size="small" @click="openPreview(page)">看细节</el-button>
            <el-button size="small" type="primary" plain @click="openEditor('page', page)">编辑分镜</el-button>
            <el-dropdown trigger="click" @command="(cmd: string) => onPageCommand(cmd, page)">
              <el-button size="small" text>更多 ▾</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="up" :disabled="store.pages.indexOf(page) === 0">上移一页</el-dropdown-item>
                  <el-dropdown-item command="down" :disabled="store.pages.indexOf(page) === store.pages.length - 1">下移一页</el-dropdown-item>
                  <el-dropdown-item command="copy">复制这一页</el-dropdown-item>
                  <el-dropdown-item command="del" divided>删除这一页</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </article>
      </div>
    </section>

    <!-- 第 4 步 -->
    <section v-if="step === 'render'" class="panel">
      <h3 class="panel-title">第四步：逐页出图</h3>
      <p class="panel-sub">
        点了开始就在服务器后台跑：切到项目列表、设置页、甚至关掉标签页都不影响（同时最多 {{ serverRender.concurrency }} 张，其余排队）。
        只有「暂停」或接口报错才会停。
      </p>
      <div class="row" style="margin-bottom: 12px">
        <el-button type="primary" @click="renderAll(true)">生成缺图的页</el-button>
        <el-button @click="renderAll(false)">全部重画</el-button>
        <el-button v-if="failedPages.length" type="danger" plain @click="retryFailedPages">
          重画失败页（{{ failedPages.length }}）
        </el-button>
        <span class="muted">{{ store.pages.filter((p: Page) => pageImageOf(p)).length }} / {{ store.pages.length }} 已完成</span>
      </div>

      <!-- 后台队列状态：这里的数字全部来自服务端，换个页面回来也是同一份 -->
      <div v-if="serverRender.total" class="render-bar" :class="serverRender.state">
        <div class="rb-head">
          <span class="rb-dot" />
          <strong>
            {{ serverRender.state === 'running' ? '后台生成中' : serverRender.state === 'paused'
              ? (serverRender.autoPaused ? '接口报错，已自动暂停' : '已暂停')
              : serverRender.state === 'stopped' ? '已停止' : '已完成' }}
          </strong>
          <span class="muted">
            {{ serverRender.done + serverRender.failed }} / {{ serverRender.total }}
            · 剩 {{ serverRender.remaining + serverRender.active }} 页
            <template v-if="serverRender.elapsedSeconds"> · 已用 {{ fmtDur(serverRender.elapsedSeconds * 1000) }}</template>
            <template v-if="serverRender.avgSeconds"> · 平均 {{ fmtDur(serverRender.avgSeconds * 1000) }}/张</template>
            <template v-if="serverRender.active"> · 正在跑第 {{ serverRender.current.join('、') }} 页</template>
          </span>
          <div class="grow" />
          <el-button v-if="serverRender.state === 'running'" size="small" @click="pauseServerRender">暂停</el-button>
          <el-button v-if="serverRender.state === 'paused'" size="small" type="primary" @click="resumeServerRender">继续</el-button>
          <el-button v-if="serverRender.state !== 'done'" size="small" text @click="stopServerRender">停止</el-button>
        </div>
        <div class="rb-track"><i :style="{ width: renderPct + '%' }" /></div>
        <div v-if="serverRender.lastError && serverRender.state === 'paused'" class="rb-error">
          最后一次错误：{{ serverRender.lastError }}（处理完点「继续」接着跑，已出的图不会重画）
        </div>
      </div>
      <!-- 成本：每张耗时都能量出来，就别让人靠猜「还要多久」 -->
      <div v-if="costSentence || historyCost" class="cost-bar">
        <div v-if="costSentence" class="cost-main">{{ costSentence }}</div>
        <div v-if="etaText" class="cost-sub">{{ etaText }}</div>
        <div v-if="historyCost" class="cost-sub">
          全本记录：{{ historyCost.count }} 张有耗时 · 累计 {{ fmtDur(historyCost.total * 1000) }} ·
          平均 {{ fmtDur(historyCost.avg * 1000) }}/张 · 最快 {{ fmtDur(historyCost.min * 1000) }} ·
          最慢 {{ fmtDur(historyCost.max * 1000) }}
        </div>
      </div>
      <div v-else class="cost-bar">
        <div class="cost-sub">
          出了第一张之后，这里会显示「本轮张数 / 总用时 / 平均每张 / 还要多久」。
        </div>
      </div>
      <div v-if="!store.pages.length" class="empty">还没有分镜页。</div>
      <div v-else class="grid grid-pages">
        <article v-for="page in store.pages" :key="page.page_index ?? page.index" class="tile" :class="{ 'is-done': pageImageOf(page) }">
          <div class="tile-thumb tall">
            <img v-if="pageImageOf(page)" :src="fileUrl(pageImageOf(page), page.imageUpdatedAt || page.page_image_url || page.pageImageUrl || page.imagePath)" loading="lazy" @dblclick="zoom(pageImageOf(page))" />
            <span v-else>未生成</span>
          </div>
          <div class="tile-body">
            <div class="tile-title">
              <strong>第 {{ page.page_index ?? page.index }} 页</strong>
              <span class="tag ghost">{{ page.panels?.length || 0 }} 格</span>
            </div>
            <p class="tile-meta">
              {{ fmt(page.scene) }}
              <span v-if="pageMs[pageKey(page)]" class="tile-cost">· 用时 {{ fmtDur(pageMs[pageKey(page)]) }}</span>
              <span v-else-if="page.genSeconds" class="tile-cost">· 上次 {{ page.genSeconds }} 秒</span>
            </p>
            <p v-if="page.safetyNote" class="tile-meta" style="color: var(--warn, #b45309)">⚠ {{ page.safetyNote }}</p>
            <p v-if="page.error && page.status === 'failed'" class="tile-err">{{ page.error }}</p>
          </div>
          <div class="tile-foot">
            <el-button
              size="small"
              :type="pageImageOf(page) ? 'default' : 'primary'"
              :disabled="stateOf('page:' + (page.page_index ?? page.index)) !== ''"
              @click="genPage(page)"
            >
              {{ stateOf('page:' + (page.page_index ?? page.index)) === 'running' ? '生成中…' : stateOf('page:' + (page.page_index ?? page.index)) === 'queued' ? '排队中…' : pageImageOf(page) ? '重画' : '生成' }}
            </el-button>
            <el-button size="small" text @click="openEditor('page', page)">编辑</el-button>
            <el-dropdown trigger="click" @command="(cmd: string) => onPageCommand(cmd, page)">
              <el-button size="small" text>更多 ▾</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="up" :disabled="store.pages.indexOf(page) === 0">上移一页</el-dropdown-item>
                  <el-dropdown-item command="down" :disabled="store.pages.indexOf(page) === store.pages.length - 1">下移一页</el-dropdown-item>
                  <el-dropdown-item command="copy">复制这一页</el-dropdown-item>
                  <el-dropdown-item command="del" divided>删除这一页</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button v-if="pageImageOf(page)" size="small" text tag="a" :href="downloadUrl(pageImageOf(page))" target="_blank">下载</el-button>
            <el-button v-if="pageImageOf(page)" size="small" text @click="copyImagePath(page)">复制路径</el-button>
          </div>
        </article>
      </div>
    </section>

    <!-- 第 5 步 -->
    <section v-if="step === 'export'" class="panel">
      <h3 class="panel-title">第五步：导出</h3>
      <p class="panel-sub">
        单页图在上一页点「下载」即可。整本打包会按下面的勾选现打一个 zip；也可以先把图导出到项目输出目录再自己拿。
      </p>

      <div class="zip-parts">
        <el-checkbox v-for="p in ZIP_PARTS" :key="p.key" v-model="zipParts[p.key]" :title="p.hint">
          {{ p.label }}
        </el-checkbox>
      </div>

      <div class="row" style="margin-top: 12px">
        <el-button type="primary" :loading="exporting" @click="exportAll">整本导出到输出目录</el-button>
        <el-button type="primary" plain tag="a" :href="zipUrl" :disabled="!chosenParts.length">打包下载 zip</el-button>
        <el-button text :loading="planLoading" @click="refreshPlan">刷新预估</el-button>
        <span class="muted">已出图 {{ store.pages.filter((p: Page) => pageImageOf(p)).length }} / {{ store.pages.length }} 页</span>
      </div>

      <div v-if="plan" class="zip-plan">
        <div>
          预计 <b>{{ plan.files }}</b> 个文件<span v-if="planMb"> · 约 <b>{{ planMb }}</b></span>
        </div>
        <div class="muted">{{ planBreakdown }}</div>
        <div v-if="planMissing" class="zip-plan-warn">{{ planMissing }}</div>
      </div>

      <p class="muted" style="margin: 12px 0 0; font-size: 12.5px">
        zip 里：整页图按 comic_page_001.png 编号；分格文案在 script/page_001.txt（逐格旁白/对白/镜头/提示词）；设定图在 refs/；
        项目 JSON 可直接再用「导入项目」读回；page-list.txt 与 manifest.json 说明每个文件是哪一页。缺失的页也会列出来。
      </p>
      <p v-if="exportResult" class="mono" style="margin-top: 12px">{{ exportResult }}</p>
    </section>

    <!-- 页面细节 -->
    <el-dialog v-model="previewOpen" :title="`第 ${previewPage?.page_index ?? previewPage?.index ?? ''} 页`" width="720px">
      <div v-if="previewPage">
        <p class="muted" style="margin-top: 0">场景：{{ fmt(previewPage.scene) }}</p>
        <el-table :data="previewPage.panels || []" size="small" border>
          <el-table-column label="格" width="60">
            <template #default="{ row }">{{ row.panel_index ?? row.index ?? '' }}</template>
          </el-table-column>
          <el-table-column prop="scene" label="场景" width="110" />
          <el-table-column label="旁白">
            <template #default="{ row }">{{ row.narration_text || row.text || '—' }}</template>
          </el-table-column>
          <el-table-column label="对白">
            <template #default="{ row }">{{ row.dialogue_text || '—' }}</template>
          </el-table-column>
        </el-table>
      </div>
    </el-dialog>

    <el-dialog v-model="zoomOpen" width="70%">
      <img :src="zoomSrc" style="width: 100%; border-radius: 10px" />
    </el-dialog>

    <PromptDrawer
      v-model:open="editor.open"
      :kind="editor.kind"
      :target="editor.target"
      :project="store.project"
      :settings="store.settings"
      @save="onEditorSave"
      @generate="onEditorGenerate"
    />

  </div>
</template>

<style scoped>
.episode-strip { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: 0 0 14px; padding: 10px 12px; border: 1px solid var(--line); border-left: 3px solid var(--gold, #b08a3e); background: var(--surface-2); }
.wz-head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
.wz-head-left { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.wz-head-left h2 { margin: 0; font-size: 19px; font-weight: 640; letter-spacing: -.3px; }
.wz-head-right { margin-left: auto; display: flex; gap: 8px; }
.live-msg {
  flex: 1 1 220px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: 12px; color: var(--text-2);
}
.tile-err {
  margin: 6px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--err);
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.tile-cost { color: var(--text-3); }
/* 分镜：一页一条横条，只放文字 */
.sb-list { display: flex; flex-direction: column; gap: 10px; }
.sb-row {
  display: flex; gap: 14px; align-items: flex-start; padding: 12px 14px;
  border: 1px solid var(--line); border-radius: 12px; background: var(--card);
  box-shadow: var(--shadow-s);
}
.sb-row:hover { border-color: color-mix(in srgb, var(--gold, #b08a3e) 40%, var(--line)); }
.sb-no {
  flex: none; width: 54px; display: flex; flex-direction: column; align-items: center;
  padding: 4px 0; border-right: 1px solid var(--line);
}
.sb-no strong { font-size: 18px; line-height: 1.1; }
.sb-no span { font-size: 11px; color: var(--muted); }
.sb-main { flex: 1 1 auto; min-width: 0; }
.sb-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 12.5px; }
.sb-scene { color: var(--muted); }
.sb-layout { color: var(--muted); }
.sb-core { margin: 6px 0 0; font-size: 13.5px; line-height: 1.55; }
.sb-panels { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.sb-panels li {
  display: flex; gap: 8px; font-size: 12.5px; line-height: 1.55; color: var(--muted);
  padding-left: 2px;
}
.sb-panels li b { flex: none; color: var(--ink); font-weight: 600; }
.sb-empty { margin: 6px 0 0; font-size: 12.5px; color: var(--muted); }
.sb-actions { flex: none; display: flex; gap: 6px; align-items: center; }

/* 页数参考 chips */
.density-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 10px; font-size: 12.5px; }
.density-chip {
  border: 1px solid var(--line); background: var(--card); color: var(--muted);
  border-radius: 999px; padding: 4px 12px; cursor: pointer; font: inherit;
}
.density-chip.on { border-color: var(--gold, #b08a3e); color: var(--ink); background: color-mix(in srgb, var(--gold, #b08a3e) 12%, transparent); }
.density-chip:hover { color: var(--ink); }

.render-bar {
  border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; margin-bottom: 12px;
  background: var(--card); box-shadow: var(--shadow-s);
}
.render-bar.running { border-color: color-mix(in srgb, var(--gold, #b08a3e) 45%, var(--line)); }
.render-bar.paused { border-color: #d08a2c; }
.render-bar .rb-head { display: flex; align-items: center; gap: 8px; font-size: 13px; flex-wrap: wrap; }
.render-bar .rb-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--gold, #b08a3e); flex: none; }
.render-bar.running .rb-dot { animation: rb-pulse 1.4s ease-in-out infinite; }
.render-bar.paused .rb-dot { background: #d08a2c; animation: none; }
.render-bar.stopped .rb-dot { background: #b0a79a; }
.render-bar.done .rb-dot { background: #4f8f55; }
@keyframes rb-pulse { 0%, 100% { opacity: 1; } 50% { opacity: .3; } }
.render-bar .rb-track { margin-top: 8px; height: 5px; border-radius: 4px; background: var(--line); overflow: hidden; }
.render-bar .rb-track i { display: block; height: 100%; background: var(--gold, #b08a3e); transition: width .4s ease; }
.render-bar .rb-error { margin-top: 8px; font-size: 12.5px; color: #c0392b; }

.cost-bar {  margin: 0 0 14px; padding: 10px 12px; border-radius: 10px;
  background: var(--surface-2); border: 1px solid var(--line-soft);
}
.cost-main { font-size: 12.5px; font-weight: 600; font-variant-numeric: tabular-nums; }
.cost-sub { margin-top: 3px; font-size: 12px; color: var(--text-2); }
.zip-parts { display: flex; flex-wrap: wrap; gap: 4px 18px; }
.zip-plan {
  margin-top: 12px; padding: 10px 12px; border-radius: 10px;
  background: var(--surface-2); border: 1px solid var(--line-soft); font-size: 12.5px;
}
.zip-plan-warn { margin-top: 4px; color: var(--warn, #b45309); font-size: 12px; }
</style>
