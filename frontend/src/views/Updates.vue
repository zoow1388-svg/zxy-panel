<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const status = ref<any>(null)
const check = ref<any>(null)
const command = ref<any>(null)
const xray = ref<any>(null)
const loading = ref(false)
const message = ref('')
const error = ref('')

async function loadStatus() {
  error.value = ''
  try {
    status.value = await api('/api/updates/status')
    xray.value = await api('/api/updates/xray-status')
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

onMounted(loadStatus)
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">系统升级</h1>
      <p class="page-desc">V0.7.5.1 升级配置修复版：先确认升级源是否已配置，再检查版本或生成升级命令，避免使用未发布的仓库地址。</p>
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
    <div class="card">
      <div class="label">当前面板版本</div>
      <div class="value small">{{ status.current_version }}</div>
    </div>
    <div class="card">
      <div class="label">远程版本清单</div>
      <div class="value small code">{{ status.manifest_display || '未配置' }}</div>
    </div>
    <div class="card">
      <div class="label">安装目录</div>
      <div class="value small code">{{ status.install_dir }}</div>
    </div>
    <div class="card">
      <div class="label">备份目录</div>
      <div class="value small code">{{ status.backup_dir }}</div>
    </div>
  </div>

  <div class="card update-card">
    <div class="row-between">
      <div>
        <h2>面板程序升级</h2>
        <p class="muted">远程版本清单配置后，才可以检查新版本并生成可审查升级命令。未配置前请继续使用上传 ZIP 包升级。</p>
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
      <div class="row-between">
        <strong>升级命令</strong>
        <button class="btn secondary" @click="copyCommand">复制命令</button>
      </div>
      <pre class="code pre-wrap">{{ command.command }}</pre>
    </div>
  </div>

  <div class="card update-card">
    <div class="row-between">
      <div>
        <h2>网络核心升级</h2>
        <p class="muted">网络核心即 Xray Core。当前版本优先显示 Agent 上报的核心版本，后续版本加入下载、校验、配置测试和一键替换。</p>
      </div>
    </div>
    <div class="notice">
      <strong>当前核心：</strong>{{ xray?.current_xray || status?.xray_version || '未检测到' }}
    </div>
    <ul class="update-list" v-if="xray?.planned_checks">
      <li v-for="item in xray.planned_checks" :key="item">{{ item }}</li>
    </ul>
  </div>

  <div class="notice warn">
    提醒：未配置远程版本清单时，“检查更新”和“生成升级命令”不会真正生效。请先把代码仓库、version.json 和升级包发布好，再设置 ZXY_UPDATE_MANIFEST_URL。
  </div>
</template>
