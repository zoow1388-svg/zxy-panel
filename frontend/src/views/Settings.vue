<script setup lang="ts">
import { reactive, ref } from 'vue'
import { api, clearToken } from '../api'
import { useRouter } from 'vue-router'

const router = useRouter()
const form = reactive({ old_password: '', new_password: '', confirm_password: '' })
const loading = ref(false)
const error = ref('')
const success = ref('')

async function changePassword() {
  error.value = ''
  success.value = ''
  if (!form.old_password || !form.new_password) {
    error.value = '请输入旧密码和新密码。'
    return
  }
  if (form.new_password.length < 8) {
    error.value = '新密码至少 8 位。'
    return
  }
  if (form.new_password !== form.confirm_password) {
    error.value = '两次输入的新密码不一致。'
    return
  }
  loading.value = true
  try {
    await api('/api/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ old_password: form.old_password, new_password: form.new_password })
    })
    success.value = '管理员密码已修改，请使用新密码重新登录。'
    form.old_password = ''
    form.new_password = ''
    form.confirm_password = ''
    setTimeout(() => { clearToken(); router.push('/login') }, 1200)
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1 class="page-title">系统设置</h1>
        <p class="page-desc">当前版本先提供管理员密码修改。后续会加入面板名称、Logo、HTTPS、备份和升级设置。</p>
      </div>
    </div>

    <div class="notice warn">
      首次部署后请立即修改默认密码。修改成功后会自动退出登录，需要用新密码重新登录。
    </div>

    <div v-if="success" class="success">{{ success }}</div>
    <div v-if="error" class="error">{{ error }}</div>

    <div class="card settings-card">
      <h2>修改管理员密码</h2>
      <label class="field">
        <span>旧密码</span>
        <input v-model="form.old_password" type="password" placeholder="当前密码" />
      </label>
      <label class="field">
        <span>新密码</span>
        <input v-model="form.new_password" type="password" placeholder="至少 8 位" />
      </label>
      <label class="field">
        <span>确认新密码</span>
        <input v-model="form.confirm_password" type="password" placeholder="再次输入新密码" />
      </label>
      <button class="btn" :disabled="loading" @click="changePassword">{{ loading ? '保存中...' : '保存新密码' }}</button>
    </div>
  </div>
</template>
