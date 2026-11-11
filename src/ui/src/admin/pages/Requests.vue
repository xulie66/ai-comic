<script setup>
import { onMounted, reactive, ref } from 'vue';
import { api, fmtTime, qs } from '../api';

// 主视图＝模型调用明细（只记真正打到上游的请求）；第二页签留着旧的「全部接口」日志
const tab = ref('model');

const calls = ref([]);
const total = ref(0);
const stats = ref({ ok: 0, fail: 0, avgSeconds: 0 });
const models = ref([]);
const loading = ref(false);
const error = ref('');
const filter = reactive({ status: '', capability: '', model: '', username: '', page: 1, pageSize: 30 });

const detail = ref(null);
const detailLoading = ref(false);
const dialog = ref(false);

// 旧的全接口日志
const rows = ref([]);
const rowsTotal = ref(0);
const facets = ref({ paths: [], statuses: [] });
const rowsLoading = ref(false);
const rowsError = ref('');
const httpFilter = reactive({ username: '', path: '', method: '', status: '', page: 1, pageSize: 30 });

const CAP_LABEL = { text: '文本', image: '图片' };
const capLabel = (value) => CAP_LABEL[value] || value || '—';

async function loadCalls() {
  loading.value = true;
  try {
    const data = await api.get('model-calls' + qs(filter));
    calls.value = data.items;
    total.value = data.total;
    stats.value = data.stats;
    models.value = data.models || [];
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

async function openDetail(row) {
  dialog.value = true;
  detailLoading.value = true;
  detail.value = null;
  try {
    const data = await api.get('model-calls' + qs({ id: row.id }));
    detail.value = data.item;
  } catch (err) {
    detail.value = { error: String(err.message || err) };
  } finally {
    detailLoading.value = false;
  }
}

function pretty(text) {
  if (!text) return '';
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return String(text); }
}

async function copy(text) {
  try { await navigator.clipboard.writeText(text || ''); } catch { /* 忽略 */ }
}

async function loadHttp() {
  rowsLoading.value = true;
  try {
    const data = await api.get('requests' + qs(httpFilter));
    rows.value = data.items;
    rowsTotal.value = data.total;
    facets.value = data.facets;
    rowsError.value = '';
  } catch (err) {
    rowsError.value = String(err.message || err);
  } finally {
    rowsLoading.value = false;
  }
}

function pickPath(path) {
  httpFilter.path = path;
  httpFilter.page = 1;
  loadHttp();
}

onMounted(loadCalls);
</script>

<template>
  <div>
    <div class="adm-page-head">
      <div>
        <h2>模型调用明细</h2>
        <p class="adm-muted">
          只记真正打到上游模型的请求：调的是哪个模型、成没成功、多久、发了什么、回来了什么。点任意一行看完整报文。
        </p>
      </div>
      <div class="adm-toolbar" style="margin: 0">
        <el-radio-group v-model="tab" size="small">
          <el-radio-button label="model">模型调用</el-radio-button>
          <el-radio-button label="http">全部接口</el-radio-button>
        </el-radio-group>
        <el-button size="small" :loading="tab === 'model' ? loading : rowsLoading" @click="tab === 'model' ? loadCalls() : loadHttp()">刷新</el-button>
      </div>
    </div>

    <!-- ============ 模型调用 ============ -->
    <template v-if="tab === 'model'">
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

      <div class="adm-grid adm-grid-4" style="margin-bottom: 14px">
        <div class="adm-card">
          <div class="adm-kpi-label">近 24 小时成功</div>
          <div class="adm-kpi-value adm-ok">{{ stats.ok }}</div>
        </div>
        <div class="adm-card">
          <div class="adm-kpi-label">近 24 小时失败</div>
          <div class="adm-kpi-value adm-bad">{{ stats.fail }}</div>
        </div>
        <div class="adm-card">
          <div class="adm-kpi-label">平均耗时</div>
          <div class="adm-kpi-value">{{ stats.avgSeconds ?? ((stats.avgMs || 0) / 1000).toFixed(3) }} <span class="adm-kpi-sub">s</span></div>
        </div>
        <div class="adm-card">
          <div class="adm-kpi-label">按模型</div>
          <div class="adm-chips" style="margin-top: 8px">
            <span v-for="item in models.slice(0, 4)" :key="item.model + item.capability" class="adm-chip" :class="item.fails ? 'red' : 'green'">
              {{ item.model || '未标模型' }} · {{ item.calls }} 次{{ item.fails ? `（失败 ${item.fails}）` : '' }}
            </span>
            <span v-if="!models.length" class="adm-muted" style="font-size: 12px">还没有调用记录</span>
          </div>
        </div>
      </div>

      <div class="adm-card" style="margin-bottom: 14px">
        <div class="adm-toolbar" style="margin-bottom: 0">
          <el-select v-model="filter.status" placeholder="状态" size="small" style="width: 120px" clearable>
            <el-option label="成功" value="ok" />
            <el-option label="失败" value="fail" />
          </el-select>
          <el-select v-model="filter.capability" placeholder="类型" size="small" style="width: 120px" clearable>
            <el-option label="文本" value="text" />
            <el-option label="图片" value="image" />
          </el-select>
          <el-select v-model="filter.model" placeholder="模型" size="small" style="width: 220px" clearable filterable allow-create>
            <el-option v-for="item in models" :key="item.model" :label="`${item.model}（${item.calls}）`" :value="item.model" />
          </el-select>
          <el-input v-model="filter.username" placeholder="用户名" size="small" style="width: 140px" clearable @keyup.enter="filter.page = 1; loadCalls()" />
          <el-button size="small" type="primary" @click="filter.page = 1; loadCalls()">查询</el-button>
        </div>
      </div>

      <div class="adm-table-wrap">
        <el-table :data="calls" size="small" :empty-text="loading ? '加载中…' : '还没有模型调用（生成一次分镜或出图就会有一条）'" @row-click="openDetail">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column prop="username" label="用户" width="120" show-overflow-tooltip />
          <el-table-column label="模型" min-width="190" show-overflow-tooltip>
            <template #default="{ row }">
              <b>{{ row.model || '—' }}</b>
              <span class="adm-muted" style="margin-left: 6px; font-size: 11.5px">{{ row.provider }}</span>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="80">
            <template #default="{ row }">{{ capLabel(row.capability) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <span :class="row.ok ? 'adm-ok' : 'adm-bad'">{{ row.ok ? '成功' : '失败' }}</span>
              <span class="adm-muted" style="font-size: 11.5px"> {{ row.statusCode || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.seconds ?? ((row.ms || 0) / 1000).toFixed(3) }} s</template>
          </el-table-column>
          <el-table-column label="token" width="130">
            <template #default="{ row }">
              <span v-if="(row.usage || {}).total_tokens" class="adm-mono">{{ row.usage.total_tokens }}</span>
              <span v-else class="adm-muted">—</span>
            </template>
          </el-table-column>
          <el-table-column prop="error" label="错误" min-width="160" show-overflow-tooltip>
            <template #default="{ row }"><span class="adm-bad">{{ row.error }}</span></template>
          </el-table-column>
          <el-table-column label="" width="80" fixed="right">
            <template #default="{ row }">
              <el-button size="small" text type="primary" @click.stop="openDetail(row)">明细</el-button>
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
          @current-change="loadCalls"
          @size-change="filter.page = 1; loadCalls()"
        />
      </div>
    </template>

    <!-- ============ 全部接口（旧日志） ============ -->
    <template v-else>
      <el-alert v-if="rowsError" :title="rowsError" type="error" show-icon :closable="false" style="margin-bottom: 12px" />
      <div class="adm-card" style="margin-bottom: 14px">
        <div class="adm-toolbar" style="margin-bottom: 0">
          <el-input v-model="httpFilter.username" placeholder="用户名" size="small" style="width: 140px" clearable @keyup.enter="loadHttp" />
          <el-input v-model="httpFilter.path" placeholder="接口路径，如 /api/state" size="small" style="width: 220px" clearable @keyup.enter="loadHttp" />
          <el-select v-model="httpFilter.method" placeholder="方法" size="small" style="width: 110px" clearable>
            <el-option label="GET" value="GET" />
            <el-option label="POST" value="POST" />
          </el-select>
          <el-select v-model="httpFilter.status" placeholder="状态" size="small" style="width: 130px" clearable>
            <el-option v-for="item in facets.statuses" :key="item" :label="item" :value="item" />
          </el-select>
          <el-button size="small" type="primary" @click="httpFilter.page = 1; loadHttp()">查询</el-button>
        </div>
      </div>
      <div class="adm-table-wrap">
        <el-table :data="rows" size="small" :empty-text="rowsLoading ? '加载中…' : '还没有请求记录'">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column prop="username" label="用户" width="130" />
          <el-table-column prop="method" label="方法" width="80" />
          <el-table-column label="接口" min-width="220" show-overflow-tooltip>
            <template #default="{ row }"><span class="adm-mono">{{ row.path }}</span></template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <span :class="row.status >= 400 ? 'adm-bad' : 'adm-ok'">{{ row.status }}</span>
            </template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ row.seconds ?? ((row.ms || 0) / 1000).toFixed(3) }} s</template>
          </el-table-column>
          <el-table-column prop="ip" label="IP" width="140" show-overflow-tooltip />
        </el-table>
      </div>
      <div class="adm-pager">
        <el-pagination
          v-model:current-page="httpFilter.page"
          v-model:page-size="httpFilter.pageSize"
          :page-sizes="[20, 30, 50, 100]"
          :total="rowsTotal"
          layout="total, sizes, prev, pager, next"
          size="small"
          @current-change="loadHttp"
          @size-change="httpFilter.page = 1; loadHttp()"
        />
      </div>
    </template>

    <!-- 明细弹窗：发出去什么、收回来什么 -->
    <el-dialog v-model="dialog" title="调用明细" width="920px" top="6vh">
      <div v-if="detailLoading" class="adm-muted">加载中…</div>
      <div v-else-if="detail && detail.error" class="adm-bad">{{ detail.error }}</div>
      <div v-else-if="detail">
        <div class="adm-grid adm-grid-4" style="margin-bottom: 14px">
          <div class="adm-card"><div class="adm-kpi-label">模型</div><div style="font-weight: 600">{{ detail.model || '—' }}</div></div>
          <div class="adm-card">
            <div class="adm-kpi-label">结果</div>
            <div :class="detail.ok ? 'adm-ok' : 'adm-bad'" style="font-weight: 600">
              {{ detail.ok ? '成功' : '失败' }} · HTTP {{ detail.statusCode || '—' }} · {{ detail.seconds ?? ((detail.ms || 0) / 1000).toFixed(3) }} s
            </div>
          </div>
          <div class="adm-card"><div class="adm-kpi-label">类型 / 场景</div><div style="font-weight: 600">{{ capLabel(detail.capability) }} · {{ detail.scene || '—' }}</div></div>
          <div class="adm-card"><div class="adm-kpi-label">用量</div><div style="font-weight: 600">{{ (detail.usage || {}).total_tokens || '—' }} tokens</div></div>
        </div>
        <p class="adm-muted" style="font-size: 12px; margin: 0 0 12px">
          地址 <span class="adm-mono">{{ detail.provider }}{{ detail.endpoint }}</span>
          · 用户 {{ detail.username || detail.userId || '—' }} · {{ fmtTime(detail.at) }}
        </p>
        <div style="margin-bottom: 6px; font-weight: 600">发出去的内容</div>
        <pre class="adm-code" style="max-height: 240px">{{ pretty(detail.requestBody) || '（空）' }}</pre>
        <el-button size="small" text type="primary" @click="copy(detail.requestBody)">复制请求</el-button>
        <div style="margin: 10px 0 6px; font-weight: 600">返回的内容</div>
        <pre class="adm-code" style="max-height: 280px">{{ pretty(detail.responseBody) || '（空）' }}</pre>
        <el-button size="small" text type="primary" @click="copy(detail.responseBody)">复制响应</el-button>
        <div v-if="detail.error" style="margin-top: 12px" class="adm-bad">错误：{{ detail.error }}</div>
      </div>
      <template #footer>
        <el-button size="small" @click="dialog = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>
