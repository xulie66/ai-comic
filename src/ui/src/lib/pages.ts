// 页面列表的纯函数操作：不碰 Vue、不碰网络，方便直接用 node 跑测试。
// 界面里所有"上移/下移/复制/删除"都走这里，避免每处各写一遍重编号逻辑。
import type { Page } from '@/api/types'

/** 页正文重排后统一重编号（1 起），页面顺序 = 数组顺序 */
export function renumber(pages: Page[]): Page[] {
  pages.forEach((page, i) => {
    page.page_index = i + 1
  })
  return pages
}

/** 上移(-1) / 下移(+1)；越界或找不到就原样返回 */
export function move(pages: Page[], page: Page | number, dir: -1 | 1): { pages: Page[]; moved: boolean; to: number } {
  const from = typeof page === 'number' ? page : pages.indexOf(page)
  const to = from + dir
  if (from < 0 || to < 0 || to >= pages.length) return { pages, moved: false, to: from }
  const [item] = pages.splice(from, 1)
  pages.splice(to, 0, item)
  renumber(pages)
  return { pages, moved: true, to }
}

/** 复制一页插在后面：图片和手写提示词清掉，避免两页共用同一张图 */
export function duplicate(pages: Page[], page: Page | number): { pages: Page[]; created: Page | null; at: number } {
  const from = typeof page === 'number' ? page : pages.indexOf(page)
  if (from < 0) return { pages, created: null, at: from }
  const copy: Page = JSON.parse(JSON.stringify(pages[from]))
  delete copy.imagePath
  delete copy.page_image_url
  delete copy.pageImageUrl
  delete copy.promptOverride
  delete copy.status
  pages.splice(from + 1, 0, copy)
  renumber(pages)
  return { pages, created: copy, at: from + 1 }
}

/** 删除一页，后面的自动重编号 */
export function remove(pages: Page[], page: Page | number): { pages: Page[]; removed: Page | null; at: number } {
  const at = typeof page === 'number' ? page : pages.indexOf(page)
  if (at < 0 || at >= pages.length) return { pages, removed: null, at }
  const [removed] = pages.splice(at, 1)
  renumber(pages)
  return { pages, removed, at }
}

/** 已出图的页按顺序给出打包名：comic_page_001.png */
export function zipNameOf(index: number, imagePath?: string): string {
  const ext = (imagePath || '').toLowerCase().endsWith('.jpg') || (imagePath || '').toLowerCase().endsWith('.jpeg') ? 'jpg' : 'png'
  return `comic_page_${String(index + 1).padStart(3, '0')}.${ext}`
}

/**
 * 这页的图在哪：新界面自己出图写 imagePath，老 Electron 版写 page_image_url / pageImageUrl。
 * 三处都得认——只认 imagePath 会让老项目在第四步全部显示「未生成」。
 */
export function pageImageOf(page: Page | null | undefined): string {
  if (!page) return ''
  return String(page.imagePath || page.page_image_url || page.pageImageUrl || '')
}

/**
 * 这页算不算"失败"：出过图的不算；否则 status 是 failed、或者带了错误信息且没图，都算。
 * 判断放在这里是因为它决定界面上的「重画失败页」按钮要不要出现。
 */
export function isFailedPage(page: Page): boolean {
  if (!page) return false
  if (pageImageOf(page)) return false
  return page.status === 'failed' || Boolean(page.error)
}

/**
 * 把抽屉里编辑过的旁白/对白同步回"拼提示词真正在用的"那几个老字段。
 *
 * 踩过的坑：分镜解析阶段会把旁白写进 panel.text_blocks / panel.text / panel.original_text，
 * 而提示词是从 text_blocks 里取的；只改 narration_text 的话，界面显示变了、出图提示词还是旧句子——
 * 于是「安全改写」看似成功，实际把原来那句触发审核的话又发了一遍。
 */
export function syncPanelText(panel: any): void {
  if (!panel || typeof panel !== 'object') return
  const narration = String(panel.narration_text ?? '').trim()
  const dialogues: string[] = Array.isArray(panel.dialogue_text)
    ? panel.dialogue_text.map((t: any) => String(t || '').trim()).filter(Boolean)
    : []
  const existing: any[] = Array.isArray(panel.text_blocks)
    ? panel.text_blocks
    : Array.isArray(panel.textBlocks)
      ? panel.textBlocks
      : []
  const keptDialogue = existing.filter((b) => b && b.type !== 'narration')
  const keptNarration = existing.filter((b) => b && b.type === 'narration')

  if (narration) {
    panel.narration = [narration]
    panel.text = narration
    panel.original_text = narration
    panel.source_text = narration
    panel.sourceText = narration
  }

  const blocks: any[] = []
  if (narration) blocks.push({ type: 'narration', text: narration, speaker: '' })
  else keptNarration.forEach((b) => blocks.push(b))
  if (dialogues.length) {
    dialogues.forEach((t) => blocks.push({ type: 'dialogue', text: t, speaker: panel.speaker || '' }))
  } else {
    keptDialogue.forEach((b) => blocks.push(b))
  }
  if (blocks.length) {
    panel.text_blocks = blocks
    panel.textBlocks = blocks
  }
}

/** 把「安全改写」接口返回的 panels 合进页面（含老字段同步），返回改了几格 */
export function applyRewrite(page: any, rewrite: any): number {
  const rw = Array.isArray(rewrite?.panels) ? rewrite.panels : []
  const panels: any[] = Array.isArray(page?.panels) ? page.panels : []
  let touched = 0
  panels.forEach((pn) => {
    const rp = rw.find((x: any) => Number(x?.panel_index ?? x?.panel_number) === Number(pn.panel_index ?? pn.panel_number))
    if (!rp) return
    ;['visual_prompt', 'image_prompt', 'action', 'emotion', 'title_text', 'narration_text', 'speaker', 'safety_notes'].forEach((k) => {
      if (rp[k]) pn[k] = rp[k]
    })
    if (Array.isArray(rp.dialogue_text) && rp.dialogue_text.length) pn.dialogue_text = rp.dialogue_text
    if (Array.isArray(rp.phone_text) && rp.phone_text.length) pn.phone_text = rp.phone_text
    syncPanelText(pn)
    touched += 1
  })
  if (rewrite?.page_core) page.page_core = rewrite.page_core
  if (rewrite?.summary) page.summary = rewrite.summary
  return touched
}
