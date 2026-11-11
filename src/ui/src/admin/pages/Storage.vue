<script setup>
import { computed, onMounted, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { api, fmtBytes, fmtTime } from '../api';

const KINDS = [
  { value: 'local', label: '服务器本地磁盘' },
  { value: 's3', label: 'S3 / MinIO / R2（S3 兼容）' },
  { value: 'oss', label: '阿里云 OSS（S3 兼容端点）' },
  { value: 'cos', label: '腾讯云 COS（S3 兼容端点）' },
  { value: 'kodo', label: '七牛 Kodo（S3 兼容端点）' },
  { value: 'webdav', label: 'WebDAV（坚果云 / Nextcloud）' }
];

const targets = ref([]);
const writes = ref([]);
const stats = ref({ writes: { total: 0, bytes: 0, failures: 0, avgMs: 0 }, disk: { used: 0, total: 0 } });
const loading = ref(false);
const error = ref('');
const selected = ref([]);
const dialog = ref(false);
const testing = ref(false);
const writeChecking = ref(false);
const form = reactive({
  id: '', name: '', kind: 'local', endpoint: '', bucket: '', region: '', accessKey: '', secretKey: '',
  publicBase: '', rootPrefix: '', writeEnabled: true, readEnabled: true, priority: 100
});

const isS3 = computed(() => ['s3', 'oss', 'cos', 'kodo'].includes(form.kind));

async function load() {
  loading.value = true;
  try {
    const data = await api.get('storage');
    targets.value = data.targets;
    writes.value = data.writes;
    stats.value = { writes: data.writes, disk: data.disk };
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

function openNew() {
  Object.assign(form, {
    id: '', name: '', kind: 'local', endpoint: '', bucket: '', region: '', accessKey: '', secretKey: '',
    publicBase: '', rootPrefix: '', writeEnabled: true, readEnabled: true, priority: 100
  });
  dialog.value = true;
}

function openEdit(row) {
  Object.assign(form, {
    id: row.id, name: row.name, kind: row.kind, endpoint: row.endpoint, bucket: row.bucket,
    region: row.region, accessKey: '', secretKey: '', publicBase: row.publicBase,
    rootPrefix: row.rootPrefix, writeEnabled: row.writeEnabled, readEnabled: row.readEnabled, priority: row.priority
  });
  dialog.value = true;
}

async function save() {
  try {
    const data = await api.post('storage/save', { ...form });
    targets.value = data.targets;
    dialog.value = false;
    ElMessage.success('已保存');
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除存储目标「${row.name}」？只删配置，已写进去的文件不动。`, '确认', {
      confirmButtonText: '删除', cancelButtonText: '取消'
    });
    const data = await api.post('storage/delete', { id: row.id });
    targets.value = data.targets;
    ElMessage.success('已删除');
  } catch (err) {
    if (err !== 'cancel') ElMessage.error(String(err.message || err));
  }
}

async function testTargets(ids) {
  testing.value = true;
  try {
    const data = await api.post('storage/test', { ids });
    const okCount = data.results.filter((item) => item.ok).length;
    targets.value = data.targets;
    ElMessage[okCount === data.results.length ? 'success' : 'warning'](
      `${okCount}/${data.results.length} 个目标可用`
    );
    for (const item of data.results.filter((result) => !result.ok)) {
      ElMessage.warning(`${item.name}：${item.message}`);
    }
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    testing.value = false;
  }
}

async function batchWrite(enabled) {
  if (!selected.value.length) { ElMessage.warning('先勾选目标'); return; }
  try {
    const data = await api.post('storage/write-targets', { ids: selected.value, writeEnabled: enabled });
    targets.value = data.targets;
    ElMessage.success(`已${enabled ? '开启' : '关闭'} ${data.updated} 个目标的写入`);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function makePrimary(row) {
  try {
    const data = await api.post('storage/write-targets', { ids: [row.id], writeEnabled: true, makePrimary: true });
    targets.value = data.targets;
    ElMessage.success(`主存储已切到「${row.name}」`);
  } catch (err) {
    ElMessage.error(String(err.message || err));
  }
}

async function writeCheck() {
  writeChecking.value = true;
  try {
    const data = await api.post('storage/write-check', {});
    const okCount = data.results.filter((item) => item.ok).length;
    writes.value = data.writes;
    ElMessage[okCount === data.results.length ? 'success' : 'warning'](`写入检查：${okCount}/${data.results.length} 成功（key=${data.key}）`);
    load();
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    writeChecking.value = false;
  }
}

const writeDialog = ref(false);
const writeForm = reactive({ key: 'uploads/hello.txt', text: 'hello from admin storage api', ids: [] });

async function submitWrite() {
  try {
    const data = await api.post('storage/write', { ...writeForm, contentType: 'text/plain' });
    writes.value = data.writes;
    writeDialog.value = false;
    ElMessage[data.okCount === data.results.length ? 'success' : 'warning'](
      `写入 ${data.key}（${data.bytes} B）：${data.okCount}/${data.results.length} 个目标成功`
    );
    for (const item of data.results) {
      if (item.url) ElMessage.success(`访问地址：${item.url}`);
      if (!item.ok) ElMessage.error(`${item.name}：${item.error}`);
    }
    load();
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
        <div class="adm-title">储存服务</div>
        <div class="adm-sub">
          多个存储目标并存：勾选哪些"可写"（多选），出图/上传就写到哪几个；主存储决定外部访问地址
        </div>
      </div>
      <div class="adm-toolbar" style="margin: 0">
        <el-button size="small" :loading="writeChecking" @click="writeCheck()">写入检查（写进所有勾选目标）</el-button>
        <el-button size="small" @click="writeDialog = true">自定义写入</el-button>
        <el-button size="small" type="primary" @click="openNew()">新增存储</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-4" style="margin-bottom: 14px">
      <div class="adm-card">
        <div class="adm-kpi-label">存储目标</div>
        <div class="adm-kpi-value">{{ targets.length }}</div>
        <div class="adm-kpi-harm">可写 {{ targets.filter((item) => item.writeEnabled).length }} 个</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">7 日写入</div>
        <div class="adm-kpi-value">{{ stats.writes?.total ?? 0 }}</div>
        <div class="adm-kpi-harm">失败 {{ stats.writes?.failures ?? 0 }} · 平均 {{ stats.writes?.avgMs ?? 0 }} ms</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">7 日写入字节</div>
        <div class="adm-kpi-value">{{ fmtBytes(stats.writes?.bytes) }}</div>
      </div>
      <div class="adm-card">
        <div class="adm-kpi-label">磁盘占用</div>
        <div class="adm-kpi-value">
          {{ stats.disk?.total ? Math.round((stats.disk.used / stats.disk.total) * 100) : 0 }}%
        </div>
        <div class="adm-kpi-harm">{{ fmtBytes(stats.disk?.used) }} / {{ fmtBytes(stats.disk?.total) }}</div>
      </div>
    </div>

    <div class="adm-card" style="margin-bottom: 12px">
      <div class="adm-toolbar" style="margin-bottom: 0">
        <span class="adm-muted" style="font-size: 12px">已选 {{ selected.length }} 个：</span>
        <el-button size="small" @click="batchWrite(true)">开启写入</el-button>
        <el-button size="small" @click="batchWrite(false)">关闭写入</el-button>
        <div class="adm-spacer" />
        <el-button size="small" :loading="testing" @click="testTargets(selected.length ? selected : targets.map((item) => item.id))">
          测试连通（{{ selected.length || targets.length }}）
        </el-button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <el-table
        :data="targets"
        size="small"
        :empty-text="loading ? '加载中…' : '还没有存储目标'"
        @selection-change="(rows) => (selected = rows.map((row) => row.id))"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column label="名称" min-width="170">
          <template #default="{ row }">
            {{ row.name }}
            <span v-if="row.isPrimary" class="adm-chip" style="margin-left: 6px">主存储</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="130">
          <template #default="{ row }">{{ (KINDS.find((item) => item.value === row.kind) || {}).label || row.kind }}</template>
        </el-table-column>
        <el-table-column label="地址 / 桶" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="adm-mono">{{ row.kind === 'local' ? '数据目录' : (row.endpoint || '-') }}{{ row.bucket ? ' / ' + row.bucket : '' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Key" width="120">
          <template #default="{ row }"><span class="adm-mono">{{ row.accessKey || '-' }}</span></template>
        </el-table-column>
        <el-table-column label="写入" width="80">
          <template #default="{ row }">
            <span class="adm-chip" :class="row.writeEnabled ? 'green' : 'gray'">{{ row.writeEnabled ? '开' : '关' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="读取" width="70">
          <template #default="{ row }">{{ row.readEnabled ? '开' : '关' }}</template>
        </el-table-column>
        <el-table-column label="最后验证" width="180">
          <template #default="{ row }">
            <span v-if="!row.lastCheckAt" class="adm-muted">未验证</span>
            <span v-else :class="row.lastCheckOk ? 'adm-ok' : 'adm-bad'" :title="row.lastCheckMsg">
              {{ row.lastCheckOk ? '通过' : '失败' }} · {{ fmtTime(row.lastCheckAt) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link size="small" :loading="testing" @click="testTargets([row.id])">测试</el-button>
            <el-button link size="small" @click="openEdit(row)">编辑</el-button>
            <el-button v-if="!row.isPrimary" link size="small" @click="makePrimary(row)">设为主</el-button>
            <el-button link size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="adm-card" style="margin-top: 16px">
      <div style="font-weight: 600; margin-bottom: 10px">最近写入</div>
      <el-table :data="writes" size="small" empty-text="还没有写入记录（点上面「写入检查」试一次）">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.at) }}</template>
        </el-table-column>
        <el-table-column prop="targetId" label="目标" width="160" show-overflow-tooltip />
        <el-table-column prop="objectKey" label="对象" min-width="200" show-overflow-tooltip />
        <el-table-column label="大小" width="90">
          <template #default="{ row }">{{ fmtBytes(row.bytes) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <span :class="row.ok ? 'adm-ok' : 'adm-bad'">{{ row.ok ? '成功' : '失败' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="ms" label="耗时" width="90">
          <template #default="{ row }">{{ row.ms }} ms</template>
        </el-table-column>
        <el-table-column prop="error" label="错误" min-width="160" show-overflow-tooltip />
      </el-table>
    </div>

    <el-dialog v-model="writeDialog" title="自定义写入（写入接口）" width="520px">
      <el-form label-width="90px" size="small">
        <el-form-item label="对象路径">
          <el-input v-model="writeForm.key" placeholder="uploads/test.txt" />
        </el-form-item>
        <el-form-item label="文本内容">
          <el-input v-model="writeForm.text" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="目标">
          <el-select v-model="writeForm.ids" multiple placeholder="留空 = 所有勾选为可写的目标" style="width: 100%">
            <el-option v-for="item in targets" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <div class="adm-muted" style="font-size: 12px; line-height: 1.7">
        接口：<span class="adm-mono">POST /api/admin/storage/write</span><br />
        参数：<span class="adm-mono">{ key, text | contentBase64, contentType, ids? }</span>；每次写入都会落到 `storage_writes` 里留痕。
      </div>
      <template #footer>
        <el-button size="small" @click="writeDialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="submitWrite()">写入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dialog" :title="form.id ? '编辑存储目标' : '新增存储目标'" width="520px">
      <el-form label-width="100px" size="small">
        <el-form-item label="名称"><el-input v-model="form.name" placeholder="例如：阿里 OSS 主库" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.kind" style="width: 100%">
            <el-option v-for="item in KINDS" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <template v-if="form.kind !== 'local'">
          <el-form-item label="Endpoint">
            <el-input v-model="form.endpoint" placeholder="https://oss-cn-hangzhou.aliyuncs.com" />
          </el-form-item>
          <el-form-item v-if="isS3" label="Bucket"><el-input v-model="form.bucket" placeholder="my-bucket" /></el-form-item>
          <el-form-item v-if="isS3" label="Region"><el-input v-model="form.region" placeholder="us-east-1" /></el-form-item>
          <el-form-item label="AccessKey">
            <el-input v-model="form.accessKey" :placeholder="form.id ? '留空 = 不改' : 'AK'" />
          </el-form-item>
          <el-form-item label="SecretKey">
            <el-input v-model="form.secretKey" type="password" :placeholder="form.id ? '留空 = 不改' : 'SK'" show-password />
          </el-form-item>
        </template>
        <el-form-item label="访问前缀">
          <el-input v-model="form.publicBase" placeholder="可选：https://cdn.example.com" />
        </el-form-item>
        <el-form-item label="目录前缀">
          <el-input v-model="form.rootPrefix" placeholder="可选：comic/images" />
        </el-form-item>
        <el-form-item label="可写">
          <el-switch v-model="form.writeEnabled" />
          <span class="adm-muted" style="margin-left: 8px; font-size: 12px">勾选后新文件会写到这里</span>
        </el-form-item>
        <el-form-item label="可读"><el-switch v-model="form.readEnabled" /></el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="form.priority" :min="1" :max="9999" />
          <span class="adm-muted" style="margin-left: 8px; font-size: 12px">数字小的先写</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" @click="dialog = false">取消</el-button>
        <el-button size="small" type="primary" @click="save()">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
