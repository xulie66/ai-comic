<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useAppStore } from '@/stores/app'
import {
  adminUsers, auth, availableModels, credits, setAdmin, setDefaultModel, setQuota, unifiedModels, usage,
  type AdminUserRow
} from '@/api/endpoints'

const store = useAppStore()
const startTab = new URLSearchParams(location.search).get('tab')
const tab = ref<'api' | 'account'>(startTab === 'account' ? 'account' : 'api')

async function refreshMe() {
  try {
    await store.loadMe()
    ElMessage.success('已刷新')
  } catch (err: any) {
    ElMessage.error(err?.message || '刷新失败')
  }
}

// 接口 / Key / 模型名全部由后台统一下发，前台只保留"能力开关"
const form = reactive({
  imageSupportsReferences: true
})
const saving = ref(false)

const pwd = reactive({ oldPassword: '', newPassword: '', confirm: '' })
const changingPwd = ref(false)

/* ---------- 用量 / 额度 ---------- */
const myUsage = reactive({ day: '', pages: 0, images: 0, storyboards: 0, textCalls: 0 })
const myLimits = reactive({ isAdmin: false, unlimited: false, dailyPages: 0, pagesPerMinute: 0 })
const myRemaining = ref(0)
const adminRows = ref<AdminUserRow[]>([])
const adminDefaults = reactive({ dailyPages: 80, pagesPerMinute: 4 })

const quotaPct = computed(() => {
  if (myLimits.unlimited || !myLimits.dailyPages) return 0
  return Math.min(100, Math.round((myUsage.pages / myLimits.dailyPages) * 100))
})

async function loadUsage() {
  try {
    const res = await usage()
    Object.assign(myUsage, res.usage || {})
    Object.assign(myLimits, res.limits || {})
    myRemaining.value = Number(res.remaining ?? 0)
  } catch { /* 未登录或无接口时忽略 */ }
}

async function loadAdmin() {
  if (!store.user?.isAdmin) { adminRows.value = []; return }
  try {
    const res = await adminUsers()
    adminRows.value = res.users || []
    if (res.defaults) Object.assign(adminDefaults, res.defaults)
  } catch { /* 非管理员会 403，忽略 */ }
}

/* ---------- 积分（充值/兑换在「钱包」页） ---------- */
const myCredits = reactive({ balance: 0, enabled: false, status: 'active' })

async function loadCredits() {
  try {
    const res = await credits()
    myCredits.balance = Number(res.balance || 0)
    myCredits.enabled = Boolean(res.enabled)
    myCredits.status = res.status || 'active'
  } catch { /* 未登录时忽略 */ }
}

/* ---------- 统一模型（后台下发，前台只读） ---------- */
const unifiedText = ref('')
const unifiedImage = ref('')
const unifiedTextMode = ref('')
const unifiedImageMode = ref('')
const available = ref<any[]>([])
const defaultSaving = ref('')
const defaultModels = reactive({ image: '', text: '' })

function billingLabel(mode: string) {
  if (mode === 'token') return '计费：按 token（每百万 token 单价）'
  if (mode === 'unit') return '计费：按次调用'
  return ''
}

async function loadUnified() {
  try {
    const res = await unifiedModels()
    unifiedText.value = res.text ? `${res.text.modelKey}` : ''
    unifiedImage.value = res.image ? `${res.image.modelKey}` : ''
    unifiedTextMode.value = res.text?.billingMode || ''
    unifiedImageMode.value = res.image?.billingMode || ''
    defaultModels.image = res.image?.id || defaultModels.image
    defaultModels.text = res.text?.id || defaultModels.text
  } catch { /* 后台没配置时留空 */ }
}
async function loadAvailableModels() {
  try { const res = await availableModels(); available.value = res.models || []; if (!defaultModels.image) defaultModels.image = res.defaults?.image || ''; if (!defaultModels.text) defaultModels.text = res.defaults?.text || '' } catch { available.value = [] }
}
async function changeDefaultModel(capability: 'image' | 'text', value: string) {
  defaultSaving.value = capability
  try { await setDefaultModel(capability, value); await loadUnified(); ElMessage.success('默认模型已切换') }
  catch (err: any) { ElMessage.error(err?.message || '默认模型切换失败') }
  finally { defaultSaving.value = '' }
}

async function changeQuota(row: AdminUserRow, value: number | undefined) {
  const quota = Number(value)
  if (!Number.isFinite(quota)) return
  try {
    const res = await setQuota(row.id, quota)
    Object.assign(row, res.user)
    ElMessage.success(`${row.username} 的额度改成 ${quota === 0 ? '默认' : quota + ' 页/天'}`)
  } catch (err: any) {
    ElMessage.error(err?.message || '改额度失败')
    await loadAdmin()
  }
}

async function toggleAdmin(row: AdminUserRow, value: boolean) {
  try {
    const res = await setAdmin(row.id, value)
    Object.assign(row, res.user)
    ElMessage.success(`${row.username} ${value ? '已设为管理员' : '已取消管理员'}`)
  } catch (err: any) {
    ElMessage.error(err?.message || '操作失败')
    await loadAdmin()
  }
}

onMounted(async () => {
  if (!store.ready) { try { await store.load() } catch { /* 忽略 */ } }
  void loadUsage()
  void loadAdmin()
  void loadCredits()
  void loadUnified()
  void loadAvailableModels()
  Object.assign(form, {
    imageSupportsReferences: store.settings.imageSupportsReferences !== false
  })
})

async function save() {
  saving.value = true
  try {
    // 只回写"能力开关"，接口/Key/模型由后台统一配置，前台不再覆盖
    await store.saveSettings({ imageSupportsReferences: form.imageSupportsReferences })
    ElMessage.success('设置已保存')
  } catch (err: any) {
    ElMessage.error(err?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function changePassword() {
  if (!pwd.oldPassword || !pwd.newPassword) { ElMessage.warning('当前密码和新密码都要填'); return }
  if (pwd.newPassword !== pwd.confirm) { ElMessage.warning('两次输入的新密码不一致'); return }
  if (pwd.newPassword.length < 6) { ElMessage.warning('新密码至少 6 位'); return }
  changingPwd.value = true
  try {
    await auth.changePassword(pwd.oldPassword, pwd.newPassword)
    ElMessage.success('密码已修改')
    pwd.oldPassword = ''
    pwd.newPassword = ''
    pwd.confirm = ''
  } catch (err: any) {
    ElMessage.error(err?.message || '修改失败')
  } finally {
    changingPwd.value = false
  }
}

function mask(key: string) {
  if (!key) return ''
  return key.length <= 8 ? '****' : key.slice(0, 4) + '****' + key.slice(-4)
}
</script>

<template>
  <div>
    <div class="page-head">
      <h1>设置</h1>
      <p class="muted">管理默认模型、参考图偏好与个人账号。</p>
    </div>

    <el-tabs v-model="tab">
      <el-tab-pane label="接口设置" name="api">
        <section class="panel">
          <div class="section-head">
            <h3>统一模型</h3>
            <span class="muted">由管理员在后台「模型与渠道」统一配置，选择适合自己的默认模型</span>
          </div>

          <el-alert
            type="info"
            show-icon
            :closable="false"
            title="模型接口、Key、模型名、计费方式统一由后台下发"
            description="前台不再填写接口地址 / Key / 模型名，避免一份配置两个地方。需要换模型、改价格、按次或按 token 计费，都请管理员在后台操作；这里可选择当前账号的默认模型。"
            style="margin-bottom: 14px"
          />

          <div class="unified-grid">
            <div class="unified-card">
              <div class="muted">文本模型（拆分镜 / 提取角色场景）</div>
              <b>{{ unifiedText || '后台尚未指定' }}</b>
              <small class="muted">{{ billingLabel(unifiedTextMode) }}</small>
            </div>
            <div class="unified-card">
              <div class="muted">图片模型（逐页出图 / 设定图）</div>
              <b>{{ unifiedImage || '后台尚未指定' }}</b>
              <small class="muted">{{ billingLabel(unifiedImageMode) }}</small>
            </div>
          </div>

          <div class="default-model-box">
            <div class="section-head"><h3>我的默认模型</h3><span class="muted">仅影响当前账号，可随时切换后台已启用模型</span></div>
            <div class="default-model-grid">
              <el-form-item label="默认文本模型">
                <el-select v-model="defaultModels.text" filterable :loading="defaultSaving === 'text'" placeholder="选择文本模型" style="width:100%" @change="(value) => changeDefaultModel('text', value)">
                  <el-option v-for="item in available.filter((model) => model.capability === 'text')" :key="item.id" :label="`${item.displayName || item.modelKey} · ${item.channelName}`" :value="item.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="默认图片模型">
                <el-select v-model="defaultModels.image" filterable :loading="defaultSaving === 'image'" placeholder="选择图片模型" style="width:100%" @change="(value) => changeDefaultModel('image', value)">
                  <el-option v-for="item in available.filter((model) => model.capability === 'image')" :key="item.id" :label="`${item.displayName || item.modelKey} · ${item.channelName}`" :value="item.id" />
                </el-select>
              </el-form-item>
            </div>
          </div>
          <div class="divider" />
          <div class="section-head"><h3>参考图（角色 / 场景设定图）</h3><span class="muted">关掉以后只按文字画，人物和场景会不一致</span></div>
          <el-form label-position="top">
            <el-form-item label="是否支持图像输入">
              <el-switch v-model="form.imageSupportsReferences" active-text="支持" inactive-text="不支持" />
            </el-form-item>
          </el-form>
          <div class="row">
            <el-button type="primary" :loading="saving" @click="save">保存设置</el-button>
          </div>
        </section>
      </el-tab-pane>

      <el-tab-pane label="账号" name="account">
        <div class="account-banner">
          <div class="account-avatar">{{ (store.user?.username || 'U').slice(0, 1).toUpperCase() }}</div>
          <div><h2>{{ store.user?.username || '我的账号' }}</h2><p class="muted">个人资料、使用额度与账户安全</p></div>
          <el-tag effect="plain">{{ store.user?.isAdmin ? '管理员' : '创作者' }}</el-tag>
        </div>
        <div class="account-grid">
        <section class="panel account-card">
          <div class="section-head">
            <h3>今日额度</h3>
            <span class="muted">每天 {{ myLimits.unlimited ? '不限' : myLimits.dailyPages + ' 页' }} · 每分钟 {{ myLimits.unlimited ? '不限' : myLimits.pagesPerMinute + ' 页' }}</span>
          </div>
          <div class="quota-line">
            <b>{{ myUsage.pages }}</b>
            <span class="muted">/ {{ myLimits.unlimited ? '不限' : myLimits.dailyPages }} 页（今天已出）</span>
            <span v-if="!myLimits.unlimited" class="quota-left" :class="{ low: myRemaining <= 5 }">
              还剩 {{ myRemaining }} 页
            </span>
            <span v-else class="quota-left">管理员不限量</span>
          </div>
          <div v-if="!myLimits.unlimited" class="quota-bar">
            <i :style="{ width: quotaPct + '%' }" />
          </div>
          <p class="muted" style="margin: 10px 0 0; font-size: 12.5px">
            分镜拆分 {{ myUsage.storyboards }} 次 · 文本调用 {{ myUsage.textCalls }} 次 · 累计出图 {{ store.user?.usage?.pages ?? 0 }} 页。
            额度每天 0 点（服务器时区）重置；出图失败不扣额度。
          </p>
        </section>

        <section class="panel account-card">
          <div class="section-head">
            <h3>积分</h3>
            <span class="muted">{{ myCredits.enabled ? '按模型调用扣费：成功才扣，失败不扣' : '当前未开启积分计费' }}</span>
          </div>
          <div class="quota-line">
            <b>{{ myCredits.balance }}</b>
            <span class="muted">积分余额</span>
            <span v-if="myCredits.status === 'banned'" class="quota-left low">账号已停用</span>
          </div>
          <div class="row" style="margin-top: 12px; gap: 8px; align-items: center">
            <el-button size="small" type="primary" @click="$router.push('/wallet')">去钱包充值</el-button>
            <el-button size="small" text @click="loadCredits">刷新</el-button>
          </div>
          <p class="muted" style="margin: 10px 0 0; font-size: 12.5px">
            充值、订单和扣费流水都在 <a href="#/wallet">钱包</a> 页。
            <template v-if="store.user?.isAdmin"> 你是管理员，可以进 <a href="/admin/" target="_blank">管理后台</a> 发积分 / 生成兑换码 / 调价。</template>
          </p>
        </section>

        <section class="panel account-card">
          <div class="section-head">
            <h3>用量</h3>
            <span class="muted">这账号累计调用次数</span>
          </div>
          <div class="usage-grid">
            <div class="usage-cell">
              <b>{{ store.user?.usage?.storyboards ?? 0 }}</b><span>拆分镜</span>
            </div>
            <div class="usage-cell">
              <b>{{ store.user?.usage?.pages ?? 0 }}</b><span>出图页数</span>
            </div>
            <div class="usage-cell">
              <b>{{ store.user?.usage?.images ?? 0 }}</b><span>张图（含设定图）</span>
            </div>
            <div class="usage-cell">
              <b>{{ store.user?.usage?.characters ?? 0 }}</b><span>提取角色</span>
            </div>
          </div>
          <div class="row" style="margin-top: 12px">
            <span class="muted" style="font-size: 12.5px">
              账号：{{ store.user?.username || '—' }}
              <template v-if="store.user?.lastLoginAt"> · 上次登录 {{ new Date(store.user.lastLoginAt).toLocaleString('zh-CN') }}</template>
            </span>
            <div class="grow" />
            <el-button size="small" text @click="refreshMe">刷新</el-button>
          </div>
        </section>

        <section v-if="store.user?.isAdmin" class="panel account-card account-admin">
          <div class="section-head">
            <h3>账号与额度（管理员）</h3>
            <span class="muted">每人每天能出多少页；0 = 用全局默认 {{ adminDefaults.dailyPages }} 页 / 分钟 {{ adminDefaults.pagesPerMinute }} 页</span>
          </div>
          <el-table :data="adminRows" size="small" style="width: 100%">
            <el-table-column label="账号" min-width="150">
              <template #default="{ row }">
                <b>{{ row.username }}</b>
                <el-tag v-if="row.isAdmin" size="small" type="warning" style="margin-left: 6px">管理员</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="今天已出" width="100">
              <template #default="{ row }">{{ row.usage?.pages ?? 0 }} 页</template>
            </el-table-column>
            <el-table-column label="每日额度" width="180">
              <template #default="{ row }">
                <el-input-number
                  :model-value="row.isAdmin ? 0 : row.limits.dailyPages"
                  :min="0"
                  :max="2000"
                  :step="10"
                  size="small"
                  :disabled="row.isAdmin"
                  @change="(v: number) => changeQuota(row, v)"
                />
                <span class="muted" style="margin-left: 6px; font-size: 12px">{{ row.isAdmin ? '不限' : '页/天' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="累计" width="90">
              <template #default="{ row }">{{ row.totals?.pages ?? 0 }} 页</template>
            </el-table-column>
            <el-table-column label="管理员" width="90">
              <template #default="{ row }">
                <el-switch
                  :model-value="row.isAdmin"
                  size="small"
                  @change="(v: any) => toggleAdmin(row, Boolean(v))"
                />
              </template>
            </el-table-column>
          </el-table>
          <p class="muted" style="margin: 10px 0 0; font-size: 12.5px">
            管理员不受额度限制，也能改别人的额度。邀请码在服务器环境变量 <code>AI_COMIC_INVITE_CODE</code> 里。
          </p>
        </section>

        <section class="panel account-card">
          <div class="section-head"><h3>修改密码</h3></div>
          <el-form label-position="top">
            <el-form-item label="当前密码">
              <el-input v-model="pwd.oldPassword" type="password" show-password autocomplete="current-password" />
            </el-form-item>
            <el-form-item label="新密码（至少 6 位）">
              <el-input v-model="pwd.newPassword" type="password" show-password autocomplete="new-password" />
            </el-form-item>
            <el-form-item label="再输一遍">
              <el-input v-model="pwd.confirm" type="password" show-password autocomplete="new-password" />
            </el-form-item>
          </el-form>
          <el-button type="primary" :loading="changingPwd" @click="changePassword">保存新密码</el-button>
          <p class="muted" style="margin: 14px 0 0; font-size: 12.5px">
            账号：{{ store.user?.username || '—' }}。忘记密码可以在账号页改，或用服务器上的数据库直接重置。
          </p>
        </section>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped>
.account-banner { display:flex; align-items:center; gap:16px; padding:22px 26px; border:1px solid var(--line); border-radius:16px; background:var(--surface); margin-bottom:20px; }
.account-avatar { width:56px; height:56px; border-radius:16px; display:grid; place-items:center; font-size:25px; font-weight:700; background:var(--surface-2); color:var(--accent); }
.account-banner h2 { margin:0 0 4px; font-size:20px; }
.account-banner p { margin:0; font-size:13px; }
.account-banner .el-tag { margin-left:auto; }
.account-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:20px; align-items:stretch; }
.account-card { min-width:0; margin:0; padding:22px; }
.account-admin { grid-column:1/-1; order:5; }
.account-grid .section-head { flex-wrap:wrap; gap:8px; }
.account-grid .section-head .muted { font-size:12px; }
@media(max-width:900px) { .account-grid { grid-template-columns:1fr; } .usage-grid { grid-template-columns:repeat(2,1fr); } }
.page-head { margin-bottom: 16px; }
.page-head h1 { margin: 0; font-size: 22px; font-weight: 700; }
.page-head p { margin: 6px 0 0; font-size: 12.5px; }
.test-out {
  margin-top: 14px; max-height: 260px; overflow: auto;
  background: var(--surface-2); padding: 12px; border-radius: 10px; border: 1px solid var(--line-soft);
}
.unified-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 10px; }
.unified-card { border: 1px solid var(--line, rgba(130,140,170,.25)); border-radius: 12px; padding: 12px 14px; display: flex; flex-direction: column; gap: 4px; }
.unified-card b { font-size: 15px; }
.unified-card small { font-size: 12px; }
.usage-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
.usage-cell {
  display: flex; flex-direction: column; gap: 3px; padding: 12px 14px;
  border-radius: 10px; background: var(--surface-2); border: 1px solid var(--line);
}
.usage-cell b { font-size: 20px; font-weight: 640; letter-spacing: -.5px; font-variant-numeric: tabular-nums; }
.usage-cell span { font-size: 11.5px; color: var(--text-3); }

.quota-line { display: flex; align-items: baseline; gap: 8px; }
.quota-line b { font-size: 26px; font-weight: 680; letter-spacing: -.8px; font-variant-numeric: tabular-nums; }
.quota-left {
  margin-left: auto; font-size: 12.5px; color: var(--text-3);
  padding: 2px 9px; border-radius: 999px; background: var(--surface-2); border: 1px solid var(--line);
}
.quota-left.low { color: #c2410c; border-color: color-mix(in srgb, #c2410c 40%, transparent); }
.quota-bar {
  margin-top: 8px; height: 6px; border-radius: 999px; overflow: hidden;
  background: var(--surface-2); border: 1px solid var(--line);
}
.quota-bar i { display: block; height: 100%; background: var(--accent); border-radius: 999px; transition: width .25s ease; }
</style>
