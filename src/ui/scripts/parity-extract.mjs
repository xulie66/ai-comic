// 第三轮：把「从原文提取角色/场景/道具」和「删除角色」补进来，对齐老界面的能力。
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')
const pairs = []

pairs.push([
  `const extractingScenes = ref(false)
async function extractScenes() {
  extractingScenes.value = true
  try {
    const res: any = await api.extractAssets('scenes')
    const items: SceneCard[] = res?.scenes || res?.items || []
    const lib = (store.project.sceneLibrary ||= [])
    let added = 0
    items.forEach((item) => {
      if (!item?.name) return
      const hit = lib.find((s) => s.name === item.name)
      if (hit) { if (!hit.description && item.description) hit.description = item.description; return }
      lib.push(item)
      added += 1
    })
    store.saveSoon()
    ElMessage.success(\`提取到 \${items.length} 个场景，新增 \${added} 个\`)
  } catch (err: any) {
    ElMessage.error(err?.message || '提取失败')
  } finally {
    extractingScenes.value = false
  }
}`,
  `const extracting = ref<'' | 'characters' | 'scenes' | 'props'>('')
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
    extractResult.value = \`提取到 \${items.length} 个\${kind === 'characters' ? '角色' : kind === 'scenes' ? '场景' : '道具'}，新增 \${added} 个\`
    ElMessage.success(extractResult.value)
  } catch (err: any) {
    ElMessage.error(err?.message || '提取失败')
  } finally {
    extracting.value = ''
  }
}`
])

pairs.push([
  `function genAllCharacters() {`,
  `async function removeCharacter(card: CharacterCard) {
  const lib = store.project.characterLibrary || []
  const idx = lib.findIndex((c: CharacterCard) => c.name === card.name)
  if (idx < 0) return
  try {
    await ElMessageBox.confirm(
      \`删除角色「\${card.name}」？只是从这个项目里去掉，已经生成的分镜页不受影响。\`,
      '确认删除',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  lib.splice(idx, 1)
  await store.saveNow()
  ElMessage.success('已删除')
}

function genAllCharacters() {`
])

// 场景页按钮区：把「从原文提取场景」换成角色/场景/道具三个入口
pairs.push([
  `            <el-button :loading="extractingScenes" @click="extractScenes">从原文提取场景</el-button>`,
  `            <el-button :loading="extracting === 'scenes'" @click="extractAssets('scenes')">从原文提取场景</el-button>`
])

pairs.push([
  `            <el-button type="primary" @click="genAllCharacters">全部生成设定图</el-button>`,
  `            <el-button type="primary" @click="genAllCharacters">全部生成设定图</el-button>
            <el-button :loading="extracting === 'characters'" @click="extractAssets('characters')">从原文提取角色</el-button>
            <el-button :loading="extracting === 'props'" @click="extractAssets('props')">提取道具</el-button>`
])

// 角色卡片补一个删除入口
pairs.push([
  `                <el-button size="small" text @click="openEditor('character', card)">编辑提示词</el-button>`,
  `                <el-button size="small" text @click="openEditor('character', card)">编辑提示词</el-button>
                <el-button size="small" text type="danger" @click="removeCharacter(card)">删除</el-button>`
])

// 覆盖确认对话框里用到的 import
pairs.push([
  `import { ElMessage } from 'element-plus'`,
  `import { ElMessage, ElMessageBox } from 'element-plus'`
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
