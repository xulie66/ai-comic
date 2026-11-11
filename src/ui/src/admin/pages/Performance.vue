<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { api, fmtBytes, fmtDuration, fmtTime } from '../api';

const data = ref(null);
const error = ref('');
const loading = ref(false);
let timer = null;

async function load() {
  loading.value = true;
  try {
    data.value = await api.get('performance');
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

onMounted(() => { load(); timer = setInterval(load, 10000); });
onUnmounted(() => { if (timer) clearInterval(timer); });

const now = computed(() => data.value?.now || {});
const history = computed(() => data.value?.history || []);
const maxCpu = computed(() => Math.max(20, ...history.value.map((item) => Number(item.cpu || 0))));
const maxReq = computed(() => Math.max(10, ...history.value.map((item) => Number(item.req || 0))));
const memPct = computed(() => {
  const total = Number(now.value.memTotal || 0);
  return total ? Math.round((Number(now.value.memUsed || 0) / total) * 100) : 0;
});
const diskPct = computed(() => {
  const total = Number(now.value.diskTotal || 0);
  return total ? Math.round((Number(now.value.diskUsed || 0) / total) * 100) : 0;
});
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">系统性能</div>
        <div class="adm-sub">10 秒刷新一次；采样每 30 秒落一次库，下面曲线是最近 60 个采样点</div>
      </div>
      <el-button size="small" :loading="loading" @click="load()">刷新</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-4">
      <div class="adm-card">
        <div class="adm-kpi-label">CPU</div>
        <div class="adm-kpi-value">{{ now.cpuPct ?? 0 }}%</div>
        <el-progress :percentage="Math.min(100, Number(now.cpuPct || 0))" :show-text="false" style="margin-top: 8px" />
        <div class="adm-kpi-harm">{{ now.cpuCount || 0 }} 核 · load1 {{ now.load1 ?? 0 }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">内存</div>
        <div class="adm-kpi-value">{{ memPct }}%</div>
        <el-progress :percentage="memPct" :show-text="false" style="margin-top: 8px" />
        <div class="adm-kpi-harm">{{ fmtBytes(now.memUsed) }} / {{ fmtBytes(now.memTotal) }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">磁盘</div>
        <div class="adm-kpi-value">{{ diskPct }}%</div>
        <el-progress :percentage="diskPct" :show-text="false" style="margin-top: 8px" />
        <div class="adm-kpi-harm">{{ fmtBytes(now.diskUsed) }} / {{ fmtBytes(now.diskTotal) }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">进程</div>
        <div class="adm-kpi-value">{{ fmtBytes(now.rss) }}</div>
        <div class="adm-kpi-harm">
          Heap {{ fmtBytes(now.heapUsed) }} · 已运行 {{ fmtDuration(now.uptimeSec) }}
        </div>
      </div>
    </div>

    <div class="adm-grid adm-grid-4" style="margin-top: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">Postgres 库体积</div>
        <div class="adm-kpi-value">{{ fmtBytes(now.pgSize) }}</div>
        <div class="adm-kpi-harm">连接数 {{ now.pgConn ?? 0 }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">连接池</div>
        <div class="adm-kpi-value">{{ now.pool?.total ?? 0 }}</div>
        <div class="adm-kpi-harm">空闲 {{ now.pool?.idle ?? 0 }} · 等待 {{ now.pool?.waiting ?? 0 }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">每分钟请求</div>
        <div class="adm-kpi-value">{{ now.reqWindow ?? 0 }}</div>
        <div class="adm-kpi-harm">错误 {{ now.errWindow ?? 0 }} · P95 {{ now.p95Ms ?? 0 }} ms</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">运行环境</div>
        <div class="adm-kpi-value" style="font-size: 16px">{{ now.nodeVersion || '-' }}</div>
        <div class="adm-kpi-harm">{{ now.platform || '-' }}</div>
      </div>
    </div>

    <div class="adm-grid adm-grid-2" style="margin-top: 14px">
      <div class="adm-card">
        <div style="font-weight: 600; margin-bottom: 12px">CPU %（近 60 个采样）</div>
        <div class="adm-bars">
          <div v-for="(item, index) in history" :key="index" style="flex: 1; display: flex; flex-direction: column; justify-content: flex-end">
            <div class="adm-bar" :style="{ height: Math.max(2, (Number(item.cpu || 0) / maxCpu) * 110) + 'px' }" :title="`${fmtTime(item.at)} CPU ${item.cpu}%`" />
          </div>
          <div v-if="!history.length" class="adm-muted">采样中…（首条 30 秒后出现）</div>
        </div>
      </div>
      <div class="adm-card">
        <div style="font-weight: 600; margin-bottom: 12px">每分钟请求数（近 60 个采样）</div>
        <div class="adm-bars">
          <div v-for="(item, index) in history" :key="index" style="flex: 1; display: flex; flex-direction: column; justify-content: flex-end">
            <div
              class="adm-bar"
              :style="{ height: Math.max(2, (Number(item.req || 0) / maxReq) * 110) + 'px', background: item.err ? 'linear-gradient(180deg,#ff9a9a,#e14545)' : '' }"
              :title="`${fmtTime(item.at)} 请求 ${item.req} 错误 ${item.err}`"
            />
          </div>
          <div v-if="!history.length" class="adm-muted">采样中…</div>
        </div>
      </div>
    </div>
  </div>
</template>
