<script setup>
import { onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { api, fmtTime } from '../api';

const rows = ref([]);
const loading = ref(false);
const error = ref('');
const keyword = ref('');
const defaults = ref({ dailyPages: 80, pagesPerMinute: 4 });
const detail = ref(null);
const drawer = ref(false);
const adjust = reactive({ userId: '', delta: 10, note: '' });
const adjustVisible = ref(false);
const resetPwd = reactive({ userId: '', password: '' });
const resetVisible = ref(false);

async function load() {
  loading.value = true;
  try {
    const data = await api.get('users' + (keyword.value ? `?keyword=${encodeURIComponent(keyword.value)}` : ''));
    rows.value = data.users;
    defaults.value = data.defaults;
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

async function openDetail(row) {
  drawer.value = true;
  detail.value = null;
  try {
    detail.value = await api.get('users/' + row.id);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function saveQuota(row, value) {
  try {
    await api.post('quota', { userId: row.id, dailyPageQuota: value });
    ElMessage.success(`已把 ${row.username} 的每日额度改成 ${value}`);
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function toggleAdmin(row) {
  try {
    await api.post('admin', { userId: row.id, isAdmin: !row.isAdmin });
    ElMessage.success(row.isAdmin ? '已取消管理员' : '已设为管理员');
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function toggleBan(row) {
  const banned = row.status === 'banned';
  try {
    if (banned) {
      await api.post('user-status', { userId: row.id, status: 'active' });
      ElMessage.success('已恢复');
    } else {
      const { value } = await ElMessageBox.prompt('停用后该用户立即下线且不能再登录，填个原因：', '停用账号', {
        inputPlaceholder: '例如：批量灌水', confirmButtonText: '停用', cancelButtonText: '取消'
      });
      await api.post('user-status', { userId: row.id, status: 'banned', reason: value || '' });
      ElMessage.success('已停用');
    }
    load();
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

function openAdjust(row) {
  adjust.userId = row.id;
  adjust.delta = 10;
  adjust.note = '';
  adjustVisible.value = true;
}

async function submitAdjust() {
  try {
    const result = await api.post('credits/adjust', { ...adjust });
    ElMessage.success(`积分已调整，当前余额 ${result.balance}`);
    adjustVisible.value = false;
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

function openReset(row) {
  resetPwd.userId = row.id;
  resetPwd.password = '';
  resetVisible.value = true;
}

async function submitReset() {
  try {
    await api.post('password', { userId: resetPwd.userId, password: resetPwd.password });
    ElMessage.success('密码已重置，该用户所有设备下线');
    resetVisible.value = false;
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">用户管理</div>
        <div class="adm-sub">额度、积分、管理员权限、停用与重置密码；点用户名看明细（项目 / 积分流水 / 订单 / 兑换记录）</div>
      </div>
      <div class="adm-toolbar" style="margin: 0">
        <el-input v-model="keyword" placeholder="搜索用户名 / 昵称" size="small" style="width: 190px" clearable @keyup.enter="load" />
        <el-button size="small" type="primary" @click="load()">查询</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-table-wrap">
      <el-table :data="rows" size="small" :empty-text="loading ? '加载中…' : '还没有用户'">
        <el-table-column label="用户" min-width="180">
          <template #default="{ row }">
            <a style="cursor: pointer; color: #4a55c7" @click="openDetail(row)">{{ row.username }}</a>
            <span v-if="row.displayName && row.displayName !== row.username" class="adm-muted"> （{{ row.displayName }}）</span>
            <span v-if="row.isAdmin" class="adm-chip" style="margin-left: 6px">管理员</span>
            <span v-if="row.status === 'banned'" class="adm-chip red" style="margin-left: 6px">已停用</span>
          </template>
        </el-table-column>
        <el-table-column label="积分" width="110">
          <template #default="{ row }">{{ row.credits }}</template>
        </el-table-column>
        <el-table-column label="今日 / 额度" width="130">
          <template #default="{ row }">
            <span :class="row.dailyPageQuota && row.today.pages >= row.dailyPageQuota ? 'adm-bad' : ''">
              {{ row.today.pages }} / {{ row.dailyPageQuota || '不限' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="累计" width="150">
          <template #default="{ row }">{{ row.usage.pages }} 页 · {{ row.usage.images }} 图</template>
        </el-table-column>
        <el-table-column prop="projectCount" label="项目" width="70" />
        <el-table-column label="最后登录" width="170">
          <template #default="{ row }">{{ fmtTime(row.lastLoginAt) }}</template>
        </el-table-column>
        <el-table-column label="每日额度" width="170">
          <template #default="{ row }">
            <el-input-number
              :model-value="row.dailyPageQuota"
              :min="0"
              :max="10000"
              size="small"
              controls-position="right"
              style="width: 110px"
              @change="(value) => saveQuota(row, value)"
            />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="260" fixed="right">
          <template #default="{ row }">
            <el-button link size="small" @click="openAdjust(row)">改积分</el-button>
            <el-button link size="small" @click="toggleAdmin(row)">{{ row.isAdmin ? '取消管理员' : '设为管理员' }}</el-button>
            <el-button link size="small" @click="openReset(row)">重置密码</el-button>
            <el-button link size="small" :type="row.status === 'banned' ? 'success' : 'danger'" @click="toggleBan(row)">
              {{ row.status === 'banned' ? '恢复' : '停用' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-drawer v-model="drawer" title="用户明细" size="560px">
      <div v-if="!detail" class="adm-muted">加载中…</div>
      <template v-else>
        <div class="adm-card" style="margin-bottom: 12px">
          <div style="font-weight: 600">{{ detail.user.username }}</div>
          <div class="adm-muted" style="margin-top: 4px">
            积分 {{ detail.user.credits }} · 今日额度 {{ detail.user.dailyPageQuota || '不限' }} ·
            累计 {{ detail.user.usage.pages }} 页 / {{ detail.user.usage.images }} 图
          </div>
          <div class="adm-muted" style="margin-top: 4px">
            注册 {{ fmtTime(detail.user.createdAt) }} · 最后登录 {{ fmtTime(detail.user.lastLoginAt) }}
          </div>
        </div>

        <div style="font-weight: 600; margin: 14px 0 8px">项目（{{ detail.projects.length }}）</div>
        <el-table :data="detail.projects" size="small">
          <el-table-column prop="name" label="名称" show-overflow-tooltip />
          <el-table-column label="创建" width="170">
            <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
          </el-table-column>
        </el-table>

        <div style="font-weight: 600; margin: 16px 0 8px">积分流水</div>
        <el-table :data="detail.ledger" size="small" empty-text="暂无">
          <el-table-column label="时间" width="160">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column label="变动" width="80">
            <template #default="{ row }">
              <span :class="row.delta >= 0 ? 'adm-ok' : 'adm-bad'">{{ row.delta > 0 ? '+' : '' }}{{ row.delta }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="kind" label="类型" width="90" />
          <el-table-column prop="note" label="说明" show-overflow-tooltip />
        </el-table>

        <div style="font-weight: 600; margin: 16px 0 8px">充值订单</div>
        <el-table :data="detail.payments" size="small" empty-text="暂无">
          <el-table-column label="时间" width="160">
            <template #default="{ row }">{{ fmtTime(row.at) }}</template>
          </el-table-column>
          <el-table-column prop="amount" label="金额" width="80" />
          <el-table-column prop="credits" label="积分" width="80" />
          <el-table-column prop="status" label="状态" width="90" />
        </el-table>
      </template>
    </el-drawer>

    <el-dialog v-model="adjustVisible" title="调整积分" width="380px">
      <el-form label-width="70px">
        <el-form-item label="变动">
          <el-input-number v-model="adjust.delta" :min="-100000" :max="100000" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="adjust.note" placeholder="例如：活动补发 / 手工充值" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="adjustVisible = false">取消</el-button>
        <el-button size="small" type="primary" @click="submitAdjust()">确定</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="resetVisible" title="重置密码" width="380px">
      <el-form label-width="80px">
        <el-form-item label="新密码">
          <el-input v-model="resetPwd.password" placeholder="至少 6 位" show-password />
        </el-form-item>
      </el-form>
      <div class="adm-muted" style="font-size: 12px">重置后该用户所有设备会立即下线。</div>
      <template #footer>
        <el-button size="small" @click="resetVisible = false">取消</el-button>
        <el-button size="small" type="primary" @click="submitReset()">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>
