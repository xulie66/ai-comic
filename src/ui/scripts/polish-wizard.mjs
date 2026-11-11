// 第二轮 UI 调整：把创作页顶部的"项目信息面板 + 步骤条"改成更紧凑的步骤条 + 概览条。
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')
const pairs = []

pairs.push([
  `const STEP_KEYS = ['import', 'cast', 'storyboard', 'render', 'export'] as const
type StepKey = (typeof STEP_KEYS)[number]`,
  `const STEP_KEYS = ['import', 'cast', 'storyboard', 'render', 'export'] as const
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
  if (key === 'render') return store.pages.some((p: Page) => p.imagePath)
  return false
}`
])

pairs.push([
  `    <section class="panel">
      <h2 class="panel-title">
        {{ store.activeProject?.name || '未命名项目' }}
        <span v-if="store.activeProject?.active" class="tag ok">当前项目</span>
        <el-button size="small" text style="margin-left: auto" @click="router.push('/projects')">返回项目列表</el-button>
        <el-button size="small" text @click="store.saveNow()">立即保存</el-button>
      </h2>
      <div class="stat-row">
        <div class="stat"><b>{{ store.pages.length }}</b><span>分镜页</span></div>
        <div class="stat"><b>{{ store.characters.length }}</b><span>角色</span></div>
        <div class="stat"><b>{{ store.scenes.length }}</b><span>场景</span></div>
        <div class="stat"><b>{{ store.pages.filter((p: Page) => p.imagePath).length }}</b><span>已出图</span></div>
      </div>
    </section>

    <div class="steps">
      <div
        v-for="(key, i) in STEP_KEYS"
        :key="key"
        class="step"
        :class="{ active: step === key }"
        @click="step = key"
      >
        <span class="no">{{ i + 1 }}</span>
        <span>{{ { import: '导入原文', cast: '角色与场景', storyboard: '分镜', render: '逐页出图', export: '导出' }[key] }}</span>
      </div>
    </div>`,
  `    <div class="wz-head">
      <div class="wz-head-left">
        <h2>{{ store.activeProject?.name || '未命名项目' }}</h2>
        <span v-if="store.activeProject?.active" class="tag ok">当前项目</span>
        <span class="muted">已出图 {{ store.pages.filter((p: Page) => p.imagePath).length }} / {{ store.pages.length }} 页</span>
      </div>
      <div class="wz-head-right">
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
    </div>`
])

let ok = 0
const missing = []
for (const [from, to] of pairs) {
  if (s.includes(from)) { s = s.split(from).join(to); ok += 1 }
  else missing.push(from.split('\n')[0].slice(0, 60))
}
s += `
<style scoped>
.wz-head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
.wz-head-left { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.wz-head-left h2 { margin: 0; font-size: 19px; font-weight: 700; letter-spacing: .3px; }
.wz-head-right { margin-left: auto; display: flex; gap: 8px; }
</style>
`
fs.writeFileSync(file, s)
console.log(`替换成功 ${ok} / 失败 ${missing.length}`)
missing.forEach((m) => console.log('  未命中: ' + m))
