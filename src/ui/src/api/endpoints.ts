// 一张表把后端接口写清楚：界面只调这里，不直接拼 URL。
import { get, post, upload } from './http'
import type {
  CharacterCard, GenResult, Page, Project, ProjectList, SceneCard,
  Settings, StateResponse, Storyboard, User
} from './types'

/* ---------- 账号 ---------- */
export const auth = {
  login: (username: string, password: string) =>
    post<{ token: string; user: User }>('/auth/login', { username, password }),
  me: () => get<{ user: User }>('/auth/me'),
  logout: () => post('/auth/logout', {}),
  changePassword: (oldPassword: string, newPassword: string) =>
    post('/auth/password', { oldPassword, newPassword })
}

/* ---------- 状态 / 项目 ---------- */
export const state = () => post<StateResponse>('/state', {})

export const projects = {
  list: () => post<{ list: ProjectList }>('/projects/list', {}),
  create: (name: string) => post<{ list: ProjectList; project: any }>('/projects/create', { name }),
  open: (id: string) => post<{ list: ProjectList; project: any }>('/projects/open', { id }),
  rename: (id: string, name: string) => post<{ list: ProjectList }>('/projects/rename', { id, name }),
  remove: (id: string) => post<{ list: ProjectList; trashedTo: string }>('/projects/delete', { id })
}

export const saveProject = (project: Project) => post('/project/save', project)

/* ---------- 项目导入 / 导出（换机器、备份）---------- */
// 导出是 GET + 附件下载，鉴权走同源 Cookie，所以直接用 <a href> 或 location 跳
export const projectExportUrl = '/api/project/export'

export const importProject = (payload: { name?: string; project: any }) =>
  post<{ list: ProjectList; project: { id: string; name: string }; pages: number; staleImages: number }>(
    '/project/import',
    payload
  )

export const importProjectArchive = (file: File, name?: string) => {
  const form = new FormData()
  form.append('file', file, file.name)
  if (name) form.append('name', name)
  return upload<{ list: ProjectList; project: { id: string; name: string }; pages?: number; staleImages?: number }>(
    '/project/import', form
  )
}

/* ---------- 生成 ---------- */
export const generateStoryboard = (payload: Record<string, any>) =>
  post<{ storyboard: Storyboard; characterLibrary?: CharacterCard[] }>('/generate/storyboard', payload)

export const generatePage = (payload: { page: Page; storyboard: Storyboard; settings?: Settings; aspectRatio?: string; artStyle?: string; quality?: string; imageQuality?: string }) =>
  post<GenResult>('/generate/page', payload)

export const generateCharacterReference = (payload: { settings?: Settings; character: CharacterCard; artStyle?: string; aspectRatio?: string; quality?: string; imageQuality?: string }) =>
  post<GenResult>('/generate/character', payload)

export const generateSceneReference = (payload: { settings?: Settings; scene: SceneCard; artStyle?: string; quality?: string; imageQuality?: string }) =>
  post<GenResult>('/generate/scene', payload)

export const rewritePagePrompt = (payload: Record<string, any>) =>
  post<{ prompt?: string }>('/generate/rewritePrompt', payload)

// 后端语义：把已出图的页按 comic_page_001.png 的顺序复制进 output 目录，返回目录与文件清单。
// 页面字段名是 page_image_url（沿用 Electron 版的数据结构），不是 imagePath。
export const exportAllPages = (payload: { pages: any[]; outputDir?: string }) =>
  post<{ directory: string; files: string[] }>('/pages/exportAll', payload)


export const previewPrompt = (payload: Record<string, any>) =>
  post<{ prompt: string; length: number; characters?: string[] }>('/preview/prompt', payload)

/* ---------- 场景 / 素材 ---------- */
export const rebuildScenes = () =>
  post<{ library: SceneCard[]; canonical: { total: number; renamed: number } }>('/scenes/rebuild', {})

export const extractAssets = (kind: 'characters' | 'scenes' | 'props', text = '') =>
  post<any>('/extract/assets', { kind, text })

// 写进服务器素材库（merge 增量 / replace 覆盖），返回最新素材库
export const extractSave = (kind: 'characters' | 'scenes' | 'props', items: any[], mode: 'merge' | 'replace' = 'merge') =>
  post<any>('/extract/save', { kind, items, mode })

export const savedAssets = () => get<any>('/extract/saved')

/* ---------- 后台出图队列（服务端跑，切页面/关标签都不停） ---------- */
export interface RenderStatus {
  state: 'idle' | 'running' | 'paused' | 'stopped' | 'done'
  total: number
  done: number
  failed: number
  remaining: number
  active: number
  concurrency: number
  current: number[]
  queued: number[]
  elapsedSeconds: number
  avgSeconds: number
  lastError: string
  autoPaused: boolean
  startedAt: number
  finishedAt: number
  projectId: string
  finishedPages: number
}

export const renderStatus = () => get<{ status: RenderStatus }>('/render/status')

export const renderStart = (payload: {
  mode: 'missing' | 'all' | 'list'
  indices?: number[]
  concurrency?: number
  settings?: Settings
  aspectRatio?: string
  artStyle?: string
  quality?: string
  imageQuality?: string
}) => post<{ success: boolean; message?: string; status: RenderStatus }>('/render/start', payload)

export const renderPause = () =>
  post<{ success: boolean; message?: string; status: RenderStatus }>('/render/pause', {})

export const renderResume = () =>
  post<{ success: boolean; message?: string; status: RenderStatus }>('/render/resume', {})

export const renderStop = () =>
  post<{ success: boolean; message?: string; status: RenderStatus }>('/render/stop', {})

/* ---------- 用量与额度（每用户限流） ---------- */
export interface UsageQuota {
  isAdmin: boolean
  unlimited: boolean
  dailyPages: number
  pagesPerMinute: number
}

export interface UsageSummary {
  day: string
  pages: number
  images: number
  storyboards: number
  textCalls: number
}

export interface UsageResponse {
  success: boolean
  user?: { id: string; username: string; displayName: string; isAdmin: boolean }
  usage: UsageSummary
  limits: UsageQuota
  remaining: number
  totals?: { pages?: number; images?: number; storyboards?: number; characters?: number }
}

export const usage = () => get<UsageResponse>('/usage')

/* ---------- 钱包 / 充值 / 兑换码 ---------- */
export interface WalletOrder {
  id: string
  outTradeNo: string
  amountYuan: number
  credits: number
  status: string
  payType: string
  createdAt: string
  paidAt?: string | null
}

export interface WalletSummary {
  success: boolean
  balance: number
  balanceMicro: number
  status: string
  enabled: boolean
  note: string
  canRecharge: boolean
  redeemEnabled: boolean
  ledger: Array<{ delta: number; deltaMicro: number; kind: string; note: string; balanceAfter: number; at: string }>
  orders: WalletOrder[]
}

export interface PayPackage {
  id: string
  title: string
  amountYuan: number
  bonusPercent: number
  credits: number
  note: string
}

export const credits = () => get<WalletSummary>('/wallet')
export const payPackages = () =>
  get<{ success: boolean; enabled: boolean; payTypes: string[]; creditsPerYuan: number; manualNote: string; packages: PayPackage[] }>(
    '/pay/packages'
  )
export const payCreate = (payload: { packageId?: string; amountYuan?: number; payType?: string }) =>
  post<{
    success: boolean; id: string; outTradeNo: string; mode: string; checkoutUrl: string
    qrPayload?: string; openUrl?: string; qrFallback?: boolean
    amountYuan: number; credits: number; expiresInMinutes?: number
  }>(
    '/pay/create',
    payload
  )
export const payStatus = (query: { id?: string; out_trade_no?: string }) => {
  const search = new URLSearchParams()
  if (query.id) search.set('id', query.id)
  if (query.out_trade_no) search.set('out_trade_no', query.out_trade_no)
  return get<{ success: boolean; order: WalletOrder & { checkoutUrl?: string }; balance: number; balanceMicro: number }>(
    `/pay/status?${search.toString()}`
  )
}
export const redeem = (code: string) => post<{ success: boolean; credits: number; balance: number }>('/redeem', { code })

/** 当前生效的统一模型（后台配置，前台只读展示） */
export const unifiedModels = () =>
  get<{
    success: boolean
    image: { logicalCode: string; modelKey: string; billingMode: string } | null
    text: { logicalCode: string; modelKey: string; billingMode: string } | null
  }>('/models/unified')
export const availableModels = () => get<{ models: any[]; defaults: { image: string; text: string } }>('/models/available')
export const setDefaultModel = (capability: 'image' | 'text', modelId: string) => post('/models/default', { capability, modelId })

export interface AdminUserRow extends UsageResponse {
  id: string
  username: string
  displayName: string
  isAdmin: boolean
  createdAt?: string
  lastLoginAt?: string
}

export const adminUsers = () =>
  get<{ success: boolean; users: AdminUserRow[]; defaults: { dailyPages: number; pagesPerMinute: number } }>('/admin/users')

export const setQuota = (userId: string, dailyPageQuota: number) =>
  post<{ success: boolean; user: AdminUserRow }>('/admin/quota', { userId, dailyPageQuota })

export const setAdmin = (userId: string, isAdmin: boolean) =>
  post<{ success: boolean; user: AdminUserRow }>('/admin/admin', { userId, isAdmin })

/* ---------- 设置 ---------- */
export const saveSettings = (settings: Settings) => post('/settings/save', settings)
export const testText = (settings: Settings) => post('/test/text', settings)
export const testImage = (settings: Settings) => post('/test/image', settings)
// 基础网络 + 图片 API 两条线路的连通性（原工作台「检查网络」按钮的同一份逻辑）
export const testNetwork = () =>
  post<{ ok: boolean; results: { name: string; reachable: boolean; status: number | null; url: string; error?: string }[] }>(
    '/test/network',
    {}
  )

// 单张图导出到服务器输出目录（老工作台「另存为」的 web 版）
export const exportImage = (payload: { sourcePath: string; outputDir?: string; aspectRatio?: string; quality?: string; format?: string }) =>
  post<{ path: string; canceled?: boolean }>('/image/export', payload)

// 老工作台「复制图片」在 web 里没有剪贴板图像；服务端只记一条 copy_image 日志，用于确认路径有效
export const copyImage = (imagePath: string) => post<boolean>('/image/copy', { imagePath })

/* ---------- 文件访问 ---------- */
// 后端把输出目录挂成 /files?p=<绝对路径>，老前端也是这么取图的
export function fileUrl(path?: string, cacheKey?: string | number) {
  if (!path) return ''
  const query = path.startsWith('/files?') ? path : '/files?p=' + encodeURIComponent(path)
  return cacheKey === undefined ? query : `${query}&v=${encodeURIComponent(String(cacheKey))}`
}

export function downloadUrl(path?: string) {
  if (!path) return ''
  return '/download?p=' + encodeURIComponent(path)
}

// 整本打包下载：打包哪些内容由前端选，服务端按 parts 现打现发
//   pages 整页图 / script 逐页逐格文案 / refs 角色场景设定图 / project 项目 JSON / panels 分格图
export const ZIP_PARTS = [
  { key: 'pages', label: '整页图', hint: 'comic_page_001.png' },
  { key: 'script', label: '分格文案', hint: 'script/page_001.txt（逐格旁白/对白/提示词）' },
  { key: 'project', label: '项目 JSON', hint: 'project.json，可再导入' },
  { key: 'refs', label: '角色与场景设定图', hint: 'refs/ 目录，体积较大' },
  { key: 'panels', label: '分格图', hint: '项目里出过分格图才有' }
] as const

export type ZipPart = (typeof ZIP_PARTS)[number]['key']

export interface ZipPlan {
  parts: string[]
  counts: {
    pages: number; script: number; refs: number; panels: number
    missingPages: number; missingRefs: number; missingPanels: number
  }
  files: number
  bytes: number
}

const partsQuery = (parts: readonly string[]) => (parts.length ? parts.join(',') : '')

export function pagesZipUrl(parts: readonly string[] = ['pages', 'script', 'project']) {
  const value = partsQuery(parts)
  return '/api/pages/zip' + (value ? '?parts=' + value : '')
}

export const zipPlan = (parts: readonly string[]) =>
  get<ZipPlan>('/pages/zip/plan', { parts: partsQuery(parts) })

