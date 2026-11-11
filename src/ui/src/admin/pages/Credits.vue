<script setup>
import { onMounted, reactive, ref } from 'vue';
import { ElMessage } from 'element-plus';
import { api, fmtTime, qs } from '../api';

const rows = ref([]);
const total = ref(0);
const stats = ref({ inflow: 0, outflow: 0, net: 0 });
const spend = ref({ todayCredits: 0, calls: 0 });
const pages = ref({ today: 0, total: 0 });
const jobs = ref({ running: 0, queued: 0, failed: 0, done: 0 });
const loading = ref(false);
const error = ref('');
const filter = reactive({ username: '', kind: '', page: 1, pageSize: 30 });
const users = ref([]);
const form = reactive({ userId: '', delta: 100, note: '', kind: 'gift' });
const dialog = ref(false);

const KINDS = [
  { value: 'consume', label: '模型调用扣费' },
  { value: 'recharge', label: '充值到账' },
  { value: 'redeem', label: '兑换码' },
  { value: 'gift', label: '活动发放' },
  { value: 'refund', label: '退款' },
  { value: 'adjust', label: '人工调整' },
  { value: 'status', label: '账号状态' }
];
const kindLabel = (kind) => (KINDS.find((item) => item.value === kind) || {}).label || kind;
const round6 = (value) => Math.round(Number(value || 0) * 1000000) / 1000000;

async function loadSummary() {
  try {
    const data = await api.get('queue');
    spend.value = data.spend;
    pages.value = data.pages;
    jobs.value = data.jobs;
  } catch { /* 摘要失败不影响主表 */ }
}

async function load() {
  loading.value = true;
  try {
    const data = await api.get('credits' + qs(filter));
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

async function submit() {
  if (!form.userId) { ElMessage.warning('先选个用户'); return; }
  try {
    const result = await api.post('credits/adjust', { userId: form.userId, delta: form.delta, note: form.note, kind: form.kind });
    ElMessage.success(`已发放，余额 ${result.balance}`);
    dialog.value = false;
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

onMounted(() => { load(); loadUsers(); loadSummary(); });
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">积分运营</div>
        <div class="adm-sub">所有积分变动都在这里留痕（充值 / 兑换 / 出图消耗 / 活动发放 / 退款）</div>
      </div>
      <el-button size="small" type="primary" @click="dialog = true">发放 / 扣减积分</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-3" style="margin-bottom: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">30 日流入</div>
        <div class="adm-kpi-value adm-ok">+{{ stats.inflow }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">30 日流出</div>
        <div class="adm-kpi-value adm-bad">-{{ stats.outflow }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">净增</div>
        <div class="adm-kpi-value">{{ stats.net }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">今日扣费（模型调用）</div>
        <div class="adm-kpi-value adm-bad">-{{ spend.todayCredits }}</div>
        <div class="adm-kpi-sub">{{ spend.calls }} 次成功调用</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">今日出图页数</div>
        <div class="adm-kpi-value">{{ pages.today }}</div>
        <div class="adm-kpi-sub">累计 {{ pages.total }} 页</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">出图任务</div>
        <div class="adm-kpi-value">{{ jobs.running }} <span class="adm-kpi-sub">跑着</span> / {{ jobs.queued }} <span class="adm-kpi-sub">排队</span></div>
        <div class="adm-kpi-sub">失败 {{ jobs.failed }} · 完成 {{ jobs.done }}</div>
      </div>
    </div>

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <el-input v-model="filter.username" placeholder="用户名" size="small" style="width: 160px" clearable @keyup.enter="load" />
        <el-select v-model="filter.kind" placeholder="类型" size="small" style="width: 160px" clearable>
          <el-option v-for="kind in KINDS" :key="kind.value" :label="kind.label" :value="kind.value" />
        </el-select>
        <el-button size="small" type="primary" @click="filter.page = 1; load()">查询</el-button>
        <el-button size="small" @click="load()">刷新</el-button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <el-table :data="rows" size="small" :empty-text="loading ? '加载中…' : '还没有积分流水'">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.at) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户" width="130" />
        <el-table-column label="变动" width="110">
          <template #default="{ row }">
            <span :class="row.delta >= 0 ? 'adm-ok' : 'adm-bad'">{{ row.delta > 0 ? '+' : '' }}{{ round6(row.delta) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="余额" width="110">
          <template #default="{ row }">{{ round6(row.balanceAfter) }}</template>
        </el-table-column>
        <el-table-column label="类型" width="120">
          <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
        </el-table-column>
        <el-table-column prop="note" label="说明" min-width="200" show-overflow-tooltip />
        <el-table-column prop="operator" label="操作人" width="180" show-overflow-tooltip />
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

    <el-dialog v-model="dialog" title="发放 / 扣减积分" width="420px">
      <el-form label-width="80px">
        <el-form-item label="用户">
          <el-select v-model="form.userId" filterable placeholder="选择用户" style="width: 100%">
            <el-option v-for="user in users" :key="user.id" :label="`${user.username}（${user.credits} 积分）`" :value="user.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="变动">
          <el-input-number v-model="form.delta" :min="-100000" :max="100000" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.kind" style="width: 100%">
            <el-option label="活动发放" value="gift" />
            <el-option label="手工充值" value="recharge" />
            <el-option label="纠错调整" value="adjust" />
            <el-option label="扣减" value="consume" />
          </el-select>
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.note" placeholder="写清楚原因，事后能查" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="dialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="submit()">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>
