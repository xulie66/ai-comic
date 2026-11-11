<script setup>
import { onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { api, fmtTime, qs } from '../api';

const rows = ref([]);
const total = ref(0);
const stats = ref({ paidAmount: 0, paidCredits: 0, pending: 0 });
const loading = ref(false);
const error = ref('');
const filter = reactive({ username: '', status: '', page: 1, pageSize: 30 });
const users = ref([]);
const dialog = ref(false);
const form = reactive({ userId: '', amount: 10, credits: 100, method: 'manual', tradeNo: '', note: '', status: 'pending' });

async function load() {
  loading.value = true;
  try {
    const data = await api.get('payments' + qs(filter));
    rows.value = data.items;
    total.value = data.total;
    stats.value = data.stats;
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

async function loadUsers() {
  try {
    const data = await api.get('users');
    users.value = data.users;
  } catch { /* 忽略 */ }
}

async function create() {
  if (!form.userId) { ElMessage.warning('先选用户'); return; }
  try {
    await api.post('payments/create', { ...form });
    ElMessage.success('订单已创建');
    dialog.value = false;
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function settle(row, action) {
  const label = action === 'paid' ? '确认到账' : action === 'refund' ? '退款' : '取消';
  try {
    await ElMessageBox.confirm(`确定对订单 ${row.id} 执行「${label}」？${action === 'paid' ? '会立即给用户加积分。' : ''}`, '确认', {
      confirmButtonText: label, cancelButtonText: '再想想'
    });
    await api.post('payments/settle', { id: row.id, action });
    ElMessage.success('已处理');
    load();
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

/* ---------- 易支付（彩虹易支付协议）配置 ---------- */
const paySettings = reactive({
  enabled: true,
  provider: 'epay',
  gateway: '',
  pid: '',
  key: '',
  hasKey: false,
  payTypes: ['alipay', 'wxpay'],
  checkoutMode: 'redirect',
  publicBaseUrl: '',
  orderMinutes: 30,
  creditsPerYuan: 10,
  manualNote: '',
  packages: []
});
const payBusy = ref(false);
const payEditOpen = ref(false);
const savedPay = reactive({ gateway: '', enabled: false });
const testOut = ref('');

async function editPayment() {
  await loadPaySettings();
  testOut.value = '';
  payEditOpen.value = true;
}

async function togglePayment() {
  payBusy.value = true;
  try {
    await api.post('pay/settings', { payment: { enabled: !savedPay.enabled } });
    await loadPaySettings();
    ElMessage.success(savedPay.enabled ? '易支付已启用' : '易支付已停用');
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally { payBusy.value = false; }
}

const PAY_TYPES = [
  { value: 'alipay', label: '支付宝' },
  { value: 'wxpay', label: '微信支付' },
  { value: 'qqpay', label: 'QQ 钱包' },
  { value: 'usdt', label: 'USDT' }
];

async function loadPaySettings() {
  try {
    const data = await api.get('pay/settings');
    const payment = data.payment || {};
    const epay = data.epay || {};
    paySettings.enabled = payment.enabled !== false;
    paySettings.provider = payment.provider || 'epay';
    paySettings.gateway = epay.gateway || '';
    savedPay.gateway = paySettings.gateway;
    savedPay.enabled = paySettings.enabled;
    paySettings.pid = epay.pid || '';
    paySettings.key = '';
    paySettings.hasKey = Boolean(epay.hasKey);
    paySettings.payTypes = epay.payTypes && epay.payTypes.length ? epay.payTypes : ['alipay', 'wxpay'];
    paySettings.checkoutMode = epay.checkoutMode || 'redirect';
    paySettings.publicBaseUrl = epay.publicBaseUrl || '';
    paySettings.orderMinutes = Number(epay.orderMinutes || 30);
    paySettings.creditsPerYuan = Number(payment.creditsPerYuan || 10);
    paySettings.manualNote = payment.manualNote || '';
    paySettings.packages = (payment.packages || []).map((item) => ({ ...item }));
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

function addPackage() {
  paySettings.packages.push({ id: `p${Date.now()}`, title: '新套餐', amountYuan: 10, bonusPercent: 0, note: '' });
}

async function savePaySettings() {
  payBusy.value = true;
  try {
    await api.post('pay/settings', {
      payment: {
        enabled: paySettings.enabled,
        provider: paySettings.provider,
        creditsPerYuan: paySettings.creditsPerYuan,
        manualNote: paySettings.manualNote,
        packages: paySettings.packages.map((item) => ({
          id: item.id, title: item.title, amountYuan: Number(item.amountYuan),
          bonusPercent: Number(item.bonusPercent || 0), note: item.note || ''
        })),
        epay: {
          gateway: paySettings.gateway,
          pid: paySettings.pid,
          key: paySettings.key,
          payTypes: paySettings.payTypes,
          payType: paySettings.payTypes[0] || 'alipay',
          checkoutMode: paySettings.checkoutMode,
          publicBaseUrl: paySettings.publicBaseUrl,
          orderMinutes: paySettings.orderMinutes
        }
      }
    });
    await loadPaySettings();
    payEditOpen.value = false;
    ElMessage.success('支付配置已保存');
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    payBusy.value = false;
  }
}

async function testGateway() {
  payBusy.value = true;
  testOut.value = '';
  try {
    const data = await api.post('pay/test', {});
    testOut.value = `网关可达：${data.sample || '已返回响应'}`;
    ElMessage.success('网关可达');
  } catch (err) {
    testOut.value = String(err.message || err);
    ElMessage.error(testOut.value);
  } finally {
    payBusy.value = false;
  }
}

onMounted(() => { load(); loadUsers(); loadPaySettings(); });
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">支付充值</div>
        <div class="adm-sub">手工充值先把订单建出来（pending），确认到账后一次点击加分；退款会扣回积分并留痕</div>
      </div>
      <el-button size="small" type="primary" @click="dialog = true">新建充值订单</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-3" style="margin-bottom: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">累计到账金额</div>
        <div class="adm-kpi-value">¥ {{ stats.paidAmount }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">累计发放积分</div>
        <div class="adm-kpi-value">{{ stats.paidCredits }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">待处理订单</div>
        <div class="adm-kpi-value">{{ stats.pending }}</div>
      </div>
    </div>

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <el-input v-model="filter.username" placeholder="用户名" size="small" style="width: 160px" clearable @keyup.enter="load" />
        <el-select v-model="filter.status" placeholder="状态" size="small" style="width: 140px" clearable>
          <el-option label="待支付" value="pending" />
          <el-option label="已到账" value="paid" />
          <el-option label="已退款" value="refunded" />
          <el-option label="已取消" value="canceled" />
        </el-select>
        <el-button size="small" type="primary" @click="filter.page = 1; load()">查询</el-button>
      </div>
    </div>

    <div class="adm-card epay-summary" style="margin-bottom: 14px">
      <div class="epay-gateway">
        <span class="adm-kpi-label">易支付网关</span>
        <strong :title="savedPay.gateway">{{ savedPay.gateway || '尚未配置' }}</strong>
      </div>
      <div class="epay-amount">
        <span class="adm-kpi-label">总收款金额</span>
        <strong>¥ {{ Number(stats.epayPaidAmount || 0).toFixed(2) }}</strong>
      </div>
      <div class="epay-actions">
        <el-tag size="small" :type="savedPay.enabled ? 'success' : 'info'">{{ savedPay.enabled ? '已启用' : '已停用' }}</el-tag>
        <el-button size="small" :disabled="payBusy" @click="editPayment">编辑</el-button>
        <el-button size="small" :type="savedPay.enabled ? 'danger' : 'success'" plain :loading="payBusy" @click="togglePayment">{{ savedPay.enabled ? '停用' : '启用' }}</el-button>
      </div>
    </div>

    <el-dialog v-model="payEditOpen" title="编辑易支付" width="min(800px, 94vw)" :close-on-click-modal="false">
      <el-form label-width="120px" size="small">
        <el-form-item label="在线充值">
          <el-switch v-model="paySettings.enabled" active-text="启用" />
        </el-form-item>
        <el-form-item label="网关地址">
          <el-input v-model="paySettings.gateway" placeholder="https://pay.example.com/（不带 mapi.php，系统自动拼）" />
        </el-form-item>
        <el-form-item label="商户 PID">
          <el-input v-model="paySettings.pid" placeholder="1000" />
        </el-form-item>
        <el-form-item label="商户密钥 Key">
          <el-input
            v-model="paySettings.key"
            :placeholder="paySettings.hasKey ? '已配置（留空表示不修改）' : '在易支付后台 → 商户资料里找'"
            show-password
          />
        </el-form-item>
        <el-form-item label="支持支付方式">
          <el-checkbox-group v-model="paySettings.payTypes">
            <el-checkbox v-for="item in PAY_TYPES" :key="item.value" :value="item.value" :label="item.value">{{ item.label }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="收银台形态">
          <el-radio-group v-model="paySettings.checkoutMode">
            <el-radio-button value="redirect">跳转收银台</el-radio-button>
            <el-radio-button value="qr_code">页内二维码</el-radio-button>
          </el-radio-group>
          <div class="adm-muted" style="margin-top: 4px; font-size: 12px">
            跳转：点支付直接跳网关收银台；页内二维码：留在钱包页显示二维码（优先用网关返回的 qrcode，网关只给收银台链接时就用链接本身生成二维码，可复制或另开窗口）。
          </div>
        </el-form-item>
        <el-form-item label="公网地址">
          <el-input v-model="paySettings.publicBaseUrl" placeholder="https://comic.example.com（回调地址用它拼，必须外网可达）" />
        </el-form-item>
        <el-form-item label="订单超时">
          <el-input-number v-model="paySettings.orderMinutes" :min="5" :max="1440" :step="5" />
          <span class="adm-muted" style="margin-left: 8px">分钟</span>
        </el-form-item>
        <el-form-item label="兑换比例">
          <el-input-number v-model="paySettings.creditsPerYuan" :min="0.01" :max="10000" :precision="2" :step="1" />
          <span class="adm-muted" style="margin-left: 8px">积分 / 元</span>
        </el-form-item>
        <el-form-item label="人工充值说明">
          <el-input v-model="paySettings.manualNote" placeholder="显示在前台钱包页" />
        </el-form-item>
        <el-form-item label="充值套餐">
          <div style="width: 100%">
            <div v-for="(pack, index) in paySettings.packages" :key="index" class="pack-row">
              <el-input v-model="pack.title" placeholder="套餐名" style="width: 140px" />
              <el-input-number v-model="pack.amountYuan" :min="1" :max="100000" :precision="2" :controls="false" style="width: 110px" />
              <span class="adm-muted">元</span>
              <el-input-number v-model="pack.bonusPercent" :min="0" :max="100" :controls="false" style="width: 90px" />
              <span class="adm-muted">% 赠送</span>
              <el-input v-model="pack.note" placeholder="备注（选填）" style="width: 160px" />
              <el-button link size="small" type="danger" @click="paySettings.packages.splice(index, 1)">删除</el-button>
            </div>
            <el-button link size="small" type="primary" @click="addPackage">+ 增加套餐</el-button>
          </div>
        </el-form-item>
      </el-form>
      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap">
        <el-button size="small" :loading="payBusy" @click="testGateway">测试已保存网关</el-button>
        <span class="adm-muted" style="font-size: 12px">
          回调地址：{{ (paySettings.publicBaseUrl || '公网地址未填') + '/api/pay/notify/epay' }}
        </span>
      </div>
      <pre v-if="testOut" class="adm-mono" style="margin-top: 8px; font-size: 12px; white-space: pre-wrap">{{ testOut }}</pre>
      <template #footer>
        <el-button :disabled="payBusy" @click="payEditOpen = false">取消</el-button>
        <el-button type="primary" :loading="payBusy" @click="savePaySettings">保存</el-button>
      </template>
    </el-dialog>

    <div class="adm-table-wrap">
      <el-table :data="rows" size="small" :empty-text="loading ? '加载中…' : '还没有订单'">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.at) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户" width="130" />
        <el-table-column prop="amount" label="金额" width="90">
          <template #default="{ row }">¥ {{ row.amount }}</template>
        </el-table-column>
        <el-table-column prop="credits" label="积分" width="90" />
        <el-table-column prop="method" label="方式" width="100" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <span
              class="adm-chip"
              :class="row.status === 'paid' ? 'green' : row.status === 'pending' ? 'amber' : 'gray'"
            >{{ { paid: '已到账', pending: '待支付', refunded: '已退款', canceled: '已取消' }[row.status] || row.status }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="tradeNo" label="交易号" width="160" show-overflow-tooltip />
        <el-table-column prop="note" label="备注" min-width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="190" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status !== 'paid'" link size="small" type="success" @click="settle(row, 'paid')">确认到账</el-button>
            <el-button v-if="row.status === 'paid'" link size="small" type="danger" @click="settle(row, 'refund')">退款</el-button>
            <el-button v-if="row.status === 'pending'" link size="small" @click="settle(row, 'canceled')">取消</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="adm-pager">
      <el-pagination
        v-model:current-page="filter.page"
        v-model:page-size="filter.pageSize"
        :page-sizes="[20, 30, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        size="small"
        @current-change="load"
        @size-change="filter.page = 1; load()"
      />
    </div>

    <el-dialog v-model="dialog" title="新建充值订单" width="440px">
      <el-form label-width="80px">
        <el-form-item label="用户">
          <el-select v-model="form.userId" filterable placeholder="选择用户" style="width: 100%">
            <el-option v-for="user in users" :key="user.id" :label="user.username" :value="user.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="金额">
          <el-input-number v-model="form.amount" :min="0" :max="100000" :precision="2" />
        </el-form-item>
        <el-form-item label="积分">
          <el-input-number v-model="form.credits" :min="0" :max="1000000" />
        </el-form-item>
        <el-form-item label="方式">
          <el-select v-model="form.method" style="width: 100%">
            <el-option label="手工 / 转账" value="manual" />
            <el-option label="支付宝" value="alipay" />
            <el-option label="微信" value="wechat" />
          </el-select>
        </el-form-item>
        <el-form-item label="交易号">
          <el-input v-model="form.tradeNo" placeholder="选填，对账用" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" placeholder="选填" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status">
            <el-radio value="pending">待支付</el-radio>
            <el-radio value="paid">已到账（立即加分）</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="dialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="create()">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.epay-summary { display: flex; align-items: center; gap: 28px; }
.epay-gateway { flex: 1; min-width: 0; }
.epay-gateway, .epay-amount { display: flex; flex-direction: column; gap: 7px; }
.epay-gateway strong { font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.epay-amount strong { font-size: 20px; font-variant-numeric: tabular-nums; }
.epay-actions { display: flex; align-items: center; gap: 10px; }
.epay-actions .el-button + .el-button { margin-left: 0; }
@media (max-width: 760px) { .epay-summary { flex-wrap: wrap; gap: 18px; } .epay-gateway { flex-basis: 55%; } .epay-actions { margin-left: auto; } }
.pack-row { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; flex-wrap: wrap; }
</style>
