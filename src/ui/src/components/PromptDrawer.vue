<script setup lang="ts">
/**
 * 提示词编辑抽屉：角色 / 场景 / 分镜页三合一。
 * 和老工作台行为一致：
 *  - 字段随便改，改动先落在副本上，点「保存」才写回项目；
 *  - 完整提示词走 /api/preview/prompt（服务端用生成时同一个函数拼），
 *    手改后存成 promptOverride，改回和自动版本一样就自动清掉 override；
 *  - 「保存并生成」= 保存 + 立刻出图/出设定图。
 */
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '@/api/endpoints'
import { applyRewrite, syncPanelText } from '@/lib/pages'
import type { CharacterCard, Page, Project, SceneCard, Settings } from '@/api/types'

const props = defineProps<{
  open: boolean
  kind: 'character' | 'page' | 'scene'
  target: any
  project: Project
  settings: Settings
}>()

const emit = defineEmits<{
  (e: 'update:open', v: boolean): void
  (e: 'save'): void
  (e: 'generate', kind: string, draft: any): void
}>()

const SIZE_OPTIONS = ['大格', '中格', '小格', '横长格', '竖长格', '特写格']
const SHOT_OPTIONS: [string, string][] = [
  ['establish_scene', '建立镜头 / 环境全貌'],
  ['action_mid_shot', '中景动作'],
  ['phone_closeup', '手机界面特写'],
  ['face_closeup', '面部特写'],
  ['eye_closeup', '眼睛特写'],
  ['object_closeup', '物件特写'],
  ['split_screen', '分屏'],
  ['wide_shot', '远景'],
  ['over_shoulder', '过肩镜头'],
  ['top_down', '俯视']
]
const BUBBLE_OPTIONS: [string, string][] = [
  ['auto', '自动'],
  ['left', '靠左'],
  ['right', '靠右'],
  ['top', '顶部'],
  ['bottom', '底部']
]
const ROLE_OPTIONS: [string, string][] = [
  ['male_lead', '男主'],
  ['female_lead', '女主'],
  ['supporting', '配角'],
  ['antagonist', '反派'],
  ['elder', '长辈'],
  ['child', '儿童角色']
]

const draft = ref<any>({})
const promptText = ref('')
const autoPrompt = ref('')
const overridden = ref(false)
const loadingPrompt = ref(false)
const promptError = ref('')
const layouts = ref<{ id: string; name: string; panelCount: number }[]>([])
const pageCharacters = ref<string[]>([])
const pageScenes = ref<string[]>([])

const title = computed(() => {
  if (props.kind === 'character') return '角色：' + (draft.value.name || '未命名')
  if (props.kind === 'scene') return '场景：' + (draft.value.name || '未命名')
  return '第 ' + (draft.value.page_index ?? draft.value.index ?? '') + ' 页'
})

const panels = computed(() => (Array.isArray(draft.value.panels) ? draft.value.panels : []))

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v ?? {}))
}

watch(
  () => [props.open, props.target, props.kind],
  () => {
    if (!props.open || !props.target) return
    draft.value = clone(props.target)
    promptText.value = ''
    autoPrompt.value = ''
    overridden.value = false
    promptError.value = ''
    layouts.value = []
    pageCharacters.value = []
    pageScenes.value = []
    void loadPrompt(false)
  },
  { immediate: true }
)

function previewPayload() {
  if (props.kind === 'character') {
    return { kind: 'character', artStyle: props.project?.artStyle, character: draft.value }
  }
  if (props.kind === 'scene') {
    return { kind: 'scene', artStyle: props.project?.artStyle, scene: draft.value }
  }
  return {
    kind: 'page',
    settings: props.settings,
    storyboard: { ...(props.project?.storyboard || {}), pages: props.project?.storyboard?.pages },
    page: draft.value,
    artStyle: props.project?.artStyle,
    aspectRatio: props.project?.aspectRatio,
    quality: props.project?.quality,
    imageQuality: props.project?.imageQuality,
    bookKeyword: props.project?.bookKeyword
  }
}

async function loadPrompt(resetOverride = false) {
  if (resetOverride) draft.value.promptOverride = ''
  loadingPrompt.value = true
  promptError.value = ''
  try {
    const res: any = await api.previewPrompt(previewPayload())
    promptText.value = res.prompt || ''
    overridden.value = Boolean(res.overridden)
    if (!res.overridden) autoPrompt.value = res.prompt || ''
    if (Array.isArray(res.layouts)) layouts.value = res.layouts
    if (Array.isArray(res.characters)) pageCharacters.value = res.characters
    if (Array.isArray(res.scenes)) pageScenes.value = res.scenes
  } catch (err: any) {
    promptError.value = err?.message || '提示词预览失败'
  } finally {
    loadingPrompt.value = false
  }
}

async function restorePrompt() {
  if (String(draft.value.promptOverride || '').trim()) {
    try {
      await ElMessageBox.confirm(
        '这会丢掉你手写的提示词，用左侧字段重新拼一份。想保留手写内容就点「只保存」。',
        '按字段重排提示词？',
        { confirmButtonText: '按字段重排', cancelButtonText: '取消', type: 'warning' }
      )
    } catch { return }
  }
  await loadPrompt(true)
}

function linesToArray(v: string | undefined | null) {
  return String(v || '')
    .split(/\n+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function arrayToText(v: any) {
  if (Array.isArray(v)) return v.join('\n')
  return String(v || '')
}

function collect() {
  // 旁白/对白改完要写回"拼提示词真正在用的"text_blocks 等老字段，
  // 否则界面看着改了、发给模型的还是旧句子（内容安全被拒的页尤其致命）。
  panels.value.forEach((pn: any) => syncPanelText(pn))
  const text = promptText.value.trim()
  const auto = autoPrompt.value.trim()
  if (!text || (auto && text === auto)) delete draft.value.promptOverride
  else draft.value.promptOverride = text
  return draft.value
}

// 「安全改写」：把触发上游审核的暴力/自残/恐怖表述换成含蓄说法，保留剧情情绪。
// 老工作台有这个按钮，新界面之前漏了——内容安全被拒的页在旧流程里能救，新流程里只能干瞪眼。
const rewriting = ref(false)
const rewriteReason = ref('')

async function safetyRewrite() {
  if (props.kind !== 'page') return
  rewriting.value = true
  rewriteReason.value = ''
  try {
    const res: any = await api.rewritePagePrompt({
      page: draft.value,
      storyboard: props.project?.storyboard,
      settings: props.settings,
      failureReason: draft.value.error || ''
    })
    if (!res?.success) {
      ElMessage.error(res?.userMessage || '安全改写失败')
      return
    }
    const touched = applyRewrite(draft.value, res.rewrite)
    if (res.rewrite?.rewrite_reason) rewriteReason.value = res.rewrite.rewrite_reason
    await loadPrompt()
    ElMessage.success(`已安全改写 ${touched} 格，提示词已同步刷新`)
  } catch (err: any) {
    ElMessage.error(err?.message || '安全改写失败')
  } finally {
    rewriting.value = false
  }
}

function commit(generate = false) {
  const next = collect()
  if (props.target && typeof props.target === 'object') {
    Object.keys(props.target).forEach((key) => { delete props.target[key] })
    Object.assign(props.target, next)
  }
  emit('save')
  if (generate) emit('generate', props.kind, next)
  emit('update:open', false)
  ElMessage.success(generate ? '已保存，开始生成' : '已保存')
}

function close() {
  emit('update:open', false)
}

const sizeOptions = SIZE_OPTIONS.map((s) => ({ label: s, value: s }))
</script>

<template>
  <el-drawer
    :model-value="open"
    :title="title"
    size="52%"
    class="prompt-drawer"
    @update:model-value="(v: boolean) => emit('update:open', v)"
  >
    <div v-if="draft && Object.keys(draft).length" class="pd-body">
      <!-- 角色 -->
      <template v-if="kind === 'character'">
        <div class="pd-section">
          <div class="pd-section-title">基本设定</div>
          <div class="pd-grid">
            <el-form-item label="姓名"><el-input v-model="draft.name" /></el-form-item>
            <el-form-item label="定位">
              <el-select v-model="draft.role" style="width: 100%">
                <el-option v-for="r in ROLE_OPTIONS" :key="r[0]" :value="r[0]" :label="r[1]" />
              </el-select>
            </el-form-item>
            <el-form-item label="性别"><el-input v-model="draft.gender" placeholder="男 / 女" /></el-form-item>
            <el-form-item label="年龄"><el-input v-model="draft.age" placeholder="原文没写就留空" /></el-form-item>
            <el-form-item label="身份"><el-input v-model="draft.identity" placeholder="职业 / 社会身份" /></el-form-item>
            <el-form-item label="性格"><el-input v-model="draft.personality" /></el-form-item>
          </div>
          <el-form-item label="外貌（决定设定图长相）">
            <el-input v-model="draft.appearance" type="textarea" :rows="3" />
          </el-form-item>
          <el-form-item label="服装"><el-input v-model="draft.outfit" type="textarea" :rows="2" /></el-form-item>
          <el-form-item label="道具（逗号分隔）">
            <el-input
              :model-value="Array.isArray(draft.props) ? draft.props.join('，') : draft.props"
              @update:model-value="(v: string) => (draft.props = v.split(/[，,、]/).map((s) => s.trim()).filter(Boolean))"
            />
          </el-form-item>
        </div>
      </template>

      <!-- 场景 -->
      <template v-else-if="kind === 'scene'">
        <div class="pd-section">
          <div class="pd-section-title">场景设定</div>
          <div class="pd-grid">
            <el-form-item label="场景名"><el-input v-model="draft.name" /></el-form-item>
            <el-form-item label="时间"><el-input v-model="draft.time_of_day" placeholder="白天 / 深夜 / 黄昏" /></el-form-item>
            <el-form-item label="氛围"><el-input v-model="draft.mood" placeholder="压抑 / 温暖 / 冷峻" /></el-form-item>
          </div>
          <el-form-item label="空间描述（决定结构，别写人物动作）">
            <el-input v-model="draft.description" type="textarea" :rows="3" />
          </el-form-item>
          <el-form-item label="关键元素（每行一个）">
            <el-input
              :model-value="arrayToText(draft.key_elements)"
              type="textarea"
              :rows="2"
              @update:model-value="(v: string) => (draft.key_elements = linesToArray(v))"
            />
          </el-form-item>
        </div>
      </template>

      <!-- 分镜页 -->
      <template v-else>
        <div class="pd-section">
          <div class="pd-section-title">页面</div>
          <div class="pd-grid">
            <el-form-item label="核心剧情 page_core">
              <el-input
                :model-value="draft.page_core || draft.summary"
                type="textarea"
                :rows="2"
                @update:model-value="(v: string) => { draft.page_core = v; draft.summary = v }"
              />
            </el-form-item>
            <el-form-item label="页面布局 layout_type">
              <el-select v-model="draft.layout_type" filterable style="width: 100%" placeholder="模板清单">
                <el-option
                  v-for="l in layouts"
                  :key="l.id"
                  :value="l.id"
                  :label="l.name + ' · ' + l.panelCount + ' 格'"
                />
                <el-option v-if="draft.layout_type && !layouts.some((l) => l.id === draft.layout_type)" :value="draft.layout_type" :label="draft.layout_type" />
              </el-select>
            </el-form-item>
          </div>
          <p class="pd-note">改核心剧情或布局会把这一页标记为待重画（和老工作台一致）。</p>
          <p v-if="pageCharacters.length || pageScenes.length" class="pd-note">
            本页出场人物：{{ pageCharacters.length ? pageCharacters.join('、') : '（没识别到，会用相邻页兜底）' }}
            <span v-if="pageScenes.length"> · 本页场景：{{ pageScenes.join('、') }}</span>
          </p>
          <div class="pd-actions">
            <el-button size="small" :loading="rewriting" @click="safetyRewrite">
              安全改写本页提示词
            </el-button>
            <span class="pd-note" style="margin: 0">
              出图被上游以「自残/暴力/血腥」为由拒绝时用这个：把过火的说法换成含蓄表达，剧情情绪保留。
            </span>
          </div>
          <p v-if="rewriteReason" class="pd-note">上次改写原因：{{ rewriteReason }}</p>
        </div>

        <div v-for="(pn, i) in panels" :key="i" class="pd-section pd-panel">
          <div class="pd-section-title">
            分镜 {{ pn.panel_index || i + 1 }}
            <span class="pd-dim">{{ pn.panel_size || '中格' }}</span>
          </div>
          <div class="pd-grid">
            <el-form-item label="格子尺寸">
              <el-select v-model="pn.panel_size" style="width: 100%">
                <el-option v-for="s in sizeOptions" :key="s.value" :value="s.value" :label="s.label" />
              </el-select>
            </el-form-item>
            <el-form-item label="镜头类型">
              <el-select v-model="pn.shot_type" style="width: 100%" @change="(v: string) => (pn.camera = v)">
                <el-option v-for="s in SHOT_OPTIONS" :key="s[0]" :value="s[0]" :label="s[1]" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="具体场景">
            <el-input v-model="pn.scene" @update:model-value="(v: string) => (pn.background = v)" />
          </el-form-item>
          <el-form-item label="标题框 title_text"><el-input v-model="pn.title_text" /></el-form-item>
          <el-form-item label="旁白框（每行一个框）">
            <el-input
              :model-value="arrayToText(pn.narration_text || pn.narration)"
              type="textarea"
              :rows="2"
              @update:model-value="(v: string) => { pn.narration_text = v; pn.narration = linesToArray(v) }"
            />
          </el-form-item>
          <el-form-item label="对白气泡（每行一个气泡）">
            <el-input
              :model-value="arrayToText(pn.dialogue_text || pn.dialog)"
              type="textarea"
              :rows="2"
              @update:model-value="(v: string) => { pn.dialogue_text = linesToArray(v); pn.dialog = linesToArray(v) }"
            />
          </el-form-item>
          <div class="pd-grid">
            <el-form-item label="说话人"><el-input v-model="pn.speaker" /></el-form-item>
            <el-form-item label="气泡位置">
              <el-select v-model="pn.bubblePosition" style="width: 100%">
                <el-option v-for="b in BUBBLE_OPTIONS" :key="b[0]" :value="b[0]" :label="b[1]" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="手机文字 phone_text（每行一条）">
            <el-input
              :model-value="arrayToText(pn.phone_text)"
              type="textarea"
              :rows="1"
              @update:model-value="(v: string) => (pn.phone_text = linesToArray(v))"
            />
          </el-form-item>
          <div class="pd-grid">
            <el-form-item label="人物（、分隔）">
              <el-input
                :model-value="arrayToText(pn.characters)"
                @update:model-value="(v: string) => (pn.characters = linesToArray(v.replace(/[，,]/g, '\n')))"
              />
            </el-form-item>
            <el-form-item label="文字位置"><el-input v-model="pn.text_position" /></el-form-item>
          </div>
          <div class="pd-grid">
            <el-form-item label="情绪"><el-input v-model="pn.emotion" /></el-form-item>
            <el-form-item label="动作"><el-input v-model="pn.action" /></el-form-item>
          </div>
          <el-form-item label="最终生图提示词 visual_prompt">
            <el-input
              :model-value="pn.visual_prompt || pn.image_prompt"
              type="textarea"
              :rows="3"
              @update:model-value="(v: string) => { pn.visual_prompt = v; pn.image_prompt = v }"
            />
          </el-form-item>
          <p v-if="pn.safety_notes" class="pd-note">安全改写说明：{{ pn.safety_notes }}</p>
        </div>
      </template>

      <!-- 完整提示词 -->
      <div class="pd-section pd-prompt">
        <div class="pd-section-title">
          完整提示词
          <span class="pd-dim">{{ overridden ? '已手动修改' : '自动生成' }}</span>
          <span class="pd-dim" style="margin-left: auto">{{ promptText.length }} 字</span>
        </div>
        <p v-if="promptError" class="pd-err">{{ promptError }}</p>
        <div v-if="loadingPrompt" class="pd-loading">正在生成提示词…</div>
        <el-input v-else v-model="promptText" type="textarea" :rows="12" class="pd-mono" spellcheck="false" />
        <p class="pd-note">
          这段就是最终发给模型的全部内容。直接改会存成<b>自定义提示词</b>；点「按字段重排提示词」会用上面的字段重拼一份。
        </p>
      </div>
    </div>

    <template #footer>
      <div class="pd-foot">
        <el-button @click="restorePrompt">按字段重排提示词</el-button>
        <div style="flex: 1" />
        <el-button @click="close">取消</el-button>
        <el-button @click="commit(false)">只保存</el-button>
        <el-button @click="commit(true)">
          {{ kind === 'page' ? '保存并重新生成' : kind === 'character' ? '保存并生成设定图' : '保存并生成场景图' }}
        </el-button>
        <el-button type="primary" @click="commit(false)">保存并关闭</el-button>
      </div>
    </template>
  </el-drawer>
</template>

<style scoped>
.pd-body { padding: 0 4px 12px; }
.pd-section { border: 1px solid var(--line); border-radius: 12px; padding: 14px 16px; margin-bottom: 14px; background: var(--surface); }
.pd-section-title { display: flex; align-items: center; gap: 8px; font-weight: 600; margin-bottom: 10px; font-size: 14px; }
.pd-dim { font-weight: 400; color: var(--ink-3); font-size: 12px; }
.pd-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 14px; }
.pd-note { margin: 6px 0 0; font-size: 12px; color: var(--ink-3); line-height: 1.6; }
.pd-actions { display: flex; align-items: center; gap: 10px; margin-top: 10px; flex-wrap: wrap; }
.pd-err { margin: 0 0 8px; color: var(--err); font-size: 12.5px; }
.pd-loading { padding: 18px; text-align: center; color: var(--text-3); background: var(--surface-2); border-radius: 10px; }
.pd-panel { background: var(--surface-2); }
.pd-prompt { background: var(--surface-2); border-color: var(--line); }
.pd-mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12.5px; line-height: 1.6; }
.pd-foot { display: flex; gap: 8px; align-items: center; }
</style>
