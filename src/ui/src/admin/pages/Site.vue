<script setup>
import { onMounted, ref } from 'vue';
import { ElMessage } from 'element-plus';
import { api } from '../api';

const loading = ref(false);
const saving = ref(false);
const error = ref('');
const settings = ref({
  site: { name: '', subtitle: '', announcement: '', icp: '', registerOpen: true, inviteRequired: true, maintenance: false, landingNote: '' },
  credits: { enabled: false, signupBonusMicro: 0, lowBalanceWarnMicro: 10000000, chargeAdmin: true, welcomeNote: '' },
  limits: { dailyPages: 80, pagesPerMinute: 4, maxConcurrentPages: 8 },
  payment: { methods: ['manual'], ratePerYuan: 10, note: 'manual' }
});

async function load() {
  loading.value = true;
  try {
    const data = await api.get('site');
    settings.value = {
      site: { ...settings.value.site, ...(data.settings.site || {}) },
      credits: { ...settings.value.credits, ...(data.settings.credits || {}) },
      limits: { ...settings.value.limits, ...(data.settings.limits || {}) },
      payment: { ...settings.value.payment, ...(data.settings.payment || {}) }
    };
    error.value = '';
  } catch (err) {
    error.value = String(err.message || err);
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    await api.post('site', settings.value);
    ElMessage.success('已保存');
  } catch (err) {
    ElMessage.error(String(err.message || err));
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="adm-head">
      <div>
        <div class="adm-title">网站管理</div>
        <div class="adm-sub">站点信息、注册策略、积分开关、额度上限、充值规则；保存即生效（创作端刷新可见）</div>
      </div>
      <el-button size="small" type="primary" :loading="saving" @click="save()">保存</el-button>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

    <div class="adm-grid adm-grid-2">
      <div class="adm-card">
        <div style="font-weight: 600; margin-bottom: 12px">站点</div>
        <el-form label-width="96px" size="small">
          <el-form-item label="站点名称"><el-input v-model="settings.site.name" /></el-form-item>
          <el-form-item label="副标题"><el-input v-model="settings.site.subtitle" /></el-form-item>
          <el-form-item label="公告">
            <el-input v-model="settings.site.announcement" type="textarea" :rows="3" placeholder="显示在创作端顶部，可留空" />
          </el-form-item>
          <el-form-item label="备案号"><el-input v-model="settings.site.icp" placeholder="选填" /></el-form-item>
          <el-form-item label="落地页说明"><el-input v-model="settings.site.landingNote" placeholder="选填" /></el-form-item>
          <el-form-item label="开放注册"><el-switch v-model="settings.site.registerOpen" /></el-form-item>
          <el-form-item label="需要邀请码"><el-switch v-model="settings.site.inviteRequired" /></el-form-item>
          <el-form-item label="维护模式"><el-switch v-model="settings.site.maintenance" /></el-form-item>
        </el-form>
      </div>

      <div>
        <div class="adm-card" style="margin-bottom: 14px">
          <div style="font-weight: 600; margin-bottom: 12px">积分</div>
          <el-form label-width="110px" size="small">
            <el-form-item label="启用积分计费">
              <el-switch v-model="settings.credits.enabled" />
              <span class="adm-muted" style="margin-left: 8px; font-size: 12px">关掉 = 不扣积分；开了以后"调用成功才扣，失败不扣"</span>
            </el-form-item>
            <el-form-item label="管理员也扣费">
              <el-switch v-model="settings.credits.chargeAdmin" />
              <span class="adm-muted" style="margin-left: 8px; font-size: 12px">开着 = 管理员调用同样记扣费流水（后台才看得到自己的消耗）；余额不足不拦</span>
            </el-form-item>
            <el-form-item label="注册赠送">
              <el-input-number v-model="settings.credits.signupBonusMicro" :min="0" :max="100000000" />
              <span class="adm-muted" style="margin-left: 8px; font-size: 12px">微积分（1 积分 = 1,000,000）</span>
            </el-form-item>
            <el-form-item label="余额提醒">
              <el-input-number v-model="settings.credits.lowBalanceWarnMicro" :min="0" :max="100000000" />
              <span class="adm-muted" style="margin-left: 8px; font-size: 12px">低于这个微积分就提示充值</span>
            </el-form-item>
            <el-form-item label="钱包说明">
              <el-input v-model="settings.credits.welcomeNote" placeholder="显示在钱包页顶部" />
            </el-form-item>
          </el-form>
        </div>

        <div class="adm-card" style="margin-bottom: 14px">
          <div style="font-weight: 600; margin-bottom: 12px">创作限额（默认值，单人可在用户管理里覆盖）</div>
          <el-form label-width="110px" size="small">
            <el-form-item label="每日页数">
              <el-input-number v-model="settings.limits.dailyPages" :min="0" :max="10000" />
            </el-form-item>
            <el-form-item label="每分钟页数">
              <el-input-number v-model="settings.limits.pagesPerMinute" :min="0" :max="200" />
            </el-form-item>
            <el-form-item label="并发出图">
              <el-input-number v-model="settings.limits.maxConcurrentPages" :min="1" :max="64" />
            </el-form-item>
          </el-form>
        </div>

        <div class="adm-card">
          <div style="font-weight: 600; margin-bottom: 12px">充值</div>
          <el-form label-width="110px" size="small">
            <el-form-item label="1 元 = 积分">
              <el-input-number v-model="settings.payment.ratePerYuan" :min="0" :max="10000" />
            </el-form-item>
            <el-form-item label="充值说明">
              <el-input v-model="settings.payment.note" type="textarea" :rows="2" />
            </el-form-item>
          </el-form>
        </div>
      </div>
    </div>
  </div>
</template>
