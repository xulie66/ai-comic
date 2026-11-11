// 主题系统 v4：只留一个"浅色 / 深色"开关，浅色=素白，深色=墨黑。
// 另外两套（石墨深侧栏 / 靛蓝）放在设置页「外观」里，需要时再选。
export type ThemeKey = 'mono' | 'midnight' | 'graphite' | 'indigo'

export interface ThemeMeta {
  key: ThemeKey
  name: string
  hint: string
  dark?: boolean
}

export const THEMES: ThemeMeta[] = [
  { key: 'mono', name: '素白', hint: '浅色，默认' },
  { key: 'midnight', name: '墨黑', hint: '深色', dark: true },
  { key: 'graphite', name: '石墨', hint: '浅色 + 深侧栏' },
  { key: 'indigo', name: '靛蓝', hint: '浅色 + 靛蓝强调' }
]

export const LIGHT_KEY: ThemeKey = 'mono'
export const DARK_KEY: ThemeKey = 'midnight'

const KEY = 'ac_theme_v4'

export function isDarkTheme(key: ThemeKey): boolean {
  const meta = THEMES.find((t) => t.key === key)
  return Boolean(meta?.dark)
}

export function readTheme(): ThemeKey {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw && THEMES.some((t) => t.key === raw)) return raw as ThemeKey
  } catch { /* ignore */ }
  return LIGHT_KEY
}

export function applyTheme(key: ThemeKey) {
  const meta = THEMES.find((t) => t.key === key) || THEMES[0]
  const root = document.documentElement
  root.dataset.theme = meta.key
  // Element Plus 自带深色变量：靠 html.dark 生效
  root.classList.toggle('dark', Boolean(meta.dark))
  // 让浏览器地址栏/滚动条也跟着深浅切换
  root.style.colorScheme = meta.dark ? 'dark' : 'light'
  try { localStorage.setItem(KEY, meta.key) } catch { /* ignore */ }
}

/** 浅色 ↔ 深色 一键切换（其它主题按下它的明暗归到对应默认） */
export function nextTheme(key: ThemeKey): ThemeKey {
  return isDarkTheme(key) ? LIGHT_KEY : DARK_KEY
}
