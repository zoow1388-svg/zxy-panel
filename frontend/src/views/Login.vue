<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, setToken } from '../api'
const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
async function login() {
  error.value = ''
  try {
    const res = await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username: username.value, password: password.value }) })
    setToken(res.token)
    router.push('/')
  } catch (e: any) { error.value = e.message }
}
</script>
<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-logo">Z</div>
      <h1>ZXY Panel</h1>
      <p class="login-desc">跨境业务专线节点管理面板。登录后可管理入站、客户订阅、节点链接和系统检测。</p>
      <input v-model="username" placeholder="管理员账号" @keyup.enter="login" />
      <input v-model="password" placeholder="管理员密码" type="password" @keyup.enter="login" />
      <button class="btn" style="width:100%" @click="login">进入控制台</button>
      <div class="error" v-if="error">{{ error }}</div>
      <p class="code">请使用安装完成后终端输出的随机管理员账号和密码登录。</p>
    </div>
  </div>
</template>
