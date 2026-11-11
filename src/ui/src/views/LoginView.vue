<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { auth } from '@/api/endpoints'
import { setToken } from '@/api/http'
import { useAppStore } from '@/stores/app'

const router = useRouter()
const route = useRoute()
const store = useAppStore()

const form = reactive({ username: '', password: '' })
const busy = ref(false)
// 多用户：注册要邀请码（服务端 AI_COMIC_INVITE_CODE），没有码的人只能登录
const registerMode = ref(false)
const regForm = reactive({ username: '', password: '', inviteCode: '' })

async function submit() {
  if (!form.username || !form.password) {
    ElMessage.warning('填一下用户名和密码')
    return
  }
  busy.value = true
  try {
    const res = await auth.login(form.username.trim(), form.password)
    setToken(res.token)
    store.user = res.user
    const to = typeof route.query.to === 'string' && route.query.to ? route.query.to : '/projects'
    router.replace(to)
  } catch (err: any) {
    ElMessage.error(err?.message || '登录失败')
  } finally {
    busy.value = false
  }
}

async function submitRegister() {
  if (!regForm.username || !regForm.password) {
    ElMessage.warning('填一下用户名和密码')
    return
  }
  busy.value = true
  try {
    const res: any = await fetch('/api/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({
        username: regForm.username.trim(),
        password: regForm.password,
        inviteCode: regForm.inviteCode.trim()
      })
    }).then(async (r) => {
      const data = await r.json().catch(() => ({}))
      if (!r.ok || data.success === false) throw new Error(data.userMessage || data.error || `HTTP ${r.status}`)
      return data
    })
    setToken(res.token)
    store.user = res.user
    ElMessage.success('注册成功，已登录')
    router.replace('/projects')
  } catch (err: any) {
    ElMessage.error(err?.message || '注册失败')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <div class="login-card panel">
      <h1 class="panel-title">AI 漫画 · 创作台</h1>
      <p class="panel-sub">
        {{ registerMode ? '用邀请码开一个自己的账号，项目和数据都跟你一个人走。' : '登录后继续你的项目：分镜、设定图、整页出图都在同一个工作台里。' }}
      </p>

      <el-form v-if="!registerMode" label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="form.username" autocomplete="username" placeholder="账号" size="large" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            placeholder="密码"
            size="large"
            show-password
            @keyup.enter="submit"
          />
        </el-form-item>
        <el-button type="primary" size="large" :loading="busy" style="width: 100%" @click="submit">
          登录
        </el-button>
      </el-form>

      <el-form v-else label-position="top" @submit.prevent="submitRegister">
        <el-form-item label="用户名">
          <el-input v-model="regForm.username" placeholder="2-24 位，中英文/数字/下划线" size="large" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="regForm.password" type="password" placeholder="至少 6 位" size="large" show-password />
        </el-form-item>
        <el-form-item label="邀请码">
          <el-input v-model="regForm.inviteCode" placeholder="向管理员要" size="large" @keyup.enter="submitRegister" />
        </el-form-item>
        <el-button type="primary" size="large" :loading="busy" style="width: 100%" @click="submitRegister">
          注册并登录
        </el-button>
      </el-form>

      <p class="login-foot muted">
        <template v-if="registerMode">
          已有账号？<el-button text size="small" @click="registerMode = false">回去登录</el-button>
        </template>
        <template v-else>
          一个账号一套项目，登录后所有设备看到的是同一份进度。
          <el-button text size="small" @click="registerMode = true">有邀请码？注册</el-button>
        </template>
      </p>
    </div>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.login-card { width: 100%; max-width: 400px; padding: 26px 26px 18px; }
.login-foot { margin: 14px 0 0; font-size: 12.5px; text-align: center; }
</style>
