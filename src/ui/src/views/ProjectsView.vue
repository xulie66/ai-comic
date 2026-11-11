<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAppStore } from '@/stores/app'
import { fileUrl, importProject, importProjectArchive, projectExportUrl } from '@/api/endpoints'
import { ART_STYLES, ASPECT_RATIOS, PAGE_DENSITY } from '@/lib/createOptions'

const store = useAppStore()
const router = useRouter()

const creating = ref(false)
const createOpen = ref(false)
const newName = ref('')
// 新建时就让人把页数 / 页数参考 / 画风 / 比例定下来（老版向导这一步本来就有）
const newPages = ref<number | undefined>(undefined)
const newDensity = ref('balanced')
const newStyle = ref('guoman')
const newRatio = ref('9:16')
const busyId = ref('')
const importing = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const items = computed(() => store.projects.items || [])

onMounted(() => {
  if (!store.ready) void store.load().catch(() => { /* App.vue 已提示 */ })
})

function exportProject(item: any) {
  // 只能导出"当前活动项目"（后端以活动项目为准），所以先切过去再下载
  if (!item.active) {
    ElMessage.warning('先点「打开」切到这个项目，再导出')
    return
  }
  window.location.href = projectExportUrl
}

function pickImportFile() {
  fileInput.value?.click()
}

async function onImportFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files && input.files[0]
  input.value = ''
  if (!file) return
  importing.value = true
  try {
    const archive = /\.zip$/i.test(file.name) || ['application/zip', 'application/x-zip-compressed'].includes(file.type)
    let parsed: any
    let project: any
    if (!archive) {
      const text = await file.text()
      try { parsed = JSON.parse(text) } catch { ElMessage.error('这不是合法的 JSON 文件'); return }
      if (!parsed || typeof parsed !== 'object') { ElMessage.error('项目 JSON 格式有误'); return }
      project = parsed.project || parsed
    }
    const suggested = archive
      ? file.name.replace(/\.zip$/i, '')
      : String(parsed.name || project.title || project.name || file.name.replace(/\.json$/i, ''))
    let name = suggested
    try {
      const got = await ElMessageBox.prompt('给导入的项目起个名字（会新建成一个项目，不影响现有的）', '导入项目', {
        inputValue: suggested,
        confirmButtonText: '导入',
        cancelButtonText: '取消'
      })
      name = String(got.value || suggested).trim() || suggested
    } catch { return }
    const res = archive
      ? await importProjectArchive(file, name)
      : await importProject({ name, project })
    if (res.list) store.projects = res.list
    await store.load()
    const pages = typeof res.pages === 'number' ? `：${res.pages} 页` : ''
    ElMessage.success(`导入完成${pages}${res.staleImages ? `（${res.staleImages} 张图路径失效，需要重画）` : ''}`)
    const id = res.project?.id
    if (id) router.push({ name: 'wizard', params: { id } })
  } catch (err: any) {
    ElMessage.error(err?.message || '导入失败')
  } finally {
    importing.value = false
  }
}

async function open(id: string) {
  busyId.value = id
  try {
    await store.openProject(id)
    router.push({ name: 'wizard', params: { id } })
  } catch (err: any) {
    ElMessage.error(err?.message || '打开项目失败')
  } finally {
    busyId.value = ''
  }
}

async function create() {
  const name = newName.value.trim()
  if (!name) { ElMessage.warning('给新项目起个名字'); return }
  creating.value = true
  try {
    const res = await store.createProject(name)
    // 新建时就定下页数口径 / 画风 / 比例，别让人进去再找一遍
    const project: any = store.project || {}
    project.targetPageCount = newPages.value || undefined
    // 手填了页数＝锁定；留空＝按页数参考自动算（老工作台的口径）
    project.targetPageCountManual = Boolean(newPages.value)
    project.pageDensity = newDensity.value
    project.artStyle = newStyle.value
    project.aspectRatio = newRatio.value
    store.project = project
    try { await store.saveNow() } catch { /* 存不上不影响进项目页 */ }
    newName.value = ''
    createOpen.value = false
    const id = (res.project && (res.project.id || res.project.item?.id)) || store.projects.activeId
    ElMessage.success('项目已创建')
    if (id) router.push({ name: 'wizard', params: { id } })
  } catch (err: any) {
    ElMessage.error(err?.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function remove(id: string, name: string) {
  try {
    await ElMessageBox.confirm(`删除项目「${name}」？目录会被移到回收目录，不会立刻清空。`, '确认删除', {
      confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
    })
  } catch { return }
  try {
    await store.deleteProject(id)
    ElMessage.success('已删除')
  } catch (err: any) {
    ElMessage.error(err?.message || '删除失败')
  }
}

function updatedText(item: any) {
  const raw = item.updatedAt || item.updated_at
  if (!raw) return ''
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return ''
  const diff = Date.now() - d.getTime()
  const min = Math.round(diff / 60000)
  if (min < 1) return '刚刚更新'
  if (min < 60) return `${min} 分钟前`
  const hour = Math.round(min / 60)
  if (hour < 24) return `${hour} 小时前`
  return `${Math.round(hour / 24)} 天前`
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1>我的项目</h1>
        <p class="muted">每个项目是一本漫画：一份原文、一套角色与场景设定图、一份分镜。</p>
      </div>
      <input ref="fileInput" type="file" accept="application/json,.json,application/zip,application/x-zip-compressed,.zip" style="display: none" @change="onImportFile" />
      <el-button size="large" :loading="importing" @click="pickImportFile">导入项目文件</el-button>
      <el-button type="primary" size="large" @click="createOpen = true">＋ 新建项目</el-button>
    </div>

    <div v-if="!items.length" class="empty">
      还没有项目。点右上角「新建项目」开始，然后贴入小说原文一键拆分。
    </div>

    <div v-else class="grid grid-projects">
      <article v-for="item in items" :key="item.id" class="tile project-tile">
        <div class="tile-thumb cover">
          <img v-if="item.cover" :src="fileUrl(item.cover)" :alt="item.name" loading="lazy" />
          <span v-else class="cover-empty">暂无封面</span>
        </div>
        <div class="tile-body">
          <div class="tile-title">
            <strong>{{ item.name }}</strong>
            <span v-if="item.active" class="tag ok">当前</span>
          </div>
          <div class="chips">
            <span class="chip">{{ item.pageCount ?? 0 }} 页</span>
            <span class="chip">{{ item.panelCount ?? 0 }} 格</span>
            <span class="chip">{{ item.characterCount ?? 0 }} 角色</span>
          </div>
          <p class="tile-desc">{{ item.title || '还没有拆分分镜' }}</p>
          <p v-if="updatedText(item)" class="tile-meta">{{ updatedText(item) }}</p>
        </div>
        <div class="tile-foot">
          <el-button size="small" type="primary" :loading="busyId === item.id" @click="open(item.id)">
            {{ item.active ? '继续创作' : '打开' }}
          </el-button>
          <el-button size="small" text :disabled="!item.active" title="导出这个项目的 JSON（备份/换机器）" @click="exportProject(item)">
            导出
          </el-button>
          <el-button size="small" text @click="remove(item.id, item.name)">删除</el-button>
        </div>
      </article>
    </div>

    <el-dialog v-model="createOpen" title="新建项目" width="520px">
      <el-input v-model="newName" placeholder="例如：都市异能 第一卷" size="large" @keyup.enter="create" />
      <div class="create-grid">
        <label class="cg-item">
          <span>目标页数</span>
          <el-input-number v-model="newPages" :min="1" :max="200" placeholder="自动" controls-position="right" />
        </label>
        <label class="cg-item">
          <span>页面比例</span>
          <el-select v-model="newRatio">
            <el-option v-for="[v, l] in ASPECT_RATIOS" :key="v" :value="v" :label="l" />
          </el-select>
        </label>
        <label class="cg-item">
          <span>画风</span>
          <el-select v-model="newStyle">
            <el-option v-for="[v, l] in ART_STYLES" :key="v" :value="v" :label="l" />
          </el-select>
        </label>
      </div>
      <div class="cg-density">
        <span class="muted">页数参考</span>
        <button
          v-for="d in PAGE_DENSITY"
          :key="d.key"
          class="density-chip"
          :class="{ on: newDensity === d.key }"
          @click="newDensity = d.key"
        >
          {{ d.label }} · {{ d.hint }}
        </button>
      </div>
      <p class="muted" style="margin: 10px 0 0; font-size: 12.5px">
        页数留空＝由模型按剧情定；贴完正文后第 1 步会按「页数参考」自动算出建议页数。
      </p>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.create-grid { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 10px; margin-top: 12px; }
.cg-item { display: flex; flex-direction: column; gap: 5px; font-size: 12.5px; color: var(--muted); }
.cg-item :deep(.el-input-number), .cg-item :deep(.el-select) { width: 100%; }
.cg-density { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 12px; font-size: 12.5px; }
.density-chip {
  border: 1px solid var(--line); background: transparent; color: var(--muted);
  border-radius: 999px; padding: 4px 12px; cursor: pointer; font: inherit;
}
.density-chip.on { border-color: var(--gold, #b08a3e); color: var(--ink); background: color-mix(in srgb, var(--gold, #b08a3e) 12%, transparent); }
.density-chip:hover { color: var(--ink); }
.page-head { display: flex; align-items: flex-end; gap: 16px; flex-wrap: wrap; margin-bottom: 20px; }
.page-head h1 { margin: 0; font-size: 22px; font-weight: 700; letter-spacing: .3px; }
.page-head p { margin: 6px 0 0; font-size: 12.5px; }
.page-head .el-button { margin-left: auto; }

.project-tile .cover { aspect-ratio: 16 / 10; }
.cover-empty { font-size: 12px; letter-spacing: 1px; }
.chips { display: flex; gap: 6px; margin-top: 8px; flex-wrap: wrap; }
.chip {
  padding: 1px 8px; border-radius: 999px; font-size: 11.5px;
  background: var(--paper-3); border: 1px solid var(--line-soft); color: var(--ink-2);
}
</style>
