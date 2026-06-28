<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const logs = ref<any[]>([])
const error = ref('')
const message = ref('')
const loading = ref(false)
const limit = ref(200)

const actionLabels: Record<string, string> = {
  'auth.login': '登录后台',
  'auth.login_failed': '登录失败',
  'auth.logout': '退出登录',
  'admin.change_password': '修改管理员密码',
  'node.create': '创建节点',
  'node.update': '修改节点',
  'node.delete': '删除节点',
  'client.create': '添加客户端',
  'client.update': '修改客户端',
  'client.delete': '删除客户端',
  'server.create': '添加服务器',
  'server.update': '修改服务器',
  'server.delete': '删除服务器',
  'server.sync': 'Agent 同步',
  'xray.apply': '应用 Xray 配置',
}
function actionText(action: string) {
  return actionLabels[action] || action
}
function formatTime(v: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString()
}
function safeDetail(v: string) {
  if (!v) return '-'
  if (v.length > 80) return v.slice(0, 32) + '...' + v.slice(-12)
  return v
}

async function load() {
  error.value = ''
  loading.value = true
  try {
    logs.value = await api(`/api/logs?limit=${limit.value}&t=${Date.now()}`)
  } catch(e:any) {
    error.value = e?.message || '日志加载失败'
  } finally {
    loading.value = false
  }
}

async function copyLogs() {
  const text = logs.value.map((l:any)=>`${formatTime(l.created_at)}\t${l.actor}\t${actionText(l.action)}\t${l.ip}\t${safeDetail(l.detail)}`).join('\n')
  const ok = await copyText(text)
  message.value = ok ? '当前日志已复制。' : '浏览器禁止自动复制，请手动选择表格内容。'
}

onMounted(load)
</script>
<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">系统日志</h1>
      <p class="page-desc">默认显示最近 200 条操作日志，已对常见动作做中文化展示，便于排查新增、删除、编辑、登录和配置同步等后台动作。</p>
    </div>
    <div class="head-actions">
      <select v-model.number="limit" @change="load"><option :value="100">100条</option><option :value="200">200条</option><option :value="500">500条</option><option :value="1000">1000条</option></select>
      <button class="btn secondary" @click="copyLogs">复制日志</button>
      <button class="btn secondary" @click="load" :disabled="loading">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>
  </div>
  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>
  <table>
    <thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>IP</th><th>详情</th></tr></thead>
    <tbody>
      <tr v-for="l in logs" :key="l.id"><td>{{ formatTime(l.created_at) }}</td><td>{{ l.actor }}</td><td>{{ actionText(l.action) }}</td><td>{{ l.ip }}</td><td>{{ safeDetail(l.detail) }}</td></tr>
      <tr v-if="logs.length===0"><td colspan="5" class="muted">暂无日志</td></tr>
    </tbody>
  </table>
</template>
