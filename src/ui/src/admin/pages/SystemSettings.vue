<script setup>
import { onMounted, reactive, ref } from 'vue';
import { ElMessage } from 'element-plus';
import { api } from '../api';

const activeTab = ref('registration');
const loading = ref(false);
const saving = ref(false);
const settings = reactive({
  registration: { open: true, inviteRequired: true, allowPasswordReset: true, defaultDailyPages: 80, welcomeMessage: '' },
  email: { enabled: false, host: '', port: 465, secure: true, from: '', username: '', password: '' },
  billing: { autoRefund: false, refundFailedCalls: true, refundPendingMinutes: 30 },
  redemption: { enabled: false }
});

async function load() {
  loading.value = true;
  try {
    const response = await api.get('settings');
    Object.assign(settings.registration, response.settings.registration || {});
    Object.assign(settings.email, response.settings.email || {});
    Object.assign(settings.billing, response.settings.billing || {});
    Object.assign(settings.redemption, response.settings.redemption || {});
    settings.email.password = '';
  } catch (error) {
    ElMessage.error(String(error.message || error));
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  try {
    const payload = { [activeTab.value]: { ...settings[activeTab.value] } };
    await api.post('settings', payload);
    settings.email.password = '';
    ElMessage.success('设置已保存');
    await load();
  } catch (error) {
    ElMessage.error(String(error.message || error));
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
        <div class="adm-title">系统设置</div>
        <div class="adm-sub">注册策略、邮件服务和计费异常处理；与现有站点、积分和支付设置分区保存。</div>
      </div>
      <el-button size="small" type="primary" :loading="saving" @click="save">保存当前设置</el-button>
    </div>
    <el-tabs v-model="activeTab">
      <el-tab-pane label="注册与访问" name="registration">
        <div class="adm-card sys-setting-card" v-loading="loading">
          <div class="sys-setting-title">注册策略</div>
          <el-form label-width="180px" size="default">
            <el-form-item label="开放注册"><el-switch v-model="settings.registration.open" /></el-form-item>
            <el-form-item label="注册需要邀请码"><el-switch v-model="settings.registration.inviteRequired" /></el-form-item>
            <el-form-item label="允许找回密码"><el-switch v-model="settings.registration.allowPasswordReset" /></el-form-item>
            <el-form-item label="新用户默认每日页数">
              <el-input-number v-model="settings.registration.defaultDailyPages" :min="0" :max="10000" />
            </el-form-item>
            <el-form-item label="注册欢迎信息">
              <el-input v-model="settings.registration.welcomeMessage" type="textarea" :rows="3" placeholder="可留空" />
            </el-form-item>
          </el-form>
          <el-alert title="网站管理中的注册开关仍作为创作端兼容配置；此处的细分选项独立保存。" type="info" :closable="false" show-icon />
        </div>
      </el-tab-pane>
      <el-tab-pane label="邮件服务" name="email">
        <div class="adm-card sys-setting-card" v-loading="loading">
          <div class="sys-setting-title">SMTP 配置</div>
          <el-form label-width="180px" size="default">
            <el-form-item label="启用邮件服务"><el-switch v-model="settings.email.enabled" /></el-form-item>
            <el-form-item label="SMTP 主机"><el-input v-model="settings.email.host" placeholder="smtp.example.com" /></el-form-item>
            <el-form-item label="端口"><el-input-number v-model="settings.email.port" :min="1" :max="65535" /></el-form-item>
            <el-form-item label="TLS/SSL"><el-switch v-model="settings.email.secure" /></el-form-item>
            <el-form-item label="发件人地址"><el-input v-model="settings.email.from" placeholder="noreply@example.com" /></el-form-item>
            <el-form-item label="SMTP 用户名"><el-input v-model="settings.email.username" autocomplete="off" /></el-form-item>
            <el-form-item label="SMTP 密码"><el-input v-model="settings.email.password" type="password" show-password autocomplete="new-password" placeholder="留空保持现有密码" /></el-form-item>
          </el-form>
          <el-alert title="邮件参数会保存供账户通知功能使用；此页面不会发送测试邮件。" type="info" :closable="false" show-icon />
        </div>
      </el-tab-pane>
      <el-tab-pane label="兑换码" name="redemption">
        <div class="adm-card sys-setting-card" v-loading="loading">
          <div class="sys-setting-title">兑换码设置</div>
          <el-form label-width="180px" size="default">
            <el-form-item label="启用兑换码"><el-switch v-model="settings.redemption.enabled" /></el-form-item>
          </el-form>
          <el-alert title="关闭后前台不显示兑换入口，兑换接口也会返回未开启。" type="info" :closable="false" show-icon />
        </div>
      </el-tab-pane>
      <el-tab-pane label="计费异常" name="billing">
        <div class="adm-card sys-setting-card" v-loading="loading">
          <div class="sys-setting-title">失败调用退款策略</div>
          <el-form label-width="180px" size="default">
            <el-form-item label="自动处理异常扣费"><el-switch v-model="settings.billing.autoRefund" /></el-form-item>
            <el-form-item label="失败调用自动退款"><el-switch v-model="settings.billing.refundFailedCalls" /></el-form-item>
            <el-form-item label="待处理超时（分钟）">
              <el-input-number v-model="settings.billing.refundPendingMinutes" :min="1" :max="1440" />
            </el-form-item>
          </el-form>
          <el-alert title="当前系统失败调用不会扣费。此设置用于记录后台策略，自动退款任务需在后续支付运营阶段接入。" type="warning" :closable="false" show-icon />
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped>
.sys-setting-card { max-width: 900px; }
.sys-setting-title { font-size: 15px; font-weight: 650; margin-bottom: 18px; }
.sys-setting-card :deep(.el-form-item) { max-width: 680px; }
.sys-setting-card :deep(.el-alert) { margin-top: 10px; }
</style>
