<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { api, fmtTime, qs } from '../api';

// 出图队列：内存里的实时运行态（/api/admin/render-status）+ 库里的历史任务（/api/admin/render-jobs）
const live = ref(null);
const queues = ref([]);
const summary = ref({ jobs: {}, pages: {}, spend: {} });
const jobs = ref([]);
const total = ref(0);
const loading = ref(false);
const error = ref('');
const autoRefresh = ref(true);
const filter = reactive({ state: '', page: 1, pageSize: 40 });
let timer = null;

const STATE_LABEL = {
  queued: '排队中', running: '生成中', done: '已完成', failed: '失败', cancelled: '已取消'
};
const STATE_CLASS = {
  queued: 'adm-warn', running: 'adm-info', done: 'adm-ok', failed: 'adm-bad', cancelled: 'adm-muted'
};
const stateLabel = (state) => STATE_LABEL[state] || state;
const stateClass = (state) => STATE_CLASS[state] || 'adm-muted';

const running = computed(() => (summary.value.jobs || {}).running || 0);
const queued = computed(() => (summary.value.jobs || {}).queued || 0);
const percent = computed(() => {
  const value = live.value;
  if (!value || !value.total) return 0;
  return Math.min(100, Math.round(((value.done + value.failed) / value.total) * 100));
});
const liveState = computed(() => {
  const state = (live.value || {}).state || 'idle';
  return ({ running: '正在出图', paused: '已暂停', idle: '空闲', done: '本轮已完成', stopped: '已停止' })[state] || state;
});

async function loadLive() {
  try {
    const data = await api.get('render-status');
    live.value = data.render;
    queues.value = Array.isArray(data.users) ? data.users : [];
  } catch { /* 实时态失败就只显示历史 */ }
}

async function loadSummary() {
  try {
    summary.value = await api.get('queue');
  } catch { /* 忽略 */ }
}

async function loadJobs() {
  loading.value = true;
  try {
    const data = await api.get('render-jobs' + qs(filter));
    jobs.value = data.items;
    total.value = data.total;
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

async function refreshAll() {
  await Promise.all([loadLive(), loadSummary(), loadJobs()]);
}

function startTimer() {
  stopTimer();
  if (!autoRefresh.value) return;
  timer = window.setInterval(() => { refreshAll(); }, 3000);
}
function stopTimer() {
  if (timer) { window.clearInterval(timer); timer = null; }
}

onMounted(() => { refreshAll(); startTimer(); });
onBeforeUnmount(stopTimer);
</script>

<template>
  <div>
    <div class="adm-page-head">
      <div>
        <h2>出图队列</h2>
        <p class="adm-muted">实时运行态来自服务进程，历史任务来自 render_jobs 表；每 3 秒自动刷新。</p>
      </div>
      <div class="adm-toolbar" style="margin: 0">
        <el-switch v-model="autoRefresh" size="small" active-text="自动刷新" @change="autoRefresh ? startTimer() : stopTimer()" />
        <el-button size="small" @click="refreshAll">立即刷新</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-4" style="margin-bottom: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">当前状态</div>
        <div class="adm-kpi-value">{{ liveState }}</div>
        <div class="adm-kpi-sub">
          并发 {{ (live || {}).concurrency || 0 }} · 已跑 {{ (live || {}).elapsedSeconds || 0 }} 秒
        </div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">本轮进度</div>
        <div class="adm-kpi-value">{{ (live || {}).done || 0 }} / {{ (live || {}).total || 0 }}</div>
        <div class="adm-progress"><i :style="{ width: percent + '%' }" /></div>
        <div class="adm-kpi-sub">失败 {{ (live || {}).failed || 0 }} · 剩余 {{ (live || {}).remaining || 0 }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">正在出的页</div>
        <div class="adm-kpi-value">{{ ((live || {}).current || []).join(' / ') || '—' }}</div>
        <div class="adm-kpi-sub">平均 {{ (live || {}).avgSeconds || 0 }} 秒一页</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">累计页数 / 今日</div>
        <div class="adm-kpi-value">{{ (summary.pages || {}).total || 0 }} / {{ (summary.pages || {}).today || 0 }}</div>
        <div class="adm-kpi-sub">今日扣费 {{ (summary.spend || {}).todayCredits || 0 }} 积分 · {{ (summary.spend || {}).calls || 0 }} 次调用</div>
      </div>
    </div>

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-kpi-label" style="margin-bottom: 8px">排队中（本进程内存队列，前 200 条）</div>
      <div v-if="live && (live.queued || []).length" class="adm-chips">
        <span v-for="index in live.queued" :key="index" class="adm-chip">第 {{ index }} 页</span>
      </div>
      <div v-else class="adm-muted" style="font-size: 12.5px">队列是空的（没任务在等）</div>
      <div v-if="(live || {}).lastError" class="adm-muted" style="margin-top: 8px; font-size: 12.5px">
        最近一次错误：{{ live.lastError }}
      </div>
    </div>

    <div v-if="queues.length > 1" class="adm-card" style="margin-bottom: 14px">
      <div class="adm-kpi-label" style="margin-bottom: 8px">各用户队列（{{ queues.length }} 个）</div>
      <el-table :data="queues" size="small">
        <el-table-column prop="userId" label="用户" min-width="200" show-overflow-tooltip />
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <span :class="row.active ? 'adm-info' : row.queue.length ? 'adm-warn' : 'adm-muted'">
              {{ row.active ? '生成中' : row.queue.length ? '排队中' : '空闲' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="进度" width="150">
          <template #default="{ row }">{{ row.done }} / {{ row.total }}（失败 {{ row.failed }}）</template>
        </el-table-column>
        <el-table-column label="正在出的页" width="140">
          <template #default="{ row }">{{ (row.current || []).join(' / ') || '—' }}</template>
        </el-table-column>
        <el-table-column label="已用时" width="110">
          <template #default="{ row }">{{ row.elapsedSeconds }} 秒</template>
        </el-table-column>
      </el-table>
    </div>

    <div class="adm-grid adm-grid-4" style="margin-bottom: 14px">
      <div class="adm-card"><div class="adm-kpi-label">生成中</div><div class="adm-kpi-value adm-info">{{ running }}</div></div>
      <div class="adm-card"><div class="adm-kpi-label">排队中</div><div class="adm-kpi-value adm-warn">{{ queued }}</div></div>
      <div class="adm-card"><div class="adm-kpi-label">已完成</div><div class="adm-kpi-value adm-ok">{{ (summary.jobs || {}).done || 0 }}</div></div>
      <div class="adm-card"><div class="adm-kpi-label">失败</div><div class="adm-kpi-value adm-bad">{{ (summary.jobs || {}).failed || 0 }}</div></div>
    </div>

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <el-select v-model="filter.state" placeholder="状态" size="small" style="width: 140px" clearable>
          <el-option label="排队中" value="queued" />
          <el-option label="生成中" value="running" />
          <el-option label="已完成" value="done" />
          <el-option label="失败" value="failed" />
          <el-option label="已取消" value="cancelled" />
        </el-select>
        <el-button size="small" type="primary" @click="filter.page = 1; loadJobs()">查询</el-button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <el-table :data="jobs" size="small" :empty-text="loading ? '加载中…' : '还没有出图任务（生成过一页就会有一条）'">
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column prop="projectId" label="项目" width="150" show-overflow-tooltip />
        <el-table-column label="页" width="70">
          <template #default="{ row }">第 {{ row.pageIndex }} 页</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <span :class="stateClass(row.state)">{{ stateLabel(row.state) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="90">
          <template #default="{ row }">{{ row.seconds ? row.seconds + ' 秒' : '—' }}</template>
        </el-table-column>
        <el-table-column prop="attempts" label="尝试" width="70" />
        <el-table-column label="完成时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.finishedAt) }}</template>
        </el-table-column>
        <el-table-column prop="error" label="错误" min-width="200" show-overflow-tooltip />
      </el-table>
    </div>

    <div class="adm-pager">
      <el-pagination
        v-model:current-page="filter.page"
        v-model:page-size="filter.pageSize"
        :page-sizes="[20, 40, 80, 120]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        size="small"
        @current-change="loadJobs"
        @size-change="filter.page = 1; loadJobs()"
      />
    </div>

    <p class="adm-muted" style="font-size: 12px; margin-top: 10px">
      提示：队列里的位置由服务进程维护；容器重启后 running 的任务会自动续跑（同一页最多重试 3 次）。
    </p>
  </div>
</template>
