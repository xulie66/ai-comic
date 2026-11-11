// 新建项目 / 第一步要用的选项口径。
// 这些值直接对着后端（main.orig.cjs 的 ART_STYLES / 页数推荐口径），别再各写一份——
// 少一个选项就是从界面上"丢功能"，之前画风丢了 6 种、比例丢了 4:5 就是这么来的。
export const ART_STYLES: Array<[string, string]> = [
  ['guoman', '高颜值国漫'],
  ['chibi', 'Q萌二头身'],
  ['stick', '简笔小人'],
  ['matchstick', '火柴人'],
  ['american', '美式卡通'],
  ['mangabw', '黑白网点漫画'],
  ['clay', '黏土玩偶'],
  ['anime', '日式动画'],
  ['realistic', '写实电影'],
  ['watercolor', '水彩手绘'],
  ['cyberpunk', '赛博朋克'],
  ['ink', '水墨']
]

export const ASPECT_RATIOS: Array<[string, string]> = [
  ['9:16', '9:16 竖屏（推荐）'],
  ['3:4', '3:4'],
  ['4:5', '4:5'],
  ['1:1', '1:1'],
  ['16:9', '16:9 横屏']
]

/** 页数参考口径：和老版向导一致（100 / 135 / 200 字每页） */
export const PAGE_DENSITY = [
  { key: 'fine', chars: 100, label: '细腻', hint: '100 字/页' },
  { key: 'balanced', chars: 135, label: '均衡', hint: '135 字/页' },
  { key: 'cheap', chars: 200, label: '省钱', hint: '200 字/页' }
] as const

export type DensityKey = (typeof PAGE_DENSITY)[number]['key']

export function countChars(text?: string) {
  return String(text || '').replace(/\s/g, '').length
}

export function densityOf(key?: string) {
  return PAGE_DENSITY.find((d) => d.key === key) || PAGE_DENSITY[1]
}

/** 按"字数 / 每页字数"算建议页数，4 页起步，200 页封顶 */
export function suggestPages(text?: string, densityKey?: string) {
  const chars = countChars(text)
  if (!chars) return 0
  const per = densityOf(densityKey).chars
  return Math.max(4, Math.min(200, Math.round(chars / per)))
}

export function artStyleLabel(value?: string) {
  return ART_STYLES.find(([k]) => k === value)?.[1] || String(value || '')
}

export function ratioLabel(value?: string) {
  return ASPECT_RATIOS.find(([k]) => k === value)?.[1] || String(value || '')
}
