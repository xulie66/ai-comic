// 修复：PowerShell 的 @"..."@ 不会把 \n 当换行，导致组件里出现字面 \n。
// 这里把字面 "\n" 还原成真换行；唯一需要保留的是正则里的 /\n/ → 先临时占位保护。
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')
if (!s.includes('\\n')) {
  console.log('没有需要修复的字面 \\n')
  process.exit(0)
}
// 保护正则中的 \n（形如 /\n/ 或 \n+ 之类出现在正则字面量里的）
s = s.replace(/\/\\n\+?\//g, (m) => m.replace('\\n', '__KEEP_NL__'))
const before = (s.match(/\\n/g) || []).length
s = s.replace(/\\n/g, '\n')
s = s.replace(/__KEEP_NL__/g, '\\n')
fs.writeFileSync(file, s)
console.log(`修复字面 \\n：${before} 处`)
