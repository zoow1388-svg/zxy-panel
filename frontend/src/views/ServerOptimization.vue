<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'

const servers = ref<any[]>([])
const loading = ref(false)
const message = ref('')
const error = ref('')
let pollTimer: number | undefined

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await api('/api/system/optimization/bbr/status')
    servers.value = result?.servers || []
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}

function stateLabel(server: any) {
  const bbr = server?.bbr || {}
  if (server?.pending_action) return '等待 Agent 执行'
  if (bbr.error) return '状态未知'
  if (bbr.enabled) return '已开启'
  if (bbr.supported) return '未开启'
  return '暂不支持'
}

function stateClass(server: any) {
  const bbr = server?.bbr || {}
  if (server?.pending_action || bbr.error || !bbr.supported) return 'warn'
  return bbr.enabled ? 'ok' : 'muted'
}

function checkedAt(value: string) {
  if (!value) return '等待 Agent 上报'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

async function queue(server: any, endpoint: string, confirmText = '') {
  if (confirmText && !confirm(confirmText)) return
  message.value = ''
  error.value = ''
  try {
    await api(endpoint, { method: 'POST', body: JSON.stringify({ server_id: server.id }) })
    message.value = `${server.name || server.host || server.id}：任务已下发，Agent 最多约 30 秒会回传结果。`
    await load()
  } catch (e: any) {
    error.value = e?.message || String(e)
  }
}

onMounted(async () => {
  await load()
  pollTimer = window.setInterval(load, 30000)
})

onUnmounted(() => {
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">服务器优化</h1>
      <p class="page-desc">BBR 是宿主机 TCP 优化项。开启后影响整台服务器的 TCP 服务，不只影响面板或网络核心。</p>
    </div>
    <button class="btn secondary" :disabled="loading" @click="load">刷新状态</button>
  </div>

  <div class="notice warn">
    关闭只移除 ZXY 创建的 BBR 配置文件。部分系统关闭后需要重启服务器，才能完全恢复原来的拥塞控制算法。
  </div>

  <div v-if="message" class="success">{{ message }}</div>
  <div v-if="error" class="error">{{ error }}</div>

  <div v-if="!loading && servers.length === 0" class="empty-state">暂未发现可管理的服务器。</div>

  <section v-for="server in servers" :key="server.id" class="card bbr-server-card">
    <div class="row-between bbr-card-head">
      <div>
        <h2>{{ server.name || '未命名服务器' }}</h2>
        <p class="muted">{{ server.host || server.id }} · Agent：{{ server.agent_version || '未上报' }}</p>
      </div>
      <span class="badge bbr-state" :class="stateClass(server)">{{ stateLabel(server) }}</span>
    </div>

    <div class="bbr-status-grid">
      <div><span>内核版本</span><strong>{{ server.bbr?.kernel || '-' }}</strong></div>
      <div><span>支持 BBR</span><strong>{{ server.bbr?.supported ? '支持' : '未检测到' }}</strong></div>
      <div><span>当前算法</span><strong>{{ server.bbr?.congestion_control || '-' }}</strong></div>
      <div><span>队列算法</span><strong>{{ server.bbr?.default_qdisc || '-' }}</strong></div>
      <div><span>BBR 模块</span><strong>{{ server.bbr?.module_loaded ? '已加载' : '未加载或不可见' }}</strong></div>
      <div><span>最后检测</span><strong>{{ checkedAt(server.bbr?.checked_at) }}</strong></div>
    </div>

    <div v-if="server.pending_action" class="notice warn bbr-pending">
      正在等待 Agent 执行：{{ server.pending_action.action }}
    </div>
    <div v-else-if="server.bbr?.message || server.bbr?.error" class="notice" :class="server.bbr?.error ? 'warn' : 'ok'">
      {{ server.bbr?.error || server.bbr?.message }}
    </div>

    <div class="actions bbr-actions">
      <button class="btn secondary" :disabled="!!server.pending_action" @click="queue(server, '/api/system/optimization/bbr/status')">重新检测</button>
      <button class="btn" :disabled="!!server.pending_action || !server.bbr?.supported" @click="queue(server, '/api/system/optimization/bbr/enable')">开启 BBR</button>
      <button class="btn danger" :disabled="!!server.pending_action" @click="queue(server, '/api/system/optimization/bbr/disable', '确认关闭此服务器的 ZXY BBR 优化？')">关闭 BBR</button>
    </div>
  </section>
</template>
