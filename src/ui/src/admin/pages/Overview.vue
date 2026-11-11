<script setup>
import { onMounted, onUnmounted, ref } from 'vue';
import { api, fmtBytes, fmtDuration, fmtTime } from '../api';

const data = ref(null);
const error = ref('');
const loading = ref(false);
let timer = null;

async function load() {
  loading.value = true;
  try {
    data.value = await api.get('overview');
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

onMounted(() => { load(); timer = setInterval(load, 15000); });
onUnmounted(() => { if (timer) clearInterval(timer); });

const maxPages = (list) => Math.max(1, ...(list || []).map((item) => Number(item.pages || 0)));
const chartX = (index, length) => 42 + index * 616 / Math.max(1, length - 1);
const chartY = (pages) => 168 - Number(pages || 0) / maxPages(data.value?.overview?.trend) * 140;
const chartPoints = () => (data.value?.overview?.trend || []).map((item, index, list) => `${chartX(index, list.length)},${chartY(item.pages)}`).join(' ');
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">运行总览</div>
        <div class="adm-sub">
          每 15 秒自动刷新 · 版本 {{ data?.version || '-' }} · 已运行 {{ fmtDuration(data?.overview?.uptimeSec) }}
        </div>
      </div>
      <el-button size="small" :loading="loading" @click="load()">刷新</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div v-if="data" class="adm-grid adm-grid-4">
      <div class="adm-card">
        <div class="adm-kpi-label">用户</div>
        <div class="adm-kpi-value">{{ data.overview.users.total }}</div>
        <div class="adm-kpi-harm">
          今日新增 {{ data.overview.users.new_1d }} · 24h 活跃 {{ data.overview.users.active_1d }} · 7 日 {{ data.overview.users.active_7d }}
        </div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">今日出图页数</div>
        <div class="adm-kpi-value">{{ data.overview.pagesToday }}</div>
        <div class="adm-kpi-harm">项目总数 {{ data.overview.projects }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">出图队列</div>
        <div class="adm-kpi-value">{{ data.overview.jobs.running }} 跑 / {{ data.overview.jobs.queued }} 等</div>
        <div class="adm-kpi-harm">
          24h 完成 {{ data.overview.jobs.done_1d }} · 失败 {{ data.overview.jobs.failed_1d }}
        </div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">近 1 小时接口</div>
        <div class="adm-kpi-value">{{ data.overview.requests.total }}</div>
        <div class="adm-kpi-harm">
          错误 {{ data.overview.requests.errors }} · 平均 {{ data.overview.requests.avg_ms ?? 0 }} ms
        </div>
      </div>
    </div>

    <div v-if="data" class="adm-grid adm-grid-4" style="margin-top: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">登录（24h）</div>
        <div class="adm-kpi-value">{{ data.overview.logins.total }}</div>
        <div class="adm-kpi-harm">失败 {{ data.overview.logins.failed }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">30 日充值</div>
        <div class="adm-kpi-value">¥ {{ data.overview.revenue.amount }}</div>
        <div class="adm-kpi-harm">订单 {{ data.overview.revenue.orders }} 笔</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">兑换码</div>
        <div class="adm-kpi-value">{{ data.overview.codes.unused }} 未用</div>
        <div class="adm-kpi-harm">已用 {{ data.overview.codes.used }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">进程内存 / 负载</div>
        <div class="adm-kpi-value">{{ fmtBytes(data.overview.perf.rss) }}</div>
        <div class="adm-kpi-harm">
          load1 {{ data.overview.perf.load1 }} · 每分钟请求 {{ data.overview.perf.reqWindow }} · P95 {{ data.overview.perf.p95Ms }}ms
        </div>
      </div>
    </div>

    <div v-if="data" class="adm-grid adm-grid-2" style="margin-top: 14px">
      <div class="adm-card">
        <div style="font-weight: 600; margin-bottom: 12px">最近 14 天出图量</div>
        <svg viewBox="0 0 700 210" role="img" aria-label="最近14天每日出图页数折线图" style="width:100%;display:block;margin-top:12px">
          <g v-for="fraction in [0, 0.5, 1]" :key="fraction">
            <line x1="42" x2="658" :y1="168 - fraction * 140" :y2="168 - fraction * 140" stroke="currentColor" opacity="0.12" />
            <text x="35" :y="172 - fraction * 140" text-anchor="end" fill="currentColor" font-size="11">{{ Math.round(maxPages(data.overview.trend) * fraction) }}</text>
          </g>
          <polyline :points="chartPoints()" fill="none" stroke="#4f81d9" stroke-width="2.5" />
          <g v-for="(item, index) in data.overview.trend" :key="item.day">
            <circle :cx="chartX(index, data.overview.trend.length)" :cy="chartY(item.pages)" r="4" fill="#4f81d9" tabindex="0"><title>{{ item.day }}：{{ item.pages }} 页</title></circle>
            <text :x="chartX(index, data.overview.trend.length)" :y="chartY(item.pages) - 9" text-anchor="middle" fill="currentColor" font-size="10">{{ item.pages }}</text>
            <text :x="chartX(index, data.overview.trend.length)" y="193" text-anchor="middle" fill="currentColor" font-size="10">{{ item.day.slice(5) }}</text>
          </g>
        </svg>
        <div v-if="!data.overview.trend.some(item => item.pages > 0)" class="adm-muted">最近 14 天暂无成功出图记录</div>
      </div>
      <div class="adm-card">
        <div style="font-weight: 600; margin-bottom: 10px">最近模型请求</div>
        <el-table :data="data.overview.recent" size="small" :show-header="true">
          <el-table-column prop="at" label="时间" width="170">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column prop="username" label="用户" width="110" />
          <el-table-column prop="model" label="模型" min-width="170" show-overflow-tooltip />
          <el-table-column label="类型" width="65"><template #default="{ row }">{{ row.capability === 'image' ? '图片' : row.capability === 'text' ? '文本' : row.capability }}</template></el-table-column>
          <el-table-column prop="scene" label="用途" min-width="100" show-overflow-tooltip />
          <el-table-column prop="status" label="状态" width="70">
            <template #default="{ row }">
              <span :class="row.ok ? 'adm-ok' : 'adm-bad'">{{ row.ok ? '成功' : '失败' }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="seconds" label="耗时" width="85">
            <template #default="{ row }">{{ row.seconds }} 秒</template>
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>
</template>
