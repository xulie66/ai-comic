// 修复被上一个脚本拆断的字符串字面量：v.join('\n') 被拆成了两行。
import fs from 'node:fs'

const file = process.argv[2]
let s = fs.readFileSync(file, 'utf8')
const nl = String.fromCharCode(10)
const broken = "return v.join('" + nl + "')"
const fixed = "return v.join('" + '\\' + "n')"
if (!s.includes(broken)) {
  console.log('没找到被拆断的 join，跳过')
  process.exit(0)
}
s = s.split(broken).join(fixed)
fs.writeFileSync(file, s)
console.log('已修复 v.join(换行) 的拆分')
