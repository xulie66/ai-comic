// 纯函数测试：页面增删排序 + 打包命名。用 esbuild 把 TS 转成 JS 后直接跑，不依赖测试框架。
// 跑法：node scripts/test-pages.mjs
import { readFile, writeFile, mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const src = path.join(here, '..', 'src', 'lib', 'pages.ts')

// pnpm 不把 esbuild 提升到顶层，直接从 .pnpm 里按版号取
const esbuildPkg = path.join(here, '..', 'node_modules', '.pnpm', 'esbuild@0.25.12', 'node_modules', 'esbuild', 'lib', 'main.js')
const esbuild = (await import(pathToFileURL(esbuildPkg).href)).default ?? (await import(pathToFileURL(esbuildPkg).href))

const code = await readFile(src, 'utf8')
const { code: js } = await esbuild.transform(code, { loader: 'ts', format: 'esm' })
const dir = await mkdtemp(path.join(tmpdir(), 'pages-test-'))
const out = path.join(dir, 'pages.mjs')
await writeFile(out, js, 'utf8')
const lib = await import(pathToFileURL(out).href)

const page = (n, image) => ({ page_index: n, page_core: 'p' + n, ...(image ? { imagePath: image } : {}) })

let pass = 0
let fail = 0
function check(name, cond, detail = '') {
  if (cond) { pass += 1; console.log(`  ok   ${name}`) }
  else { fail += 1; console.log(`  FAIL ${name} ${detail}`) }
}

const idx = (pages) => pages.map((p) => p.page_index).join(',')
const cores = (pages) => pages.map((p) => p.page_core).join(',')

console.log('页面操作：')
{
  const pages = [page(1), page(2), page(3)]
  const r = lib.move(pages, pages[1], -1)
  check('上移第 2 页 → 顺序 2,1,3', cores(r.pages) === 'p2,p1,p3', cores(r.pages))
  check('上移后重编号 → 1,2,3', idx(r.pages) === '1,2,3', idx(r.pages))
  check('moved=true', r.moved === true)
}
{
  const pages = [page(1), page(2), page(3)]
  const r = lib.move(pages, 0, -1)
  check('第一页上移不动', cores(r.pages) === 'p1,p2,p3' && r.moved === false)
}
{
  const pages = [page(1), page(2), page(3)]
  const r = lib.move(pages, 2, 1)
  check('最后一页下移不动', cores(r.pages) === 'p1,p2,p3' && r.moved === false)
}
{
  const pages = [page(1, '/x/a.png'), page(2, '/x/b.png')]
  const r = lib.duplicate(pages, 0)
  check('复制第 1 页插在它后面', cores(r.pages) === 'p1,p1,p2', cores(r.pages))
  check('复制后重编号 → 1,2,3', idx(r.pages) === '1,2,3', idx(r.pages))
  check('复制页不带原图', r.pages[1].imagePath === undefined, String(r.pages[1].imagePath))
  check('原页还带图', r.pages[0].imagePath === '/x/a.png')
  check('复制是深拷贝（改副本不动原件）', (() => { r.pages[1].page_core = 'X'; return r.pages[0].page_core === 'p1' })())
}
{
  const pages = [page(1), page(2, '/x/b.png'), page(3)]
  const r = lib.remove(pages, 1)
  check('删除第 2 页 → 剩 p1,p3', cores(r.pages) === 'p1,p3', cores(r.pages))
  check('删除后重编号 → 1,2', idx(r.pages) === '1,2', idx(r.pages))
  check('删除返回被删的页（带着图）', r.removed && r.removed.imagePath === '/x/b.png')
}
{
  const pages = [page(1)]
  const r = lib.remove(pages, 5)
  check('删不存在的页 → removed=null，列表不变', r.removed === null && pages.length === 1)
}
{
  const pages = [page(7), page(8), page(9)]
  lib.renumber(pages)
  check('乱序编号能被重排成 1,2,3', idx(pages) === '1,2,3', idx(pages))
}

console.log('打包文件名：')
check('png', lib.zipNameOf(0, '/x/a.png') === 'comic_page_001.png')
check('jpg', lib.zipNameOf(9, '/x/a.JPG') === 'comic_page_010.jpg')
check('第 100 页还是三位', lib.zipNameOf(99) === 'comic_page_100.png')
check('第 1000 页不截断', lib.zipNameOf(999) === 'comic_page_1000.png')

console.log('失败页判定：')
check('status=failed 算失败', lib.isFailedPage({ status: 'failed' }) === true)
check('有 error 没图算失败', lib.isFailedPage({ error: '网络连接失败' }) === true)
check('有图就算成功（即便 status 还是 failed）', lib.isFailedPage({ status: 'failed', imagePath: '/x/a.png' }) === false)
check('page_image_url 有值也算成功', lib.isFailedPage({ page_image_url: '/x/a.png', status: 'failed' }) === false)
check('啥都没有不算失败', lib.isFailedPage({}) === false)
check('已出图但带老错误不算失败', lib.isFailedPage({ imagePath: '/x/a.png', error: '旧的' }) === false)

console.log(`\n${pass} 通过 / ${fail} 失败`)
process.exit(fail ? 1 : 0)
