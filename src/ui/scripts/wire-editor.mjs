// 一次性迁移脚本：把「编辑提示词 / 编辑分镜」按钮和 PromptDrawer 挂进 WizardView。
// 用 node 跑，避免 PowerShell 的引号解析问题。
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')
const pairs = []

pairs.push([
  `                  {{ stateOf('char:' + card.name) === 'running' ? '生成中…' : stateOf('char:' + card.name) === 'queued' ? '排队中…' : card.referenceImagePath ? '重做' : '生成设定图' }}\n                </el-button>`,
  `                  {{ stateOf('char:' + card.name) === 'running' ? '生成中…' : stateOf('char:' + card.name) === 'queued' ? '排队中…' : card.referenceImagePath ? '重做' : '生成设定图' }}\n                </el-button>\n                <el-button size="small" text @click="openEditor('character', card)">编辑提示词</el-button>`
])

pairs.push([
  `                  {{ stateOf('scene:' + scene.name) === 'running' ? '生成中…' : stateOf('scene:' + scene.name) === 'queued' ? '排队中…' : scene.refImagePath ? '重画' : '生成场景图' }}\n                </el-button>`,
  `                  {{ stateOf('scene:' + scene.name) === 'running' ? '生成中…' : stateOf('scene:' + scene.name) === 'queued' ? '排队中…' : scene.refImagePath ? '重画' : '生成场景图' }}\n                </el-button>\n                <el-button size="small" text @click="openEditor('scene', scene)">编辑提示词</el-button>`
])

pairs.push([
  `            <el-button size="small" @click="openPreview(page)">看细节</el-button>`,
  `            <el-button size="small" @click="openPreview(page)">看细节</el-button>\n            <el-button size="small" type="primary" plain @click="openEditor('page', page)">编辑分镜</el-button>`
])

pairs.push([
  `            <el-button v-if="page.imagePath" size="small" text tag="a" :href="downloadUrl(page.imagePath)" target="_blank">下载</el-button>`,
  `            <el-button size="small" text @click="openEditor('page', page)">编辑</el-button>\n            <el-button v-if="page.imagePath" size="small" text tag="a" :href="downloadUrl(page.imagePath)" target="_blank">下载</el-button>`
])

pairs.push([
  `    <el-dialog v-model="zoomOpen" width="70%">\n      <img :src="zoomSrc" style="width: 100%; border-radius: 10px" />\n    </el-dialog>`,
  `    <el-dialog v-model="zoomOpen" width="70%">\n      <img :src="zoomSrc" style="width: 100%; border-radius: 10px" />\n    </el-dialog>\n\n    <PromptDrawer\n      v-model:open="editor.open"\n      :kind="editor.kind"\n      :target="editor.target"\n      :project="store.project"\n      :settings="store.settings"\n      @save="onEditorSave"\n      @generate="onEditorGenerate"\n    />`
])

let ok = 0
const missing = []
for (const [from, to] of pairs) {
  if (s.includes(from)) { s = s.split(from).join(to); ok += 1 }
  else missing.push(from.split('\n')[0].slice(0, 70))
}
fs.writeFileSync(file, s)
console.log(`替换成功 ${ok} / 失败 ${missing.length}`)
missing.forEach((m) => console.log('  未命中: ' + m))
