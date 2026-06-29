<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const status = ref<any>(null)
const check = ref<any>(null)
const command = ref<any>(null)
const xray = ref<any>(null)
const task = ref<any>(null)
const taskLogs = ref('')
const precheck = ref<any>(null)
const loading = ref(false)
const taskLoading = ref(false)
const message = ref('')
const error = ref('')
let timer: number | undefined

function isRunning(t: any) {
  return ['pending', 'running', 'queued', 'downloading', 'verifying', 'backing_up', 'installing', 'restarting'].includes(t?.status) && !t?.stale
}

async function loadStatus() {
  error.value = ''
  try {
    status.value = await api('/api/updates/status')
    xray.value = await api('/api/updates/xray-status')
    await loadTask()
  } catch (e: any) {
    error.value = e?.message || '加载升级状态失败'
  }
}

async function checkUpdate() {
  loading.value = true
  error.value = ''
  message.value = ''
  command.value = null
  try {
    check.value = await api('/api/updates/check', { method: 'POST', body: '{}' })
    if (check.value?.ok === false) {
      error.value = check.value?.message || check.value?.error || '检查更新失败'
    } else {
      message.value = check.value?.message || '检查完成'
    }
  } catch (e: any) {
    error.value = e?.message || '检查更新失败'
  } finally {
    loading.value = false
  }
}

async function makeCommand() {
  loading.value = true
  error.value = ''
  message.value = ''
  try {
    command.value = await api('/api/updates/panel-command', { method: 'POST', body: '{}' })
    if (command.value?.ok === false) {
      error.value = command.value?.message || command.value?.error || '生成升级命令失败'
    } else {
      message.value = command.value?.message || '升级命令已生成'
    }
  } catch (e: any) {
    error.value = e?.message || '生成升级命令失败'
  } finally {
    loading.value = false
  }
}

async function copyCommand() {
  if (!command.value?.command) return
  const ok = await copyText(command.value.command)
  message.value = ok ? '升级命令已复制。' : '浏览器禁止自动复制，请手动复制下方命令。'
}

async function runPrecheck() {
  taskLoading.value = true
  error.value = ''
  message.value = ''
  try {
    precheck.value = await api('/api/updates/tasks/precheck')
    message.value = precheck.value?.message || '升级前检查完成'
  } catch (e: any) {
    error.value = e?.message || '升级前检查失败'
  } finally {
    taskLoading.value = false
  }
}

async function startManagedUpgrade() {
  if (!confirm('确认开始托管升级吗？升级过程中面板可能会短暂断开 30-120 秒。系统会自动备份当前配置和数据。')) return
  taskLoading.value = true
  error.value = ''
  message.value = ''
  try {
    const res = await api('/api/updates/tasks', { method: 'POST', body: '{}' })
    if (res?.ok === false) {
      error.value = res?.message || '托管升级启动失败'
    } else {
      task.value = res.task
      message.value = res?.message || '托管升级已启动'
      startPolling()
    }
  } catch (e: any) {
    error.value = e?.message || '托管升级启动失败'
  } finally {
    taskLoading.value = false
  }
}

async function loadTask() {
  try {
    const res = await api(`/api/updates/tasks/latest?t=${Date.now()}`)
    task.value = res?.has_task ? res.task : null
    if (task.value) await loadLogs()
  } catch {
    // ignore polling errors during service restart
  }
}

async function loadLogs() {
  try {
    const res = await api(`/api/updates/tasks/logs?t=${Date.now()}`)
    taskLogs.value = res?.logs || ''
  } catch {
    // ignore polling errors during service restart
  }
}

async function clearStaleTask() {
  if (!confirm('确认清理卡死升级任务吗？该操作只会把任务标记为 failed，日志和备份都会保留。')) return
  taskLoading.value = true
  error.value = ''
  message.value = ''
  try {
    const res = await api('/api/updates/tasks/clear-stale', { method: 'POST', body: '{}' })
    if (res?.ok === false) {
      error.value = res?.message || '清理卡死任务失败'
    } else {
      task.value = res.task || task.value
      message.value = res?.message || '卡死任务已清理'
      await loadLogs()
    }
  } catch (e:any) {
    error.value = e?.message || '清理卡死任务失败'
  } finally {
    taskLoading.value = false
  }
}

function startPolling() {
  if (timer) window.clearInterval(timer)
  timer = window.setInterval(async () => {
    await loadTask()
    if (task.value && !isRunning(task.value)) {
      if (timer) window.clearInterval(timer)
      timer = undefined
    }
  }, 3000)
}

onMounted(async () => {
  await loadStatus()
  if (task.value && isRunning(task.value)) startPolling()
})
onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">系统升级</h1>
      <p class="page-desc">V0.7.5.9.1 托管升级中心：改用独立 systemd runner，API 重启不会中断升级，并支持识别/清理卡死任务。</p>
    </div>
    <div class="head-actions">
      <button class="btn secondary" @click="loadStatus">刷新状态</button>
      <button class="btn" :disabled="loading" @click="checkUpdate">{{ loading ? '检查中...' : '检查更新' }}</button>
    </div>
  </div>

  <div v-if="error" class="error">{{ error }}</div>
  <div v-if="message" class="success">{{ message }}</div>
  <div v-if="status?.note" class="notice" :class="status.manifest_configured ? 'success' : 'warn'">{{ status.note }}</div>

  <div class="cards diag-cards" v-if="status">
    <div class="card"><div class="label">当前面板版本</div><div class="value small">{{ status.current_version }}</div></div>
    <div class="card"><div class="label">远程版本清单</div><div class="value small code">{{ status.manifest_display || '未配置' }}</div></div>
    <div class="card"><div class="label">安装目录</div><div class="value small code">{{ status.install_dir }}</div></div>
    <div class="card"><div class="label">备份目录</div><div class="value small code">{{ status.backup_dir }}</div></div>
  </div>

  <div class="card update-card">
    <div class="row-between">
      <div>
        <h2>一、托管升级任务</h2>
        <p class="muted">后台托管升级会下载官方 version.json 指定的升级包、校验 SHA256、备份当前配置，然后在宿主机执行 deploy/install.sh。</p>
      </div>
      <div class="head-actions">
        <button class="btn secondary" :disabled="taskLoading" @click="runPrecheck">升级前检查</button>
        <button class="btn" :disabled="taskLoading || isRunning(task)" @click="startManagedUpgrade">{{ isRunning(task) ? '升级执行中...' : '立即托管升级' }}</button>
      </div>
    </div>

    <div v-if="precheck" class="notice" :class="precheck.ok ? 'success' : 'warn'">
      <strong>{{ precheck.message }}</strong>
      <ul class="update-list">
        <li v-for="item in precheck.checks" :key="item.name">
          <span>{{ item.ok ? '✅' : '⚠️' }}</span> {{ item.name }}：{{ item.message }}
        </li>
      </ul>
    </div>

    <div v-if="task" class="config-preview">
      <div class="row-between">
        <strong>最新升级任务</strong>
        <div class="head-actions">
          <button v-if="task.stale" class="btn secondary" :disabled="taskLoading" @click="clearStaleTask">清理卡死任务</button>
          <button class="btn secondary" @click="loadTask">刷新任务</button>
        </div>
      </div>
      <div class="cards diag-cards">
        <div class="card"><div class="label">状态</div><div class="value small">{{ task.status }}</div></div>
        <div class="card"><div class="label">阶段</div><div class="value small">{{ task.stage }}</div></div>
        <div class="card"><div class="label">目标版本</div><div class="value small">{{ task.target_version }}</div></div>
        <div class="card"><div class="label">升级包</div><div class="value small code">{{ task.package }}</div></div>
      </div>
      <div v-if="task.stale" class="notice warn">
        任务可能已中断：{{ task.stale_reason || '超过 10 分钟没有更新状态' }}。可以先查看日志，再点击“清理卡死任务”。
      </div>
      <div class="notice" :class="task.status === 'success' ? 'success' : (task.status === 'failed' || task.stale ? 'warn' : '')">
        {{ task.message || task.error || '任务状态已更新' }}
      </div>
      <pre class="code pre-wrap" v-if="taskLogs">{{ taskLogs }}</pre>
    </div>
  </div>

  <div class="card update-card">
    <div class="row-between">
      <div>
        <h2>二、面板程序升级命令</h2>
        <p class="muted">如果托管升级不可用，仍可生成命令后通过 SSH 执行。这是保底方案。</p>
      </div>
      <div class="head-actions">
        <button class="btn secondary" :disabled="loading" @click="checkUpdate">检查更新</button>
        <button class="btn" :disabled="loading" @click="makeCommand">生成升级命令</button>
      </div>
    </div>

    <div v-if="check" class="notice" :class="!check.ok ? 'warn' : (check.update_available ? 'warn' : 'success')">
      <strong>{{ check.ok ? '检查结果' : '检查失败' }}：</strong>
      {{ check.message || check.error }}
      <div v-if="check.latest_version" class="muted">最新版本：{{ check.latest_version }}</div>
      <ul v-if="check.manifest?.changelog?.length" class="update-list">
        <li v-for="item in check.manifest.changelog" :key="item">{{ item }}</li>
      </ul>
    </div>

    <div v-if="command?.command" class="config-preview">
      <div class="row-between"><strong>升级命令</strong><button class="btn secondary" @click="copyCommand">复制命令</button></div>
      <pre class="code pre-wrap">{{ command.command }}</pre>
    </div>
  </div>

  <div class="card update-card">
    <div class="row-between">
      <div>
        <h2>三、网络核心升级</h2>
        <p class="muted">网络核心即 Xray Core。当前版本优先显示 Agent 上报的核心版本。</p>
      </div>
    </div>
    <div class="notice"><strong>当前核心：</strong>{{ xray?.current_xray || status?.xray_version || '未检测到' }}</div>
    <ul class="update-list" v-if="xray?.planned_checks"><li v-for="item in xray.planned_checks" :key="item">{{ item }}</li></ul>
  </div>

  <div class="notice warn">
    安全规则：托管升级只允许使用官方 version.json 中的 download_url 和 sha256，不允许在后台输入任意 Shell 命令。升级失败时请查看日志或使用复制命令兜底。
  </div>
</template>
