<script setup>
import { onMounted, reactive, ref } from 'vue';
import { api, fmtTime, qs } from '../api';

const rows = ref([]);
const total = ref(0);
const loading = ref(false);
const error = ref('');
const filter = reactive({ username: '', ok: '', page: 1, pageSize: 30 });

async function load() {
  loading.value = true;
  try {
    const data = await api.get('login-logs' + qs(filter));
    rows.value = data.items;
    total.value = data.total;
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">登录日志</div>
        <div class="adm-sub">成功和失败都记：用来查"谁在撞库""谁家 IP 一直登录失败"</div>
      </div>
      <el-button size="small" :loading="loading" @click="load()">刷新</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-card" style="margin-bottom: 14px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <el-input v-model="filter.username" placeholder="用户名" size="small" style="width: 170px" clearable @keyup.enter="load" />
        <el-select v-model="filter.ok" placeholder="结果" size="small" style="width: 130px" clearable>
          <el-option label="成功" value="true" />
          <el-option label="失败" value="false" />
        </el-select>
        <el-button size="small" type="primary" @click="filter.page = 1; load()">查询</el-button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <el-table :data="rows" size="small" :empty-text="loading ? '加载中…' : '还没有登录记录'">
        <el-table-column prop="at" label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.at) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" width="140" />
        <el-table-column prop="ok" label="结果" width="80">
          <template #default="{ row }">
            <span class="adm-chip" :class="row.ok ? 'green' : 'red'">{{ row.ok ? '成功' : '失败' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="reason" label="说明" min-width="200" show-overflow-tooltip />
        <el-table-column prop="ip" label="IP" width="140" />
        <el-table-column prop="ua" label="设备 / UA" min-width="220" show-overflow-tooltip />
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
  </div>
</template>
