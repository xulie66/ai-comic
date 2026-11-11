<script setup>
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { api, fmtTime } from '../api';

const PROVIDERS = [
  { value: 'openai-compat', label: 'OpenAI 兼容（/v1）' },
  { value: 'openai', label: 'OpenAI 官方' },
  { value: 'azure', label: 'Azure OpenAI' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'anthropic', label: 'Anthropic Claude' },
  { value: 'volcengine', label: '火山引擎 / 豆包' },
  { value: 'dashscope', label: '阿里通义 / 万相' },
  { value: 'custom', label: '自定义中转' }
];
const CAPABILITIES = [
  { value: 'image', label: '图片' },
  { value: 'text', label: '文本' }
];

const channels = ref([]);
const models = ref([]);
const loading = ref(false);
const error = ref('');
const busy = ref(false);
const tab = ref('channels');
const channelView = ref('by-channel');
const selectedChannelId = ref('');
const modelSearch = ref('');
const modelCapabilityFilter = ref('');
const modelStatusFilter = ref('');
const selectedModelIds = ref([]);
const bulkPriceDialog = ref(false);
const bulkMarkup = ref(0);
const reorderDialog = ref(false);
const reorderDraft = ref([]);
const importing = ref(false);
const importInput = ref(null);
const pullDialog = ref(false);
const pullLoading = ref(false);
const pullSaving = ref(false);
const pullSearch = ref('');
const pullModels = ref([]);
const pullSelection = ref([]);
const pullChannel = ref(null);
const pullCapabilityMap = reactive({});
const modelHealth = ref({});

const selectedChannel = computed(() => channels.value.find((item) => item.id === selectedChannelId.value) || channels.value[0] || null);
const filteredPullModels = computed(() => {
  const needle = String(pullSearch.value || '').trim().toLowerCase();
  return pullModels.value.filter((item) => !needle || item.modelKey.toLowerCase().includes(needle));
});

const visibleChannelModels = computed(() => {
  const source = channelView.value === 'by-channel' && selectedChannel.value
    ? (selectedChannel.value.models || []).map((item) => ({ ...item, channelName: selectedChannel.value.name }))
    : allChannelModels.value;
  const needle = String(modelSearch.value || '').trim().toLowerCase();
  return source.filter((item) => {
    if (needle && !`${item.modelKey} ${item.displayName || ''} ${item.channelName || ''}`.toLowerCase().includes(needle)) return false;
    if (modelCapabilityFilter.value && item.capability !== modelCapabilityFilter.value) return false;
    if (modelStatusFilter.value === 'enabled' && !item.enabled) return false;
    if (modelStatusFilter.value === 'disabled' && item.enabled) return false;
    return true;
  });
});

async function copyChannel(row) {
  const copy = { ...row, apiKey: undefined, apiKeyMasked: row.apiKeyMasked || '' };
  await navigator.clipboard.writeText(JSON.stringify(copy, null, 2));
  ElMessage.success('渠道配置已复制（已隐藏 API Key）');
}
function exportChannels() {
  const blob = new Blob([JSON.stringify(channels.value.map((item) => ({ ...item, apiKey: undefined })), null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = 'ai-comic-channels.json'; a.click(); URL.revokeObjectURL(url);
}
function selectChannel(row) {
  selectedChannelId.value = row.id;
  channelView.value = 'by-channel';
  selectedModelIds.value = [];
}
function channelSuccess(row) {
  const state = row.models || [];
  const checked = state.filter((model) => model.lastCheckAt);
  if (!checked.length) return row.enabled ? '已启用' : '已停用';
  const okCount = checked.filter((model) => model.lastCheckOk).length;
  return `${Math.round(okCount / checked.length * 100)}%`;
}
function importChannels() { importInput.value?.click(); }
async function handleImport(event) {
  const file = event.target.files?.[0];
  event.target.value = '';
  if (!file) return;
  importing.value = true;
  try {
    const raw = JSON.parse(await file.text());
    const rows = Array.isArray(raw) ? raw : (raw.channels || []);
    for (const row of rows) await api.post('channels/save', { ...row, id: undefined, apiKey: '' });
    await load();
    ElMessage.success(`已导入 ${rows.length} 个渠道（凭证需单独配置）`);
  } catch (err) { ElMessage.error(`导入失败：${String(err.message || err)}`); }
  finally { importing.value = false; }
}
async function toggleChannel(row) {
  try { await api.post('channels/save', { ...row, apiKey: '' }); await load(); }
  catch (err) { ElMessage.error(String(err.message || err)); }
}
function openChannelOrder() { reorderDraft.value = channels.value.map((item) => ({ id: item.id, name: item.name, sortOrder: Number(item.sortOrder || 100) })); reorderDialog.value = true; }
async function saveChannelOrder() {
  busy.value = true;
  try { for (let i = 0; i < reorderDraft.value.length; i++) await api.post('channels/save', { ...channels.value.find((c) => c.id === reorderDraft.value[i].id), sortOrder: (i + 1) * 10, apiKey: '' }); await load(); reorderDialog.value = false; ElMessage.success('排序已保存'); }
  catch (err) { ElMessage.error(String(err.message || err)); }
  finally { busy.value = false; }
}
async function bulkDeleteModels() {
  if (!selectedModelIds.value.length) return;
  try { await ElMessageBox.confirm(`将删除选中的 ${selectedModelIds.value.length} 个模型及其路由，是否继续？`, '批量删除模型'); for (const id of selectedModelIds.value) await api.post('channel-models/delete', { id }); selectedModelIds.value = []; await load(); ElMessage.success('选中模型已删除'); }
  catch (err) { if (err !== 'cancel') ElMessage.error(String(err.message || err)); }
}
async function bulkReprice() {
  const markup = Number(bulkMarkup.value);
  if (!Number.isFinite(markup) || markup < 0 || markup > 1000) { ElMessage.warning('加价比例需在 0% 至 1000% 之间'); return; }
  try { await ElMessageBox.confirm(`对选中的 ${selectedModelIds.value.length} 个按次模型应用成本加价 ${markup.toFixed(2)}%？`, '批量调价'); for (const item of visibleChannelModels.value.filter((m) => selectedModelIds.value.includes(m.id) && m.billingMode !== 'token')) { const cost = Math.max(0, Number(item.unitPriceMicro || 0)); const price = Math.round(cost * (10000 + markup * 100) / 10000); await api.post('channel-models/save', { ...item, unitPriceMicro: price }); } bulkPriceDialog.value = false; selectedModelIds.value = []; await load(); ElMessage.success('批量调价完成'); }
  catch (err) { if (err !== 'cancel') ElMessage.error(String(err.message || err)); }
}
async function toggleModel(row) {
  try { await api.post('channel-models/save', { ...row, enabled: !row.enabled }); await load(); }
  catch (err) { ElMessage.error(String(err.message || err)); }
}

const channelDialog = ref(false);
const channelForm = reactive({
  id: '', name: '', provider: 'openai-compat', baseUrl: '', apiKey: '', enabled: true,
  priority: 100, weight: 1, timeoutMs: 60000, notes: '', sortOrder: 100
});

const modelDialog = ref(false);
const modelForm = reactive({
  id: '', channelId: '', modelKey: '', displayName: '', capability: 'image', enabled: true,
  billingMode: 'unit', unitPriceMicro: 1000000, inputTokenPriceMicro: 0, outputTokenPriceMicro: 0,
  cachedTokenPriceMicro: 0, protocol: 'openai', upstreamModelKey: '', sortOrder: 100, note: ''
});

const logicalDialog = ref(false);
const logicalForm = reactive({
  id: '', code: '', name: '', icon: '', description: '', capability: 'image', enabled: true,
  sortOrder: 100, pricePolicy: 'channel', billingMode: 'per_page', unitPrice: 0, creditCost: 1, routes: []
});

const allChannelModels = computed(() => channels.value.flatMap((channel) =>
  (channel.models || []).map((model) => ({ ...model, channelName: channel.name }))
));

async function load() {
  loading.value = true;
  try {
    const data = await api.get('channels');
    channels.value = data.channels;
    models.value = data.models;
    if (!selectedChannelId.value && channels.value.length) selectedChannelId.value = channels.value[0].id;
    for (const channel of channels.value) {
      for (const model of channel.models || []) modelHealth.value[model.id] = { ok: model.lastCheckOk, at: model.lastCheckAt };
    }
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

function openNewChannel() {
  Object.assign(channelForm, {
    id: '', name: '', provider: 'openai-compat', baseUrl: '', apiKey: '', enabled: true,
    priority: 100, weight: 1, timeoutMs: 60000, notes: '', sortOrder: 100
  });
  channelDialog.value = true;
}

function openEditChannel(row) {
  Object.assign(channelForm, {
    id: row.id, name: row.name, provider: row.provider, baseUrl: row.baseUrl, apiKey: '',
    enabled: row.enabled, priority: row.priority, weight: row.weight, timeoutMs: row.timeoutMs,
    notes: row.notes, sortOrder: row.sortOrder
  });
  channelDialog.value = true;
}

async function saveChannel() {
  busy.value = true;
  try {
    const data = await api.post('channels/save', { ...channelForm });
    channels.value = data.channels;
    channelDialog.value = false;
    ElMessage.success('渠道已保存');
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    busy.value = false;
  }
}

async function testChannel(row) {
  busy.value = true;
  try {
    const data = await api.post('channels/test', { id: row.id });
    const result = data.results[0];
    channels.value = data.channels;
    if (result.ok) ElMessage.success(`${row.name}：${result.message}（${result.seconds ?? (result.ms / 1000).toFixed(3)} s）`);
    else ElMessage.error(`${row.name} 验证失败：${result.message}`);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    busy.value = false;
  }
}

async function testAllChannels() {
  busy.value = true;
  try {
    const data = await api.post('channels/test', { ids: channels.value.map((item) => item.id) });
    channels.value = data.channels;
    const okCount = data.results.filter((item) => item.ok).length;
    ElMessage[okCount === data.results.length ? 'success' : 'warning'](`接口验证：${okCount}/${data.results.length} 个渠道连通`);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    busy.value = false;
  }
}

async function syncModels(row) {
  pullChannel.value = row;
  pullModels.value = [];
  pullSelection.value = [];
  pullSearch.value = '';
  Object.keys(pullCapabilityMap).forEach((key) => delete pullCapabilityMap[key]);
  pullDialog.value = true;
  pullLoading.value = true;
  try {
    const data = await api.get(`channels/sync-models?id=${encodeURIComponent(row.id)}`);
    pullModels.value = data.models || [];
    pullModels.value.forEach((item) => { pullCapabilityMap[item.modelKey] = item.capability === 'image' ? 'image' : 'text'; });
    pullSelection.value = [];
  } catch (err) {
    pullDialog.value = false;
    ElMessage.error(String(err.message || err));
  } finally {
    pullLoading.value = false;
  }
}
function setPullSelection(rows) {
  const visible = new Set(filteredPullModels.value.map((item) => item.modelKey));
  const keepHidden = pullSelection.value.filter((key) => !visible.has(key));
  pullSelection.value = [...keepHidden, ...rows.map((item) => item.modelKey)];
}
function togglePullAll(checked) {
  const visibleKeys = filteredPullModels.value.filter((item) => !item.exists).map((item) => item.modelKey);
  pullSelection.value = checked ? [...new Set([...pullSelection.value, ...visibleKeys])] : pullSelection.value.filter((key) => !visibleKeys.includes(key));
}
async function confirmPullModels() {
  if (!pullSelection.value.length) { ElMessage.warning('请至少勾选一个模型'); return; }
  pullSaving.value = true;
  try {
    const data = await api.post('channels/sync-models', { id: pullChannel.value.id, models: pullSelection.value, capabilities: Object.fromEntries(pullSelection.value.map((key) => [key, pullCapabilityMap[key] || 'text'])) });
    channels.value = data.channels || channels.value;
    selectedChannelId.value = pullChannel.value.id;
    pullDialog.value = false;
    ElMessage.success(`已导入 ${data.added.length} 个模型；已存在模型已自动跳过`);
  } catch (err) { ElMessage.error(String(err.message || err)); }
  finally { pullSaving.value = false; }
}

async function removeChannel(row) {
  try {
    await ElMessageBox.confirm(`删除渠道「${row.name}」？它的渠道模型和路由会一起删掉。`, '确认', {
      confirmButtonText: '删除', cancelButtonText: '取消'
    });
    const data = await api.post('channels/delete', { id: row.id });
    channels.value = data.channels;
    ElMessage.success('已删除');
    load();
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

function openNewModel(channelId) {
  Object.assign(modelForm, {
    id: '', channelId: channelId || (channels.value[0] || {}).id || '', modelKey: '', displayName: '',
    capability: 'image', enabled: true, billingMode: 'unit',
    unitPriceMicro: 1000000, inputTokenPriceMicro: 0, outputTokenPriceMicro: 0, cachedTokenPriceMicro: 0,
    protocol: 'openai', upstreamModelKey: '', sortOrder: 100, note: ''
  });
  modelDialog.value = true;
}

function openEditModel(row) {
  Object.assign(modelForm, {
    id: row.id, channelId: row.channelId, modelKey: row.modelKey, displayName: row.displayName,
    capability: row.capability, enabled: row.enabled,
    billingMode: row.billingMode || 'unit',
    unitPriceMicro: Number(row.unitPriceMicro || 0),
    inputTokenPriceMicro: Number(row.inputTokenPriceMicro || 0),
    outputTokenPriceMicro: Number(row.outputTokenPriceMicro || 0),
    cachedTokenPriceMicro: Number(row.cachedTokenPriceMicro || 0),
    protocol: row.protocol || 'openai', upstreamModelKey: row.upstreamModelKey || '',
    sortOrder: Number(row.sortOrder || 100), note: row.note
  });
  modelDialog.value = true;
}

/* 微积分 ↔ 积分：界面上填"积分"，后端存微积分（1 积分 = 1,000,000） */
const microToCredits = (micro) => Number((Number(micro || 0) / 1000000).toFixed(6));
const creditsToMicro = (credits) => Math.round(Number(credits || 0) * 1000000);

function priceLabel(row) {
  if (row.billingMode === 'token') {
    return `按 token：入 ${microToCredits(row.inputTokenPriceMicro)} / 出 ${microToCredits(row.outputTokenPriceMicro)}（每百万）`;
  }
  return `按次 ${microToCredits(row.unitPriceMicro)} 积分`;
}

async function saveModel() {
  try {
    const data = await api.post('channel-models/save', { ...modelForm });
    channels.value = data.channels;
    modelDialog.value = false;
    ElMessage.success('渠道模型已保存');
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function testModel(row) {
  try {
    let confirmCost = false;
    if (row.capability === 'image') {
      await ElMessageBox.confirm(
        `图片模型验证会真实生成 1 张图（会产生费用）。确定测试「${row.modelKey}」？`,
        '验证接口',
        { confirmButtonText: '确认消耗一次', cancelButtonText: '取消' }
      );
      confirmCost = true;
    }
    busy.value = true;
    const data = await api.post('channel-models/test', { id: row.id, confirmCost });
    channels.value = data.channels;
    if (data.result.ok) ElMessage.success(`${row.modelKey} 可用（${data.result.seconds ?? (data.result.ms / 1000).toFixed(3)} s）`);
    else ElMessage.error(`${row.modelKey}：${data.result.message}`);
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  } finally {
    busy.value = false;
  }
}

async function removeModel(row) {
  try {
    await ElMessageBox.confirm(`删除渠道模型「${row.modelKey}」？`, '确认', { confirmButtonText: '删除', cancelButtonText: '取消' });
    const data = await api.post('channel-models/delete', { id: row.id });
    channels.value = data.channels;
    load();
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

function openNewLogical() {
  Object.assign(logicalForm, {
    id: '', code: '', name: '', icon: '', description: '', capability: 'image', enabled: true,
    sortOrder: 100, pricePolicy: 'channel', billingMode: 'per_page', unitPrice: 0, creditCost: 1, routes: []
  });
  logicalDialog.value = true;
}

function openEditLogical(row) {
  Object.assign(logicalForm, {
    id: row.id, code: row.code, name: row.name, icon: row.icon, description: row.description,
    capability: row.capability, enabled: row.enabled, sortOrder: row.sortOrder,
    pricePolicy: row.pricePolicy, billingMode: row.billingMode,
    unitPrice: row.unitPrice, creditCost: row.creditCost,
    routes: (row.routes || []).map((route) => ({
      channelModelId: route.channelModelId, enabled: route.enabled, priority: route.priority, weight: route.weight
    }))
  });
  logicalDialog.value = true;
}

async function setDefault(row) {
  busy.value = true;
  try {
    await api.post('models/set-default', { id: row.id, capability: row.capability });
    await load();
    ElMessage.success(`已把「${row.name}」设为${row.capability === 'text' ? '文本' : '图片'}统一默认模型`);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    busy.value = false;
  }
}

function addRoute(channelModelId) {
  if (!channelModelId) return;
  if (logicalForm.routes.some((route) => route.channelModelId === channelModelId)) return;
  logicalForm.routes.push({ channelModelId, enabled: true, priority: 100, weight: 1 });
}

async function saveLogical() {
  try {
    const data = await api.post('logical-models/save', { ...logicalForm });
    models.value = data.models;
    logicalDialog.value = false;
    ElMessage.success('逻辑模型已保存');
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function removeLogical(row) {
  try {
    await ElMessageBox.confirm(`删除逻辑模型「${row.name}」？`, '确认', { confirmButtonText: '删除', cancelButtonText: '取消' });
    const data = await api.post('logical-models/delete', { id: row.id });
    models.value = data.models;
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

function routeLabel(channelModelId) {
  const found = allChannelModels.value.find((item) => item.id === channelModelId);
  return found ? `${found.channelName} / ${found.modelKey}` : channelModelId;
}

onMounted(load);
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">模型与渠道</div>
        <div class="adm-sub">
          三层结构：渠道（谁提供服务）→ 渠道模型（上游真实 model key）→ 逻辑模型（对外暴露 + 路由 + 计价）
        </div>
      </div>
      <div class="adm-toolbar" style="margin: 0">
        <el-button size="small" :loading="busy" @click="testAllChannels()">全部接口验证</el-button>
        <el-button size="small" type="primary" @click="openNewChannel()">新增渠道</el-button>
        <el-button size="small" :loading="importing" @click="importChannels">导入</el-button>
      </div>
    </div>

    <input ref="importInput" type="file" accept="application/json,.json" hidden @change="handleImport" />
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="oitv-channel-switch">
      <el-radio-group v-model="channelView" size="small">
        <el-radio-button label="by-channel">按渠道</el-radio-button>
        <el-radio-button label="all-models">全部模型</el-radio-button>
      </el-radio-group>
      <div class="adm-toolbar" style="margin:0"><el-button size="small" @click="exportChannels">导出全部</el-button></div>
    </div>
    <div v-if="channelView === 'by-channel'" class="oitv-channel-workspace">
      <aside class="oitv-channel-list">
        <div class="oitv-list-head"><b>渠道</b><span>{{ channels.length }}</span></div>
        <button v-for="row in channels" :key="row.id" type="button" class="oitv-channel-item" :class="{ active: selectedChannel?.id === row.id }" @click="selectChannel(row)">
          <span class="oitv-channel-avatar">{{ String(row.name || '渠').slice(0, 1) }}</span>
          <span class="oitv-channel-meta"><b>{{ row.name }}</b><small>{{ row.models?.length || 0 }} 个模型 · {{ row.lastCheckOk === false ? '验证失败' : '已启用' }}</small></span><i :class="row.enabled ? 'is-on' : ''" />
        </button>
        <button type="button" class="oitv-sort-button" @click="openNewChannel">＋ 新增渠道</button>
      </aside>
      <section v-if="selectedChannel" class="oitv-channel-detail">
        <div class="oitv-channel-detail-head"><div><h3>{{ selectedChannel.name }}</h3><p>{{ selectedChannel.enabled ? '已启用' : '已停用' }} · {{ selectedChannel.models?.length || 0 }} 个模型</p></div><el-tag :type="selectedChannel.enabled ? 'success' : 'info'" effect="plain">{{ selectedChannel.enabled ? '已启用' : '已停用' }}</el-tag></div>
        <div class="oitv-channel-info"><span>{{ selectedChannel.baseUrl || '未配置接口地址' }}</span><span>{{ selectedChannel.hasKey ? 'API Key 已配置' : 'API Key 未配置' }}</span><span>最大并发：跟随系统</span></div>
        <div class="oitv-channel-actions"><el-button size="small" @click="openEditChannel(selectedChannel)">编辑</el-button><el-button size="small" @click="copyChannel(selectedChannel)">复制</el-button><el-button size="small" @click="exportChannels">导出</el-button><el-button size="small" @click="testChannel(selectedChannel)">验证</el-button><el-button size="small" type="danger" plain @click="removeChannel(selectedChannel)">删除</el-button></div>
        <div class="oitv-model-manager-head"><div><h3>模型管理 <span>{{ selectedChannel.models?.length || 0 }}</span></h3><p>稳定性只统计模型生成请求；勾选模型可统一调价或批量删除</p></div><div class="adm-toolbar" style="margin:0"><el-button size="small" @click="openChannelOrder">自定义排序</el-button><el-button size="small" @click="syncModels(selectedChannel)">拉取模型</el-button><el-button size="small" type="primary" @click="openNewModel(selectedChannel.id)">新增模型</el-button></div></div>
        <div class="oitv-model-filters"><el-input v-model="modelSearch" size="small" placeholder="搜索模型标识或显示名称" clearable style="width:240px" /><el-select v-model="modelCapabilityFilter" size="small" placeholder="全部能力" clearable style="width:120px"><el-option v-for="item in CAPABILITIES" :key="item.value" :label="item.label" :value="item.value" /></el-select><el-select v-model="modelStatusFilter" size="small" placeholder="全部状态" clearable style="width:120px"><el-option label="启用" value="enabled" /><el-option label="停用" value="disabled" /></el-select><el-button v-if="selectedModelIds.length" size="small" @click="bulkPriceDialog = true">批量调价 {{ selectedModelIds.length }}</el-button><el-button v-if="selectedModelIds.length" size="small" type="danger" plain @click="bulkDeleteModels">批量删除</el-button></div>
        <div class="adm-table-wrap oitv-model-table"><el-table :data="visibleChannelModels" size="small" empty-text="还没有模型" @selection-change="(rows) => selectedModelIds = rows.map((item) => item.id)"><el-table-column type="selection" width="44" /><el-table-column label="模型" min-width="180"><template #default="{ row }"><b>{{ row.displayName || '未分类' }}</b><div class="adm-mono">{{ row.modelKey }}</div></template></el-table-column><el-table-column label="能力" width="80"><template #default="{ row }">{{ (CAPABILITIES.find((item) => item.value === row.capability) || {}).label || row.capability }}</template></el-table-column><el-table-column label="稳定性（24h）" width="130"><template #default="{ row }"><span :class="row.lastCheckOk === false ? 'adm-bad' : 'adm-ok'">{{ row.lastCheckOk === false ? '验证失败' : '待统计' }}</span></template></el-table-column><el-table-column label="请求协议" width="150"><template #default="{ row }">{{ row.protocol || 'OpenAI' }}</template></el-table-column><el-table-column label="规格成本价 / 销售价 / 加价" min-width="210"><template #default="{ row }">{{ priceLabel(row) }}</template></el-table-column><el-table-column label="版本" width="70">v1</el-table-column><el-table-column label="状态" width="75"><template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'" effect="plain">{{ row.enabled ? '启用' : '停用' }}</el-tag></template></el-table-column><el-table-column label="操作" width="220" fixed="right"><template #default="{ row }"><el-button link size="small" @click="openEditModel(row)">编辑</el-button><el-button link size="small" @click="toggleModel(row)">{{ row.enabled ? '停用' : '启用' }}</el-button><el-button link size="small" :loading="busy" @click="testModel(row)">测试</el-button><el-button link size="small" type="danger" @click="removeModel(row)">删除</el-button></template></el-table-column></el-table></div>
      </section>
    </div>
    <div v-if="channelView === 'all-models'" class="oitv-all-models"><div class="adm-toolbar"><el-input v-model="modelSearch" size="small" placeholder="搜索模型标识或显示名称" clearable style="width:280px" /><el-select v-model="modelCapabilityFilter" size="small" placeholder="全部能力" clearable style="width:130px"><el-option v-for="item in CAPABILITIES" :key="item.value" :label="item.label" :value="item.value" /></el-select><el-select v-model="modelStatusFilter" size="small" placeholder="全部状态" clearable style="width:130px"><el-option label="启用" value="enabled" /><el-option label="停用" value="disabled" /></el-select></div><div class="adm-table-wrap"><el-table :data="visibleChannelModels" size="small" empty-text="还没有模型"><el-table-column label="模型" min-width="190"><template #default="{ row }"><b>{{ row.displayName || '未分类' }}</b><div class="adm-mono">{{ row.modelKey }}</div></template></el-table-column><el-table-column prop="channelName" label="渠道" width="170" /><el-table-column label="能力" width="80"><template #default="{ row }">{{ (CAPABILITIES.find((item) => item.value === row.capability) || {}).label || row.capability }}</template></el-table-column><el-table-column label="稳定性（24h）" width="130"><template #default="{ row }"><span :class="row.lastCheckOk === false ? 'adm-bad' : 'adm-ok'">{{ row.lastCheckOk === false ? '验证失败' : '待统计' }}</span></template></el-table-column><el-table-column label="请求协议" width="150"><template #default="{ row }">{{ row.protocol || 'OpenAI' }}</template></el-table-column><el-table-column label="规格成本价 / 售价 / 加价" min-width="210"><template #default="{ row }">{{ priceLabel(row) }}</template></el-table-column><el-table-column label="版本" width="70">v1</el-table-column><el-table-column label="状态" width="75"><template #default="{ row }"><el-tag size="small" :type="row.enabled ? 'success' : 'info'" effect="plain">{{ row.enabled ? '启用' : '停用' }}</el-tag></template></el-table-column><el-table-column label="操作" width="220" fixed="right"><template #default="{ row }"><el-button link size="small" @click="openEditModel(row)">编辑</el-button><el-button link size="small" @click="toggleModel(row)">{{ row.enabled ? '停用' : '启用' }}</el-button><el-button link size="small" :loading="busy" @click="testModel(row)">测试</el-button><el-button link size="small" type="danger" @click="removeModel(row)">删除</el-button></template></el-table-column></el-table></div></div>
    <el-tabs v-model="tab" class="legacy-tabs">
      <el-tab-pane v-if="false" label="旧渠道表" name="legacy-channels">
        <div class="adm-table-wrap">
          <el-table :data="channels" size="small" :empty-text="loading ? '加载中…' : '还没有渠道'">
            <el-table-column label="名称" min-width="170">
              <template #default="{ row }">
                {{ row.name }}
                <span v-if="!row.enabled" class="adm-chip gray" style="margin-left: 6px">已停用</span>
              </template>
            </el-table-column>
            <el-table-column label="类型" width="180">
              <template #default="{ row }">{{ (PROVIDERS.find((item) => item.value === row.provider) || {}).label || row.provider }}</template>
            </el-table-column>
            <el-table-column label="接口地址" min-width="230" show-overflow-tooltip>
              <template #default="{ row }"><span class="adm-mono">{{ row.baseUrl || '（空）' }}</span></template>
            </el-table-column>
            <el-table-column label="Key" width="120">
              <template #default="{ row }">
                <span class="adm-mono">{{ row.hasKey ? row.apiKeyMasked : '未填' }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="models.length" label="模型" width="70" />
            <el-table-column label="优先级/权重" width="120">
              <template #default="{ row }">{{ row.priority }} / {{ row.weight }}</template>
            </el-table-column>
            <el-table-column label="接口验证" width="190">
              <template #default="{ row }">
                <span v-if="!row.lastCheckAt" class="adm-muted">未验证</span>
                <span v-else :class="row.lastCheckOk ? 'adm-ok' : 'adm-bad'" :title="row.lastCheckMsg">
                  {{ row.lastCheckOk ? '通过' : '失败' }} · {{ row.lastCheckMs || 0 }}ms · {{ fmtTime(row.lastCheckAt) }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="270" fixed="right">
              <template #default="{ row }">
                <el-button link size="small" :loading="busy" @click="testChannel(row)">验证</el-button>
                <el-button link size="small" @click="syncModels(row)">拉模型</el-button>
                <el-button link size="small" @click="openNewModel(row.id)">加模型</el-button>
                <el-button link size="small" @click="openEditChannel(row)">编辑</el-button>
                <el-button link size="small" type="danger" @click="removeChannel(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div class="adm-muted" style="font-size: 12px; margin-top: 8px">
          「验证」= 请求 {baseUrl}/models 拉模型列表，成功即链路通（不发真实生成请求，不花钱）。
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="false" label="旧渠道模型" name="channel-models">
        <div class="adm-toolbar">
          <el-button size="small" type="primary" @click="openNewModel('')">新增渠道模型</el-button>
          <span class="adm-muted" style="font-size: 12px">同一个上游模型可以属于不同渠道；逻辑模型通过路由挑它</span>
        </div>
        <div class="adm-table-wrap">
          <el-table :data="allChannelModels" size="small" empty-text="还没有渠道模型">
            <el-table-column prop="channelName" label="渠道" width="180" show-overflow-tooltip />
            <el-table-column label="模型 Key" min-width="220">
              <template #default="{ row }"><span class="adm-mono">{{ row.modelKey }}</span></template>
            </el-table-column>
            <el-table-column label="能力" width="90">
              <template #default="{ row }">{{ (CAPABILITIES.find((item) => item.value === row.capability) || {}).label || row.capability }}</template>
            </el-table-column>
            <el-table-column label="计费" width="230">
              <template #default="{ row }">
                <span class="adm-chip" :class="row.billingMode === 'token' ? 'blue' : 'gray'" style="margin-right: 6px">
                  {{ row.billingMode === 'token' ? '按 token' : '按次' }}
                </span>
                <span style="font-size: 12px">{{ priceLabel(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <span class="adm-chip" :class="row.enabled ? 'green' : 'gray'">{{ row.enabled ? '启用' : '停用' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="接口验证" width="190">
              <template #default="{ row }">
                <span v-if="!row.lastCheckAt" class="adm-muted">未验证</span>
                <span v-else :class="row.lastCheckOk ? 'adm-ok' : 'adm-bad'" :title="row.lastCheckMsg">
                  {{ row.lastCheckOk ? '通过' : '失败' }} · {{ fmtTime(row.lastCheckAt) }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="200" fixed="right">
              <template #default="{ row }">
                <el-button link size="small" :loading="busy" @click="testModel(row)">验证</el-button>
                <el-button link size="small" @click="openEditModel(row)">编辑</el-button>
                <el-button link size="small" type="danger" @click="removeModel(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <el-tab-pane label="逻辑模型" name="logical">
        <div class="adm-toolbar">
          <el-button size="small" type="primary" @click="openNewLogical()">新增逻辑模型</el-button>
          <span class="adm-muted" style="font-size: 12px">对创作端暴露的是逻辑模型；路由按优先级 + 权重选渠道模型，全挂时会自动跳过</span>
        </div>
        <div class="adm-table-wrap">
          <el-table :data="models" size="small" empty-text="还没有逻辑模型">
            <el-table-column label="名称" min-width="200">
              <template #default="{ row }">
                {{ row.icon }} {{ row.name }}
                <span v-if="row.isDefault" class="adm-chip green" style="margin-left: 6px">统一默认</span>
                <span v-if="!row.enabled" class="adm-chip gray" style="margin-left: 6px">停用</span>
              </template>
            </el-table-column>
            <el-table-column label="Code" width="180">
              <template #default="{ row }"><span class="adm-mono">{{ row.code }}</span></template>
            </el-table-column>
            <el-table-column label="能力" width="80">
              <template #default="{ row }">{{ (CAPABILITIES.find((item) => item.value === row.capability) || {}).label || row.capability }}</template>
            </el-table-column>
            <el-table-column label="计价" width="150">
              <template #default="{ row }">
                {{ row.pricePolicy === 'channel' ? '跟渠道' : '统一价' }} / {{ row.billingMode === 'token' ? '按 token' : '按次' }}
              </template>
            </el-table-column>
            <el-table-column label="积分价" width="90">
              <template #default="{ row }">{{ row.creditCost }}</template>
            </el-table-column>
            <el-table-column label="路由" min-width="260">
              <template #default="{ row }">
                <el-tag
                  v-for="route in row.routes"
                  :key="route.id"
                  size="small"
                  :type="route.available ? 'success' : 'info'"
                  effect="plain"
                  style="margin: 2px 4px 2px 0"
                >{{ route.channelName }} / {{ route.channelModelKey }} · P{{ route.priority }} W{{ route.weight }}</el-tag>
                <span v-if="!row.routes.length" class="adm-muted">未配置路由</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="200" fixed="right">
              <template #default="{ row }">
                <el-button v-if="!row.isDefault" link size="small" :loading="busy" @click="setDefault(row)">设为默认</el-button>
                <el-button link size="small" @click="openEditLogical(row)">编辑</el-button>
                <el-button link size="small" type="danger" @click="removeLogical(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="channelDialog" :title="channelForm.id ? '编辑渠道' : '新增渠道'" width="520px">
      <el-form label-width="100px" size="small">
        <el-form-item label="名称"><el-input v-model="channelForm.name" placeholder="例如：oiai 中转" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="channelForm.provider" style="width: 100%">
            <el-option v-for="item in PROVIDERS" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="接口地址">
          <el-input v-model="channelForm.baseUrl" placeholder="https://api.example.com/v1" />
        </el-form-item>
        <el-form-item label="API Key">
          <el-input v-model="channelForm.apiKey" type="password" show-password :placeholder="channelForm.id ? '留空 = 不改' : 'sk-...'" />
        </el-form-item>
        <el-form-item label="超时(ms)">
          <el-input-number v-model="channelForm.timeoutMs" :min="5000" :max="600000" :step="5000" />
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="channelForm.priority" :min="1" :max="9999" />
        </el-form-item>
        <el-form-item label="权重">
          <el-input-number v-model="channelForm.weight" :min="1" :max="100" />
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="channelForm.enabled" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="channelForm.notes" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="channelDialog = false">取消</el-button>
        <el-button size="small" type="primary" :loading="busy" @click="saveChannel()">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="modelDialog" :title="modelForm.id ? '编辑渠道模型' : '新增渠道模型'" width="480px">
      <el-form label-width="100px" size="small">
        <el-form-item label="渠道">
          <el-select v-model="modelForm.channelId" style="width: 100%">
            <el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="channel.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型 Key">
          <el-input v-model="modelForm.modelKey" placeholder="gpt-image-1 / seedream-4.0" />
        </el-form-item>
        <el-form-item label="显示名"><el-input v-model="modelForm.displayName" placeholder="留空 = 同 Key" /></el-form-item>
        <el-form-item label="能力">
          <el-select v-model="modelForm.capability" style="width: 100%">
            <el-option v-for="item in CAPABILITIES" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="计费方式">
          <el-radio-group v-model="modelForm.billingMode">
            <el-radio-button value="unit">按次调用</el-radio-button>
            <el-radio-button value="token">按 token</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="modelForm.billingMode === 'unit'" label="每次价格">
          <el-input-number
            :model-value="microToCredits(modelForm.unitPriceMicro)"
            :min="0" :max="100000" :precision="6" :step="1"
            @update:model-value="(v) => (modelForm.unitPriceMicro = creditsToMicro(v))"
          />
          <span class="adm-muted" style="margin-left: 8px">积分 / 次（成功才扣）</span>
        </el-form-item>
        <template v-else>
          <el-form-item label="输入单价">
            <el-input-number
              :model-value="microToCredits(modelForm.inputTokenPriceMicro)"
              :min="0" :max="100000" :precision="6" :step="0.5"
              @update:model-value="(v) => (modelForm.inputTokenPriceMicro = creditsToMicro(v))"
            />
            <span class="adm-muted" style="margin-left: 8px">积分 / 百万 input token</span>
          </el-form-item>
          <el-form-item label="输出单价">
            <el-input-number
              :model-value="microToCredits(modelForm.outputTokenPriceMicro)"
              :min="0" :max="100000" :precision="6" :step="0.5"
              @update:model-value="(v) => (modelForm.outputTokenPriceMicro = creditsToMicro(v))"
            />
            <span class="adm-muted" style="margin-left: 8px">积分 / 百万 output token</span>
          </el-form-item>
          <el-form-item label="缓存单价">
            <el-input-number
              :model-value="microToCredits(modelForm.cachedTokenPriceMicro)"
              :min="0" :max="100000" :precision="6" :step="0.1"
              @update:model-value="(v) => (modelForm.cachedTokenPriceMicro = creditsToMicro(v))"
            />
            <span class="adm-muted" style="margin-left: 8px">积分 / 百万 cached token，可留 0</span>
          </el-form-item>
        </template>
        <el-form-item label="保底按次价">
          <el-input-number
            :model-value="microToCredits(modelForm.unitPriceMicro)"
            :min="0" :max="100000" :precision="6"
            @update:model-value="(v) => (modelForm.unitPriceMicro = creditsToMicro(v))"
          />
          <span class="adm-muted" style="margin-left: 8px">{{ modelForm.billingMode === 'token' ? '上游没返回 usage 时按这个价兜底' : '就是上面的每次价格' }}</span>
        </el-form-item>
        <el-form-item label="协议">
          <el-select v-model="modelForm.protocol" style="width: 100%">
            <el-option label="OpenAI 兼容" value="openai" />
            <el-option label="Anthropic" value="anthropic" />
            <el-option label="Gemini" value="gemini" />
          </el-select>
        </el-form-item>
        <el-form-item label="上游模型名"><el-input v-model="modelForm.upstreamModelKey" placeholder="留空 = 同模型 Key" /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="modelForm.sortOrder" :min="1" :max="9999" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="modelForm.enabled" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="modelForm.note" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="modelDialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="saveModel()">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="pullDialog" :title="`拉取模型 · ${pullChannel?.name || ''}`" width="720px" destroy-on-close>
      <div class="pull-model-toolbar">
        <el-input v-model="pullSearch" size="small" clearable placeholder="搜索上游模型名称" style="width:260px" />
        <span class="adm-muted">已按模型名称自动判断能力；上游共 {{ pullModels.length }} 个，已选 {{ pullSelection.length }} 个</span>
      </div>
      <div v-loading="pullLoading" class="pull-model-picker">
        <div v-if="!pullLoading && !filteredPullModels.length" class="pull-empty">没有匹配的上游模型</div>
        <el-table v-else :data="filteredPullModels" size="small" height="390" row-key="modelKey" :default-selection="filteredPullModels.filter((item) => pullSelection.includes(item.modelKey))" @selection-change="setPullSelection">
          <el-table-column type="selection" width="48" :selectable="(row) => !row.exists" />
          <el-table-column label="上游模型" min-width="300"><template #default="{ row }"><span class="adm-mono">{{ row.modelKey }}</span></template></el-table-column>
          <el-table-column label="能力" width="120"><template #default="{ row }"><el-select v-model="pullCapabilityMap[row.modelKey]" size="small" :disabled="row.exists" style="width:100%"><el-option label="图片模型" value="image" /><el-option label="文本模型" value="text" /></el-select></template></el-table-column>
          <el-table-column label="状态" width="130"><template #default="{ row }"><el-tag v-if="row.exists" size="small" type="info" effect="plain">已在管理列表</el-tag><span v-else class="adm-muted">待导入</span></template></el-table-column>
        </el-table>
      </div>
      <template #footer><el-button size="small" @click="pullDialog = false">取消</el-button><el-button size="small" type="primary" :loading="pullSaving" :disabled="pullLoading || !pullSelection.length" @click="confirmPullModels">确认导入 {{ pullSelection.length }} 个</el-button></template>
    </el-dialog>

    <el-dialog v-model="reorderDialog" title="自定义排序" width="460px">
      <div v-for="(item, index) in reorderDraft" :key="item.id" class="adm-sort-row"><b>{{ index + 1 }}</b><span>{{ item.name }}</span><el-input-number v-model="item.sortOrder" :min="1" :max="9999" size="small" /></div>
      <template #footer><el-button size="small" @click="reorderDialog = false">取消</el-button><el-button size="small" type="primary" :loading="busy" @click="saveChannelOrder">保存排序</el-button></template>
    </el-dialog>
    <el-dialog v-model="bulkPriceDialog" title="批量调价" width="420px">
      <el-form label-width="110px" size="small"><el-form-item label="成本加价比例"><el-input-number v-model="bulkMarkup" :min="0" :max="1000" :precision="2" /><span class="adm-muted" style="margin-left:8px">%</span></el-form-item></el-form>
      <template #footer><el-button size="small" @click="bulkPriceDialog = false">取消</el-button><el-button size="small" type="primary" @click="bulkReprice">应用调价</el-button></template>
    </el-dialog>

    <el-dialog v-model="logicalDialog" :title="logicalForm.id ? '编辑逻辑模型' : '新增逻辑模型'" width="620px">
      <el-form label-width="100px" size="small">
        <el-form-item label="Code"><el-input v-model="logicalForm.code" placeholder="manga-image-default" /></el-form-item>
        <el-form-item label="名称"><el-input v-model="logicalForm.name" placeholder="默认漫画模型" /></el-form-item>
        <el-form-item label="图标"><el-input v-model="logicalForm.icon" placeholder="选填，emoji 或图片地址" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="logicalForm.description" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="能力">
          <el-select v-model="logicalForm.capability" style="width: 100%">
            <el-option v-for="item in CAPABILITIES" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="计价策略">
          <el-radio-group v-model="logicalForm.pricePolicy">
            <el-radio value="channel">跟渠道单价</el-radio>
            <el-radio value="unified">统一价</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="计费方式">
          <el-radio-group v-model="logicalForm.billingMode">
            <el-radio value="per_page">按页 / 按次</el-radio>
            <el-radio value="token">按 token</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="单价"><el-input-number v-model="logicalForm.unitPrice" :min="0" :max="10000" :precision="2" /></el-form-item>
        <el-form-item label="积分价"><el-input-number v-model="logicalForm.creditCost" :min="0" :max="10000" /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="logicalForm.sortOrder" :min="1" :max="9999" /></el-form-item>
        <el-form-item label="启用"><el-switch v-model="logicalForm.enabled" /></el-form-item>
        <el-form-item label="新增路由">
          <el-select placeholder="选一个渠道模型" style="width: 100%" @change="addRoute">
            <el-option
              v-for="item in allChannelModels"
              :key="item.id"
              :label="`${item.channelName} / ${item.modelKey}（${item.capability}）`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="路由列表">
          <div style="width: 100%">
            <div v-for="(route, index) in logicalForm.routes" :key="index" style="display: flex; gap: 8px; align-items: center; margin-bottom: 6px">
              <span style="flex: 1; font-size: 12px">{{ routeLabel(route.channelModelId) }}</span>
              <el-input-number v-model="route.priority" :min="1" :max="9999" size="small" controls-position="right" style="width: 96px" />
              <el-input-number v-model="route.weight" :min="1" :max="100" size="small" controls-position="right" style="width: 84px" />
              <el-switch v-model="route.enabled" size="small" />
              <el-button link size="small" type="danger" @click="logicalForm.routes.splice(index, 1)">移除</el-button>
            </div>
            <div v-if="!logicalForm.routes.length" class="adm-muted" style="font-size: 12px">还没有路由</div>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="logicalDialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="saveLogical()">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
