// 任务实时事件（SSE）。
// 后端 /api/events 会把主进程 comic:progress 的推送原样广播出来，
// 事件形状形如 { taskId, stage, message, level?, pageNumber? }。
export interface TaskEvent {
  taskId?: string
  stage?: string
  message?: string
  level?: string
  pageNumber?: number
  characterCount?: number
  at: number
}

type Handler = (ev: TaskEvent) => void
type StateHandler = (online: boolean) => void

let source: EventSource | null = null
const handlers = new Set<Handler>()
const stateHandlers = new Set<StateHandler>()
let online = true

function setOnline(next: boolean) {
  if (next === online) return
  online = next
  stateHandlers.forEach((h) => { try { h(next) } catch { /* ignore */ } })
}

export function isOnline() {
  return online
}

/** 订阅连接状态：断线时界面要提示"实时进度可能不准" */
export function onEventsState(handler: StateHandler): () => void {
  ensure()
  stateHandlers.add(handler)
  handler(online)
  return () => { stateHandlers.delete(handler) }
}

function ensure() {
  if (source) return
  // EventSource 不能带自定义头，鉴权走同源 Cookie
  source = new EventSource('/api/events', { withCredentials: true })
  source.onopen = () => setOnline(true)
  source.onmessage = (ev) => {
    let data: any
    try { data = JSON.parse(ev.data) } catch { return }
    if (!data || typeof data !== 'object') return
    setOnline(true)
    const event: TaskEvent = { ...data, at: Date.now() }
    handlers.forEach((h) => { try { h(event) } catch { /* 单个订阅者出错不影响别人 */ } })
  }
  // 浏览器会自动重连；这里只负责把"现在是断的"告诉界面
  source.onerror = () => setOnline(false)
}

export function onTaskEvent(handler: Handler): () => void {
  ensure()
  handlers.add(handler)
  return () => { handlers.delete(handler) }
}

/** 阶段名 → 中文标签，界面上别让用户看英文 stage（其余阶段名以真实推送为准） */
export const STAGE_LABEL: Record<string, string> = {
  text_split: '拆解分镜',
  long_text_split_success: '拆解完成',
  storyboard_merge_success: '分镜合并完成',
  character_extract_success: '角色提取完成',
  image_generate_start: '开始出图',
  image_generate_poll: '出图进行中',
  image_generate_success: '出图完成',
  image_generate_error: '出图失败',
  image_model_fallback: '切换备用模型',
  image_provider_retry: '接口重试',
  image_reference_limit: '参考图超出上限',
  image_reference_warning: '参考图提示',
  prompt_rewrite_start: '安全改写提示词',
  prompt_rewrite_success: '提示词已改写',
  prompt_rewrite_error: '提示词改写失败'
}

export function stageLabel(stage?: string): string {
  if (!stage) return ''
  return STAGE_LABEL[stage] || stage
}
