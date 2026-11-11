import { defineStore } from 'pinia'
import * as api from '@/api/endpoints'
import type { RenderStatus } from '@/api/endpoints'
import { onTaskEvent, onEventsState, type TaskEvent } from '@/api/events'
import type { Project, ProjectList, Settings, User } from '@/api/types'

const EMPTY_RENDER: RenderStatus = {
  state: 'idle', total: 0, done: 0, failed: 0, remaining: 0, active: 0, concurrency: 2,
  current: [], queued: [], elapsedSeconds: 0, avgSeconds: 0, lastError: '',
  autoPaused: false, startedAt: 0, finishedAt: 0, projectId: '', finishedPages: 0
}

interface StoreState {
  ready: boolean
  loading: boolean
  error: string
  settings: Settings
  project: Project
  projects: ProjectList
  user: User | null
  saving: boolean
  lastSavedAt: number
  events: TaskEvent[]
  eventsBound: boolean
  eventsOnline: boolean
  render: RenderStatus
  quietAt: number
  forceQuietPending: boolean
}

let saveTimer: number | undefined
const MAX_EVENTS = 300

export const useAppStore = defineStore('app', {
  state: (): StoreState => ({
    ready: false,
    loading: false,
    error: '',
    settings: {},
    project: {},
    projects: { activeId: '', activeRoot: '', items: [] },
    user: null,
    saving: false,
    lastSavedAt: 0,
    events: [],
    eventsBound: false,
    eventsOnline: true,
    render: { ...EMPTY_RENDER },
    quietAt: 0,
    forceQuietPending: false
  }),

  getters: {
    lastEvent(state): TaskEvent | null {
      return state.events.length ? state.events[state.events.length - 1] : null
    },
    activeProject(state) {
      return state.projects.items.find((item) => item.id === state.projects.activeId) || null
    },
    pages(state) {
      return state.project?.storyboard?.pages || []
    },
    characters(state): any[] {
      return state.project?.characterLibrary || state.project?.storyboard?.characterLibrary || []
    },
    scenes(state): any[] {
      return state.project?.sceneLibrary || state.project?.storyboard?.sceneLibrary || []
    },
    hasStoryboard(state) {
      return Boolean(state.project?.storyboard?.pages?.length)
    },
    /** 后台批量出图是否在动（切到任何页面都能看到这个） */
    renderBusy(state): boolean {
      return state.render.state === 'running' || state.render.state === 'paused'
    },
    /** 出图进度文本：12 / 50 · 剩 38 页 */
    renderProgressText(state): string {
      const r = state.render
      return `${r.done + r.failed} / ${r.total}${r.remaining ? ` · 剩 ${r.remaining} 页` : ''}`
    }
  },

  actions: {
    // 订阅后端 SSE：整个页面只连一条，事件进 store，各视图自己挑要看的
    bindEvents() {
      if (this.eventsBound) return
      this.eventsBound = true
      onEventsState((online) => { this.eventsOnline = online })
      onTaskEvent((ev: any) => {
        // 后台出图状态是"状态快照"，不进日志流（否则每张图两三条，日志会被刷没）
        if (ev?.stage === 'image_ready') {
          // 服务端先落盘再推送，收到后立即同步权威项目数据。
          void this.loadQuietly(true)
        }
        if (ev?.stage === 'render_status' && ev.render) {
          this.render = { ...EMPTY_RENDER, ...ev.render }
          // 状态里说某页完成了，界面上那页要跟着亮起来
          if ((this.render.done + this.render.failed) > 0 || this.render.state === 'done') {
            void this.loadQuietly(this.render.state === 'done')
          }
          return
        }
        const last = this.events[this.events.length - 1]
        // 同阶段同文案的重复推送不刷屏
        if (last && last.stage === ev.stage && last.message === ev.message && last.pageNumber === ev.pageNumber) {
          this.events[this.events.length - 1] = ev
          return
        }
        this.events.push(ev)
        if (this.events.length > MAX_EVENTS) this.events.splice(0, this.events.length - MAX_EVENTS)
      })
    },

    clearEvents() {
      this.events = []
    },

    /** 前端自己发现的问题也丢进同一个日志流，用户在一个地方就能看全 */
    pushEvent(ev: Omit<TaskEvent, 'at'>) {
      this.events.push({ ...ev, at: Date.now() })
      if (this.events.length > MAX_EVENTS) this.events.splice(0, this.events.length - MAX_EVENTS)
    },

    async load() {
      this.loading = true
      this.error = ''
      try {
        const res: any = await api.state()
        this.settings = res.settings || {}
        this.project = res.project || {}
        if (res.projects) this.projects = res.projects
        if (res.render) this.render = { ...EMPTY_RENDER, ...res.render }
        this.ready = true
        return res
      } catch (err: any) {
        this.error = err?.message || '加载失败'
        throw err
      } finally {
        this.loading = false
      }
    },

    /** 后台出图期间静默刷新项目（最多 3 秒一次），卡片上的图跟着出来 */
    async loadQuietly(force = false) {
      if (this.loading) {
        if (force) this.forceQuietPending = true
        return
      }
      const now = Date.now()
      if (!force && now - this.quietAt < 3000) return
      this.quietAt = now
      try {
        const res: any = await api.state()
        this.project = res.project || this.project
        if (res.render) this.render = { ...EMPTY_RENDER, ...res.render }
      } catch { /* 静默失败，下一轮再试 */ }
      finally {
        if (this.forceQuietPending) {
          this.forceQuietPending = false
          window.setTimeout(() => { void this.loadQuietly(true) }, 0)
        }
      }
    },

    /** 只拉后台出图状态（进任何页面时对一次账） */
    async syncRender() {
      try {
        const res: any = await api.renderStatus()
        if (res?.status) this.render = { ...EMPTY_RENDER, ...res.status }
      } catch { /* 忽略 */ }
    },

    async loadMe() {
      try {
        const res = await api.auth.me()
        this.user = res.user
      } catch { this.user = null }
    },

    async openProject(id: string) {
      const res = await api.projects.open(id)
      if (res.list) this.projects = res.list
      await this.load()
      return res
    },

    async createProject(name: string) {
      const res = await api.projects.create(name)
      if (res.list) this.projects = res.list
      await this.load()
      return res
    },

    async deleteProject(id: string) {
      const res = await api.projects.remove(id)
      if (res.list) this.projects = res.list
      return res
    },

    // 保存防抖：编辑越频繁越省请求，但最多 1.2 秒一定会写盘
    saveSoon(delay = 700) {
      window.clearTimeout(saveTimer)
      saveTimer = window.setTimeout(() => { void this.saveNow() }, delay)
    },

    async saveNow() {
      if (!this.project) return
      this.saving = true
      try {
        await api.saveProject(this.project)
        this.lastSavedAt = Date.now()
      } catch (err: any) {
        this.error = err?.message || '保存失败'
      } finally {
        this.saving = false
      }
    },

    async saveSettings(patch: Settings) {
      const merged = { ...this.settings, ...patch }
      await api.saveSettings(merged)
      this.settings = merged
      return merged
    }
  }
})
