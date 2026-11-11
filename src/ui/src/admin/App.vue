<script setup>
import { computed, onMounted, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { api, fmtDuration, readToken } from './api';
import Overview from './pages/Overview.vue';
import Requests from './pages/Requests.vue';
import LoginLogs from './pages/LoginLogs.vue';
import Performance from './pages/Performance.vue';
import Queue from './pages/Queue.vue';
import Users from './pages/Users.vue';
import Credits from './pages/Credits.vue';
import Payments from './pages/Payments.vue';
import Codes from './pages/Codes.vue';
import Site from './pages/Site.vue';
import SystemSettings from './pages/SystemSettings.vue';
import Storage from './pages/Storage.vue';
import Channels from './pages/Channels.vue';

const GROUPS = [
  {
    title: '运营监控',
    items: [
      { key: 'overview', label: '运行总览', comp: Overview, icon: '▥' },
      { key: 'requests', label: '请求明细', comp: Requests, icon: '▤' },
      { key: 'login-logs', label: '登录日志', comp: LoginLogs, icon: '↪' },
      { key: 'performance', label: '系统性能', comp: Performance, icon: '⌁' },
      { key: 'queue', label: '出图队列', comp: Queue, icon: '◫' }
    ]
  },
  {
    title: '用户与运营',
    items: [
      { key: 'users', label: '用户管理', comp: Users, icon: '♙' },
      { key: 'credits', label: '积分运营', comp: Credits, icon: '◎' },
      { key: 'payments', label: '支付充值', comp: Payments, icon: '▣' },
      { key: 'codes', label: '兑换码', comp: Codes, icon: '◇' }
    ]
  },
  {
    title: '模型与渠道',
    items: [
      { key: 'channels', label: '模型与渠道', comp: Channels, icon: '◉' },
      { key: 'storage', label: '储存服务', comp: Storage, icon: '▤' }
    ]
  },
  {
    title: '系统设置',
    items: [
      { key: 'site', label: '网站管理', comp: Site, icon: '⚙' },
      { key: 'system-settings', label: '系统设置', comp: SystemSettings, icon: '☷' }
    ]
  }
];

const flat = GROUPS.flatMap((group) => group.items);
const current = ref(hashRoute());
const me = ref(null);
const bootError = ref('');
const uptime = ref(0);

function hashRoute() {
  const key = String(location.hash || '').replace(/^#\/?/, '');
  return flat.some((item) => item.key === key) ? key : 'overview';
}

watch(current, (key) => {
  if (String(location.hash || '').replace(/^#\/?/, '') !== key) location.hash = `#/${key}`;
});

window.addEventListener('hashchange', () => { current.value = hashRoute(); });

const active = computed(() => flat.find((item) => item.key === current.value));
const activeComp = computed(() => (active.value ? active.value.comp : Overview));

onMounted(async () => {
  try {
    const state = await api.get('overview');
    me.value = { version: state.version };
    uptime.value = state.overview.uptimeSec;
  } catch (err) {
    bootError.value = String(err.message || err);
    if (/未登录|管理员/.test(bootError.value)) {
      setTimeout(() => { location.href = `/?next=${encodeURIComponent('/admin/')}#/login`; }, 900);
    }
  }
  if (!readToken() && !document.cookie.includes('ac_session')) {
    // 没有本地 token 时用 Cookie 也能进；两条都没有就是真的没登录
  }
});

function reloadFrame() {
  const frame = document.querySelector('iframe');
  if (frame) frame.contentWindow?.location.reload();
}

function reloadPage() {
  location.reload();
}
</script>

<template>
  <div class="adm-shell">
    <aside class="adm-side">
      <div class="adm-brand">
        <div class="adm-brand-badge">漫</div>
        <div class="adm-brand-copy">
          <div class="adm-brand-text">AI 漫画</div>
          <div class="adm-brand-sub">运营控制台</div>
        </div>
        <span class="adm-brand-version">{{ me?.version || 'WEB' }}</span>
      </div>
      <nav class="adm-nav" aria-label="管理后台导航">
        <template v-for="group in GROUPS" :key="group.title">
          <div class="adm-nav-group">{{ group.title }}</div>
          <button
            v-for="item in group.items"
            :key="item.key"
            type="button"
            class="adm-nav-item"
            :class="{ active: current === item.key }"
            :aria-current="current === item.key ? 'page' : undefined"
            @click="current = item.key"
          >
            <span class="adm-nav-icon" aria-hidden="true">{{ item.icon }}</span>
            <span>{{ item.label }}</span>
          </button>
        </template>
      </nav>
      <div class="adm-side-footer">
        <a class="adm-nav-item" href="/" target="_blank" rel="noopener">
          <span class="adm-nav-icon" aria-hidden="true">↗</span><span>返回创作台</span>
        </a>
        <div class="adm-operator"><span class="adm-operator-avatar">A</span><span><b>管理员</b><small>系统运营</small></span><span class="adm-online-dot" /></div>
      </div>
    </aside>

    <section class="adm-workspace">
      <header class="adm-topbar">
        <div class="adm-breadcrumb"><span>管理后台</span><i>/</i><b>{{ active?.label || '运行总览' }}</b></div>
        <div class="adm-top-actions">
          <span class="adm-live"><i />服务运行中</span>
          <span class="adm-version">{{ me?.version || 'WEB' }}</span>
          <el-button size="small" plain @click="reloadPage()">刷新页面</el-button>
        </div>
      </header>
      <main class="adm-main">
        <div v-if="bootError" class="adm-card adm-boot-error">
          <div class="adm-error-title">后台暂时不可用</div>
          <div class="adm-muted">{{ bootError }}</div>
          <el-button size="small" type="primary" @click="reloadPage()">重新加载</el-button>
        </div>
        <component :is="activeComp" v-else :key="current" />
      </main>
      <footer class="adm-footer">AI 漫画运营控制台 <span>运行状态与业务数据实时同步</span></footer>
    </section>
  </div>
</template>
