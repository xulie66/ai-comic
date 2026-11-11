// 补两个之前漏掉的接口：素材库写入、按类型取已保存素材
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')

const from = `export const extractAssets = (kind: 'characters' | 'scenes' | 'props', text = '') =>
  post<any>('/extract/assets', { kind, text, settings: undefined })`

const to = `export const extractAssets = (kind: 'characters' | 'scenes' | 'props', text = '') =>
  post<any>('/extract/assets', { kind, text })

// 写进服务器素材库（merge 增量 / replace 覆盖），返回最新素材库
export const extractSave = (kind: 'characters' | 'scenes' | 'props', items: any[], mode: 'merge' | 'replace' = 'merge') =>
  post<any>('/extract/save', { kind, items, mode })`

if (!s.includes(from)) {
  console.error('锚点未命中')
  process.exit(1)
}
s = s.replace(from, to)
fs.writeFileSync(file, s)
console.log('endpoints.ts 已补 extractSave')
