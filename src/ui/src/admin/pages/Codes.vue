<script setup>
import { onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { api, fmtTime, qs } from '../api';

const rows = ref([]);
const total = ref(0);
const stats = ref({ unused: 0, used: 0, issuedCredits: 0 });
const loading = ref(false);
const error = ref('');
const filter = reactive({ status: '', batch: '', page: 1, pageSize: 30 });
const dialog = ref(false);
const form = reactive({ count: 20, credits: 100, batch: '', prefix: 'MANGA', expiresAt: '', note: '' });
const created = ref([]);

async function load() {
  loading.value = true;
  try {
    const data = await api.get('codes' + qs(filter));
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

async function create() {
  try {
    const result = await api.post('codes/create', { ...form });
    created.value = result.codes;
    ElMessage.success(`已生成 ${result.count} 个兑换码（每码 ${result.credits} 积分）`);
    dialog.value = false;
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function disable(row) {
  try {
    await ElMessageBox.confirm(`作废兑换码 ${row.code}？`, '确认', { confirmButtonText: '作废', cancelButtonText: '取消' });
    await api.post('codes/disable', { id: row.id, status: 'disabled' });
    ElMessage.success('已作废');
    load();
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

async function enable(row) {
  try {
    await api.post('codes/disable', { id: row.id, status: 'unused' });
    ElMessage.success('已恢复可用');
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function copyAll() {
  try {
    await navigator.clipboard.writeText(created.value.join('\n'));
    ElMessage.success('已复制到剪贴板');
  } catch {
    ElMessage.warning('浏览器不让复制，手动选中吧');
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">兑换码</div>
        <div class="adm-sub">批量生成、按批次管理、作废/恢复；用户在创作端「设置」里自己兑换</div>
      </div>
      <el-button size="small" type="primary" @click="dialog = true">批量生成</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-3" style="margin-bottom: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">未使用</div>
        <div class="adm-kpi-value">{{ stats.unused }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">已使用</div>
        <div class="adm-kpi-value">{{ stats.used }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">已兑出积分</div>
        <div class="adm-kpi-value">{{ stats.issuedCredits }}</div>
      </div>
    </div>

    <div v-if="created.length" class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar">
        <span style="font-weight: 600">刚生成的 {{ created.length }} 个码</span>
        <div class="adm-spacer" />
        <el-button size="small" @click="copyAll()">复制全部</el-button>
        <el-button size="small" @click="created = []">收起</el-button>
      </div>
      <div class="adm-mono" style="max-height: 160px; overflow: auto; line-height: 1.7">{{ created.join('　') }}</div>
    </div>

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <el-input v-model="filter.batch" placeholder="批次" size="small" style="width: 160px" clearable @keyup.enter="load" />
        <el-select v-model="filter.status" placeholder="状态" size="small" style="width: 140px" clearable>
          <el-option label="未使用" value="unused" />
          <el-option label="已使用" value="used" />
          <el-option label="已作废" value="disabled" />
        </el-select>
        <el-button size="small" type="primary" @click="filter.page = 1; load()">查询</el-button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <el-table :data="rows" size="small" :empty-text="loading ? '加载中…' : '还没有兑换码'">
        <el-table-column prop="code" label="兑换码" width="200">
          <template #default="{ row }"><span class="adm-mono">{{ row.code }}</span></template>
        </el-table-column>
        <el-table-column prop="credits" label="积分" width="90" />
        <el-table-column prop="batch" label="批次" width="150" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <span class="adm-chip" :class="row.status === 'unused' ? 'green' : row.status === 'used' ? 'gray' : 'red'">
              {{ { unused: '未使用', used: '已使用', disabled: '已作废' }[row.status] || row.status }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="创建" width="170">
          <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="使用" width="170">
          <template #default="{ row }">{{ fmtTime(row.usedAt) }}</template>
        </el-table-column>
        <el-table-column prop="usedBy" label="使用者" width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="110" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'unused'" link size="small" type="danger" @click="disable(row)">作废</el-button>
            <el-button v-else-if="row.status === 'disabled'" link size="small" @click="enable(row)">恢复</el-button>
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

    <el-dialog v-model="dialog" title="批量生成兑换码" width="440px">
      <el-form label-width="90px">
        <el-form-item label="数量">
          <el-input-number v-model="form.count" :min="1" :max="500" />
        </el-form-item>
        <el-form-item label="每码积分">
          <el-input-number v-model="form.credits" :min="1" :max="100000" />
        </el-form-item>
        <el-form-item label="前缀">
          <el-input v-model="form.prefix" placeholder="MANGA" />
        </el-form-item>
        <el-form-item label="批次名">
          <el-input v-model="form.batch" placeholder="留空自动按日期生成" />
        </el-form-item>
        <el-form-item label="过期时间">
          <el-date-picker v-model="form.expiresAt" type="datetime" placeholder="留空 = 不过期" style="width: 100%" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" placeholder="选填，例如：双十一活动" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="dialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="create()">生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>
