// 构建后把 admin.html 改名成 index.html：/admin/ 才能直接打开，子路由刷新也能回落到它。
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const outDir = fileURLToPath(new URL('../../web/public/admin', import.meta.url));
const from = path.join(outDir, 'admin.html');
const to = path.join(outDir, 'index.html');
if (fs.existsSync(from)) {
  fs.rmSync(to, { force: true });
  fs.renameSync(from, to);
  console.log(`[admin] 入口产物：${to}`);
} else if (fs.existsSync(to)) {
  console.log('[admin] 入口已经是 index.html');
} else {
  console.error('[admin] 没找到构建产物，检查 vite.admin.config.ts 的 outDir');
  process.exit(1);
}
