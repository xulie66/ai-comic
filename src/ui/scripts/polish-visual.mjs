// 视觉精修（按调研结论：壳抄 shadcn-admin 的克制细线，卡片抄 art-design-pro 的圆角+微上浮，
// 表单密度抄 Ant Design Pro 的分组小标题）。只改样式与壳结构，不动业务逻辑。
import fs from 'node:fs'

const base = process.argv[2]

/* 1) styles.css：降阴影、加分组小标题、分镜图统一 3:4 */
{
  const file = base + '/src/styles.css'
  let s = fs.readFileSync(file, 'utf8')
  s = s.replace(
    `  --shadow-1: 0 1px 2px rgba(35, 32, 28, .04), 0 6px 18px -12px rgba(35, 32, 28, .18);
  --shadow-2: 0 2px 6px rgba(35, 32, 28, .06), 0 18px 40px -22px rgba(35, 32, 28, .3);`,
    `  --shadow-1: 0 1px 1px rgba(35, 32, 28, .03);
  --shadow-2: 0 4px 14px -8px rgba(35, 32, 28, .22);
  --radius: 14px;`
  )
  s = s.replace(
    `  border-radius: 16px;
  padding: 20px 22px;
  box-shadow: var(--shadow-1);`,
    `  border-radius: var(--radius);
  padding: 20px 22px;`
  )
  s = s.replace(
    `  border-radius: 16px;
  overflow: hidden; display: flex; flex-direction: column;
  transition: border-color .18s, transform .18s, box-shadow .18s;
  box-shadow: var(--shadow-1);`,
    `  border-radius: var(--radius);
  overflow: hidden; display: flex; flex-direction: column;
  transition: border-color .18s, transform .18s, box-shadow .18s;`
  )
  s = s.replace(
    `.tile:hover { border-color: var(--gold-soft); transform: translateY(-2px); box-shadow: var(--shadow-2); }`,
    `.tile:hover { border-color: #dcc79f; transform: translateY(-2px); box-shadow: var(--shadow-2); }`
  )
  s = s.replace(`.tile-thumb.tall { aspect-ratio: 9 / 12.6; }`, `.tile-thumb.tall { aspect-ratio: 3 / 4; }`)
  s = s.replace(`.tile-thumb.ratio-page { aspect-ratio: 9 / 8; }`, `.tile-thumb.ratio-page { aspect-ratio: 3 / 4; }`)
  s += `
/* 侧栏分组小标题（shadcn-admin 那种灰字小标题） */
.side-group { margin: 18px 0 6px; padding: 0 12px; font-size: 10.5px; letter-spacing: 1.2px; text-transform: uppercase; color: #7d766a; }
`
  fs.writeFileSync(file, s)
  console.log('styles.css 已精修')
}

/* 2) App.vue：侧栏加分组标题 */
{
  const file = base + '/src/App.vue'
  let s = fs.readFileSync(file, 'utf8')
  s = s.replace(
    `      <nav class="side-nav">
        <router-link to="/projects" class="side-item"`,
    `      <div class="side-group">工作区</div>
      <nav class="side-nav">
        <router-link to="/projects" class="side-item"`
  )
  s = s.replace(
    `      <div class="side-block">`,
    `      <div class="side-group">系统</div>
      <nav class="side-nav">
        <router-link to="/settings" class="side-item" :class="{ active: activeName === 'settings' }">
          <span class="ico">⚙</span><span>接口设置</span>
        </router-link>
        <a class="side-item" href="/?studio=1" title="旧版高级工作台：逐格微调、日志">
          <span class="ico">↗</span><span>旧版工作台</span>
        </a>
      </nav>

      <div class="side-block">`
  )
  // 原来「接口设置」那条挪到系统分组，避免重复
  s = s.replace(
    `        <router-link to="/settings" class="side-item" :class="{ active: activeName === 'settings' }">
          <span class="ico">⚙</span><span>接口设置</span>
        </router-link>
        <router-link
          v-if="store.projects.activeId"`,
    `        <router-link
          v-if="store.projects.activeId"`
  )
  s = s.replace(
    `        <a href="/?studio=1" class="side-link">旧版高级工作台 ↗</a>\n`,
    ''
  )
  fs.writeFileSync(file, s)
  console.log('App.vue 侧栏已分组')
}
