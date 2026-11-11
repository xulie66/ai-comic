<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useAppStore } from '@/stores/app'
import { credits, payCreate, payPackages, payStatus, redeem } from '@/api/endpoints'
import { getToken } from '@/api/http'

const store = useAppStore()

type Pack = {
  id: string; title: string; amountYuan: number; bonusPercent: number; credits: number; note: string
}
type Order = {
  id: string; outTradeNo: string; amountYuan: number; credits: number; status: string
  payType: string; createdAt: string; paidAt?: string | null
}

const loading = ref(true)
const balance = ref(0)
const enabled = ref(false)
const note = ref('')
const packages = ref<Pack[]>([])
const payTypes = ref<string[]>(['alipay', 'wxpay'])
const payType = ref('alipay')
const canRecharge = ref(false)
const redeemEnabled = ref(false)
const historyTab = ref('orders')
const manualNote = ref('')
const balanceError = ref('')

const ledger = ref<Array<{ delta: number; kind: string; note: string; balanceAfter: number; at: string }>>([])
const orders = ref<Order[]>([])

const selected = ref('')
const payBusy = ref(false)
const pending = reactive<{ id: string; outTradeNo: string; url: string; mode: string }>({ id: '', outTradeNo: '', url: '', mode: '' })
const redeemForm = reactive({ code: '', busy: false })

const KIND_LABEL: Record<string, string> = {
  recharge: '充值', redeem: '兑换', consume: '模型调用', refund: '退款', gift: '赠送',
  adjust: '人工调整', status: '账号状态', signup_bonus: '注册赠送'
}

let pollTimer: number | undefined

const payTypeLabel = (value: string) => ({ alipay: '支付宝', wxpay: '微信支付', qqpay: 'QQ 钱包', usdt: 'USDT' } as Record<string, string>)[value] || value

const selectedPack = computed(() => packages.value.find((item) => item.id === selected.value) || null)

function fmt(value: number) {
  return Number(value || 0).toLocaleString('zh-CN', { maximumFractionDigits: 6 })
}

function fmtTime(value?: string | null) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
}

async function loadWallet() {
  loading.value = true
  try {
    const res = await credits()
    balance.value = Number(res.balance || 0)
    enabled.value = Boolean(res.enabled)
    note.value = res.note || ''
    ledger.value = res.ledger || []
    orders.value = (res.orders as Order[]) || []
    canRecharge.value = Boolean(res.canRecharge)
    redeemEnabled.value = res.redeemEnabled === true
    balanceError.value = ''
  } catch (err: any) {
    balanceError.value = err?.message || '钱包加载失败'
  } finally {
    loading.value = false
  }
}

async function loadPackages() {
  try {
    const res = await payPackages()
    packages.value = res.packages || []
    if (res.payTypes && res.payTypes.length) {
      payTypes.value = res.payTypes
      if (!payTypes.value.includes(payType.value)) payType.value = payTypes.value[0]
    }
    canRecharge.value = canRecharge.value && Boolean(res.enabled)
    manualNote.value = res.manualNote || ''
    if (!selected.value && packages.value.length) selected.value = packages.value[0].id
  } catch (err: any) {
    ElMessage.warning(err?.message || '套餐加载失败')
  }
}

async function startPay() {
  const pack = selectedPack.value
  if (!pack) { ElMessage.warning('先选一个套餐'); return }
  payBusy.value = true
  try {
    const res = await payCreate({ packageId: pack.id, payType: payType.value })
    pending.id = res.id
    pending.outTradeNo = res.outTradeNo
    pending.url = res.checkoutUrl || ''
    pending.mode = res.mode
    if (res.mode === 'qr_code') {
      // 二维码模式：单一收银台弹窗，只展示当前选中的支付渠道
      qrCards.value = []
      qrVisible.value = true
      await addQrCard(payType.value, res, pack.title)
      startCountdown(res.expiresInMinutes)
      ElMessage.success(`订单已创建：${res.amountYuan} 元 / ${res.credits} 积分，扫码即可支付`)
    } else {
      ElMessage.success(`订单已创建：${res.amountYuan} 元 / ${res.credits} 积分，正在打开收银台`)
      window.location.href = res.checkoutUrl
    }
    startPoll()
  } catch (err: any) {
    ElMessage.error(err?.message || '下单失败')
  } finally {
    payBusy.value = false
  }
}

type QrCard = {
  payType: string; id: string; outTradeNo: string; payload: string; image: string; error: string; paid: boolean
  productTitle: string; amountYuan: number; expiresAt: number
}
const qrCards = ref<QrCard[]>([])
const qrVisible = ref(false)
const qrSecondsLeft = ref(0)
let countdownTimer: number | undefined

/** 品牌色（都够深，扫得出来）：支付宝深蓝 / 微信深绿 / 默认墨黑 */
const QR_COLOR = '#000000'

async function addQrCard(type: string, res: { id: string; outTradeNo: string; qrPayload?: string; checkoutUrl: string; amountYuan: number; expiresInMinutes?: number }, productTitle: string) {
  const payload = res.qrPayload || res.checkoutUrl || ''
  const minutes = Number(res.expiresInMinutes || 30)
  const card: QrCard = {
    payType: type, id: res.id, outTradeNo: res.outTradeNo, payload, image: '', error: '', paid: false,
    productTitle, amountYuan: Number(res.amountYuan || 0), expiresAt: Date.now() + minutes * 60 * 1000
  }
  qrCards.value.push(card)
  try {
    card.image = await qrDataUrl(payload, QR_COLOR)
  } catch (err: any) {
    card.error = err?.message || '二维码生成失败'
  }
}

/** 二维码 SVG 走鉴权请求拿回来，转 data URI 给 <img>（<img> 带不了 Bearer 头） */
async function qrDataUrl(text: string, color = '#101528') {
  const query = `/api/qr.svg?size=300&margin=4&color=${encodeURIComponent(color)}&text=${encodeURIComponent(text)}`
  const res = await fetch(query, {
    headers: { Authorization: 'Bearer ' + getToken() },
    credentials: 'same-origin'
  })
  if (!res.ok) {
    const raw = await res.text()
    let message = `HTTP ${res.status}`
    try { message = JSON.parse(raw).error || message } catch { /* 保留默认 */ }
    throw new Error(message)
  }
  const svg = await res.text()
  return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
}

function startCountdown(minutes?: number) {
  if (countdownTimer) window.clearInterval(countdownTimer)
  const fallback = Number(minutes || 30) * 60
  const expiresAt = qrCards.value[0]?.expiresAt || Date.now() + fallback * 1000
  const tick = () => {
    qrSecondsLeft.value = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000))
    if (!qrSecondsLeft.value && countdownTimer) {
      window.clearInterval(countdownTimer)
      countdownTimer = undefined
    }
  }
  tick()
  countdownTimer = window.setInterval(tick, 1000) as unknown as number
}

const countdownText = computed(() => {
  const seconds = Math.max(0, qrSecondsLeft.value)
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`
})

function closeQr() {
  qrVisible.value = false
  stopPoll()
  if (countdownTimer) { window.clearInterval(countdownTimer); countdownTimer = undefined }
}

function startPoll() {
  stopPoll()
  let tries = 0
  pollTimer = window.setInterval(async () => {
    tries += 1
    try {
      const targets = qrCards.value.length
        ? qrCards.value.filter((card) => !card.paid)
        : [{ id: pending.id, outTradeNo: pending.outTradeNo, paid: false } as QrCard]
      for (const target of targets) {
        const res = await payStatus({ id: target.id, out_trade_no: target.outTradeNo })
        if (res.order?.status === 'paid') {
          stopPoll()
          if (qrCards.value.length) {
            qrCards.value.forEach((card) => { card.paid = card.outTradeNo === target.outTradeNo })
          }
          balance.value = Number(res.balance || 0)
          ElMessage.success(`到账 ${res.order.credits} 积分，当前余额 ${fmt(res.balance)}`)
          await loadWallet()
          // 先让用户看到“已到账”，再自动收起收银台
          window.setTimeout(() => {
            closeQr()
          }, 1000)
          return
        }
      }
    } catch { /* 继续轮询 */ }
    if (tries > 100) stopPoll()
  }, 3000) as unknown as number
}

function stopPoll() {
  if (pollTimer) { window.clearInterval(pollTimer); pollTimer = undefined }
}

async function submitRedeem() {
  const code = redeemForm.code.trim()
  if (!code) { ElMessage.warning('请输入兑换码'); return }
  redeemForm.busy = true
  try {
    const res = await redeem(code)
    redeemForm.code = ''
    ElMessage.success(`兑换成功 +${fmt(res.credits)} 积分，余额 ${fmt(res.balance)}`)
    await loadWallet()
  } catch (err: any) {
    ElMessage.error(err?.message || '兑换失败')
  } finally {
    redeemForm.busy = false
  }
}

onMounted(async () => {
  if (!store.ready) { try { await store.load() } catch { /* 忽略 */ } }
  await Promise.all([loadWallet(), loadPackages()])
  const params = new URLSearchParams(location.search)
  if (params.get('pay') === 'return') {
    const outTradeNo = params.get('out_trade_no') || ''
    if (outTradeNo) {
      pending.outTradeNo = outTradeNo
      void payStatus({ out_trade_no: outTradeNo }).then((res: any) => {
        if (res?.order?.id) { pending.id = res.order.id; startPoll() }
      }).catch(() => { /* 忽略 */ })
    }
  }
})

onBeforeUnmount(stopPoll)
</script>

<template>
  <div class="wallet-page">
    <div class="wallet-head">
      <div>
        <h2>钱包</h2>
        <p class="muted">
          积分按模型调用扣费：<b>调用成功才扣，调用失败不扣</b>。
          {{ enabled ? '当前已开启积分计费。' : '当前未开启积分计费（出图不扣积分）。' }}
        </p>
      </div>
      <div class="wallet-balance">
        <span class="muted">余额</span>
        <b>{{ fmt(balance) }}</b>
        <span class="muted">积分</span>
      </div>
    </div>

    <el-alert v-if="balanceError" :title="balanceError" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
    <el-alert v-if="note" :title="note" type="info" show-icon :closable="false" style="margin-bottom: 12px" />

    <section class="panel" style="margin-bottom: 16px">
      <div class="section-head">
        <h3>充值</h3>
        <span class="muted">{{ canRecharge ? '易支付：支付宝 / 微信，扫码或跳转支付后自动到账' : (redeemEnabled ? '在线充值未开启，可用兑换码或联系管理员' : '在线充值未开启，请联系管理员') }}</span>
      </div>

      <div class="pack-grid">
        <button
          v-for="pack in packages"
          :key="pack.id"
          class="pack-card"
          :class="{ active: selected === pack.id }"
          type="button"
          @click="selected = pack.id"
        >
          <div class="pack-title">{{ pack.title }}</div>
          <div class="pack-price">¥{{ pack.amountYuan }}</div>
          <div class="pack-credits">{{ fmt(pack.credits) }} 积分</div>
          <div v-if="pack.bonusPercent" class="pack-bonus">+{{ pack.bonusPercent }}%</div>
          <div v-if="pack.note" class="muted pack-note">{{ pack.note }}</div>
        </button>
      </div>

      <div class="row" style="margin-top: 14px; gap: 10px; align-items: center; flex-wrap: wrap">
        <el-radio-group v-model="payType" size="small">
          <el-radio-button v-for="type in payTypes" :key="type" :value="type">{{ payTypeLabel(type) }}</el-radio-button>
        </el-radio-group>
        <el-button type="primary" size="small" :loading="payBusy" :disabled="!canRecharge || !selected" @click="startPay">
          去支付
        </el-button>
        <span v-if="selectedPack" class="muted" style="font-size: 12.5px">
          应付 ¥{{ selectedPack.amountYuan }}，到账 {{ fmt(selectedPack.credits) }} 积分
        </span>
      </div>

      <el-dialog
        v-model="qrVisible"
        class="pay-dialog"
        width="450px"
        top="6vh"
        :show-close="true"
        destroy-on-close
        @closed="closeQr"
      >
        <template #header>
          <div class="pay-dialog-title">扫码支付</div>
        </template>
        <div v-if="qrCards.length" class="pay-checkout">
          <div class="pay-product">{{ qrCards[0].productTitle }}</div>
          <div class="pay-amount"><span>¥</span>{{ qrCards[0].amountYuan.toFixed(2) }}</div>
          <div class="pay-countdown" :class="{ expired: !qrSecondsLeft }">
            {{ qrSecondsLeft ? `请在 ${countdownText} 内完成支付` : '订单已超时，请重新下单' }}
          </div>
          <img v-if="qrCards[0].image" :src="qrCards[0].image" alt="支付二维码" class="qr-img pay-qr-img" />
          <div v-else class="qr-fallback pay-qr-img">{{ qrCards[0].error || '二维码生成中…' }}</div>
          <div v-if="qrCards[0].paid" class="pay-paid">已到账</div>
          <div class="pay-order">订单号：{{ qrCards[0].outTradeNo }}</div>
        </div>
      </el-dialog>

      <div v-if="pending.url && !qrVisible" class="pay-hint">
        收银台没自动打开？<a :href="pending.url" target="_blank" rel="noopener">点这里继续支付</a>
        <span class="muted"> · 订单 {{ pending.outTradeNo }}，支付完成后余额会自动刷新</span>
      </div>
      <p v-if="manualNote" class="muted" style="margin: 10px 0 0; font-size: 12.5px">{{ manualNote }}</p>
    </section>

    <section v-if="redeemEnabled" class="panel" style="margin-bottom: 16px; max-width: 620px">
      <div class="section-head"><h3>兑换码</h3><span class="muted">一码一用，用过即失效</span></div>
      <div class="row" style="gap: 8px; align-items: center">
        <el-input v-model="redeemForm.code" placeholder="例如 MANGA-XXXXXXXXXX" size="small" style="max-width: 280px" @keyup.enter="submitRedeem" />
        <el-button size="small" type="primary" :loading="redeemForm.busy" @click="submitRedeem">兑换</el-button>
      </div>
    </section>

    <section class="panel" style="margin-bottom: 16px">
      <el-tabs v-model="historyTab">
        <el-tab-pane label="充值订单" name="orders">
          <div class="section-head"><span class="muted">最近 10 笔</span></div>
          <el-table :data="orders" size="small" empty-text="还没有订单" style="width: 100%">
            <el-table-column label="时间" width="170"><template #default="{ row }">{{ fmtTime(row.createdAt) }}</template></el-table-column>
            <el-table-column prop="outTradeNo" label="订单号" min-width="200" show-overflow-tooltip />
            <el-table-column label="金额" width="90"><template #default="{ row }">¥{{ row.amountYuan }}</template></el-table-column>
            <el-table-column label="积分" width="110"><template #default="{ row }">{{ fmt(row.credits) }}</template></el-table-column>
            <el-table-column label="方式" width="90"><template #default="{ row }">{{ payTypeLabel(row.payType || '') }}</template></el-table-column>
            <el-table-column label="状态" width="90"><template #default="{ row }"><span :style="{ color: row.status === 'paid' ? '#1f9d55' : row.status === 'pending' ? '#b7791f' : '#c53030' }">{{ ({ paid: '已到账', pending: '待支付', failed: '失败', refunded: '已退款' } as Record<string, string>)[row.status] || row.status }}</span></template></el-table-column>
            <el-table-column label="到账时间" width="170"><template #default="{ row }">{{ fmtTime(row.paidAt) }}</template></el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="积分流水" name="ledger">
          <div class="section-head"><span class="muted">最近 20 条 · 扣费失败不记流水</span></div>
          <el-table :data="ledger" size="small" :empty-text="loading ? '加载中…' : '还没有流水'" style="width: 100%">
            <el-table-column label="时间" width="170"><template #default="{ row }">{{ fmtTime(row.at) }}</template></el-table-column>
            <el-table-column label="类型" width="110"><template #default="{ row }">{{ KIND_LABEL[row.kind] || row.kind }}</template></el-table-column>
            <el-table-column label="变动" width="120"><template #default="{ row }"><span :style="{ color: row.delta >= 0 ? '#1f9d55' : '#c53030' }">{{ row.delta >= 0 ? '+' : '' }}{{ fmt(row.delta) }}</span></template></el-table-column>
            <el-table-column label="余额" width="110"><template #default="{ row }">{{ fmt(row.balanceAfter) }}</template></el-table-column>
            <el-table-column prop="note" label="说明" min-width="240" show-overflow-tooltip />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </section>
  </div>
</template>

<style scoped>
:deep(.el-dialog.pay-dialog) {
  --el-dialog-bg-color: rgba(255, 255, 255, .78);
  background: rgba(255, 255, 255, .78) !important;
  border: 1px solid rgba(255, 255, 255, .72) !important;
  border-radius: 18px;
  box-shadow: 0 24px 80px rgba(16, 21, 40, .22);
  backdrop-filter: blur(18px);
  color: #182033 !important;
  opacity: 1 !important;
}
:deep(.el-dialog.pay-dialog .el-dialog__header) {
  margin-right: 0;
  padding: 18px 22px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, .12);
  color: #182033 !important;
  opacity: 1 !important;
}
:deep(.el-dialog.pay-dialog .el-dialog__headerbtn) { top: 15px; right: 16px; }
:deep(.el-dialog.pay-dialog .el-dialog__headerbtn .el-dialog__close) { color: rgba(24, 32, 51, .72) !important; }
:deep(.el-dialog.pay-dialog .el-dialog__body) {
  padding: 16px 22px 22px;
  background: transparent !important;
  color: #182033 !important;
  opacity: 1 !important;
}
:deep(.el-dialog.pay-dialog .el-dialog__footer) { background: transparent !important; }
.wallet-page { padding: 20px 22px 40px; max-width: 1100px; }
.wallet-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.wallet-head h2 { margin: 0 0 4px; font-size: 20px; }
.wallet-head p { margin: 0; font-size: 12.5px; }
.wallet-balance { display: flex; align-items: baseline; gap: 6px; padding: 10px 16px; border-radius: 12px; background: var(--panel-2, rgba(120,130,160,.08)); }
.wallet-balance b { font-size: 24px; }
.pack-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 10px; }
.pack-card { text-align: left; border: 1px solid var(--line, rgba(130,140,170,.25)); border-radius: 12px; padding: 12px 14px; background: transparent; cursor: pointer; transition: border-color .15s, transform .15s; }
.pack-card:hover { transform: translateY(-1px); }
.pack-card.active { border-color: #5b6cf9; box-shadow: 0 0 0 1px #5b6cf9 inset; }
.pack-title { font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.pack-price { font-size: 20px; font-weight: 700; }
.pack-credits { font-size: 12.5px; color: #5b6cf9; margin-top: 2px; }
.pack-bonus { font-size: 12px; color: #1f9d55; margin-top: 2px; }
.pack-note { margin-top: 4px; font-size: 11.5px; }
.pay-hint { margin-top: 12px; font-size: 12.5px; }
.pay-dialog-title { font-size: 17px; font-weight: 700; text-align: center; }
.pay-checkout { text-align: center; padding: 0 4px 12px; }
.pay-product { color: #4d5870; font-size: 14px; font-weight: 600; margin: 0 0 8px; }
.pay-amount { color: #182033; font-size: 42px; line-height: 1.1; font-weight: 800; letter-spacing: .01em; margin-bottom: 8px; }
.pay-amount span { font-size: 23px; margin-right: 3px; vertical-align: 8px; }
.pay-countdown { color: #7a5a1f; font-size: 13px; margin-bottom: 12px; }
.pay-countdown.expired { color: #c53030; }
.qr-img { width: 300px; height: 300px; display: block; margin: 0 auto; border-radius: 8px; image-rendering: pixelated; }
.pay-qr-img { width: 300px; height: 300px; }
.qr-fallback { margin: 0 auto; display: grid; place-items: center; text-align: center; font-size: 12.5px; padding: 12px; border: 1px dashed rgba(197,48,48,.5); border-radius: 8px; color: #c53030; }
.pay-paid { color: #1f9d55; font-size: 15px; font-weight: 700; margin-top: 10px; }
.pay-order { color: #68738b; font-size: 11px; margin-top: 10px; word-break: break-all; }
</style>
