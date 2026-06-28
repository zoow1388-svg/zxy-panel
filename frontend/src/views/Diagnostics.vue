<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const data = ref<any>(null)
const error = ref('')
const loading = ref(false)
const message = ref('')
const lastChecked = ref('')
const report = ref('')
const copying = ref(false)

function nowText() {
  const d = new Date()
  const pad = (n:number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

async function load() {
  error.value = ''
  message.value = ''
  loading.value = true
  try {
    data.value = await api(`/api/system/checks?t=${Date.now()}`)
    lastChecked.value = nowText()
    message.value = `检测完成：${lastChecked.value}`
  } catch(e:any) {
    error.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function makeReport() {
  copying.value = true
  error.value = ''
  message.value = ''
  try {
    const text = await api(`/api/system/report?t=${Date.now()}`)
    report.value = text
    const ok = await copyText(text)
    message.value = ok ? '诊断报告已复制。' : '浏览器禁止自动复制，已在下方显示诊断报告，可手动复制。'
  } catch(e:any) {
    error.value = e?.message || '生成诊断报告失败'
  } finally {
    copying.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">系统检测</h1>
      <p class="page-desc">V0.7.5.1 UI 弹窗优化版：检测多服务器、Agent 版本、中转线路、协议匹配、端口冲突、DNS 与出站策略。</p>
    </div>
    <div class="head-actions">
      <button class="btn secondary" @click="makeReport" :disabled="copying">{{ copying ? '生成中...' : '复制诊断报告' }}</button>
      <button class="btn secondary" @click="load" :disabled="loading">{{ loading ? '检测中...' : '重新检测' }}</button>
    </div>
  </div>

  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>

  <div v-if="data" class="cards diag-cards">
    <div class="card"><div class="label">版本</div><div class="value small">{{ data.version }}</div></div>
    <div class="card"><div class="label">安装目录</div><div class="value small code">{{ data.install_dir }}</div></div>
    <div class="card"><div class="label">服务器 / 在线</div><div class="value">{{ data.counts.servers }} / {{ data.counts.online_servers }}</div></div>
    <div class="card"><div class="label">节点 / 客户</div><div class="value">{{ data.counts.nodes }} / {{ data.counts.clients }}</div></div>
    <div class="card"><div class="label">中转线路</div><div class="value">{{ data.counts.relays || 0 }}</div></div>
  </div>

  <div v-if="data" class="check-list">
    <div v-for="c in data.checks" :key="c.key" class="card check-item" :class="c.status">
      <div class="check-left">
        <strong>{{ c.label }}</strong>
        <p>{{ c.message }}</p>
      </div>
      <span class="badge" :class="c.status === 'ok' ? 'online' : ''">{{ c.status === 'ok' ? '正常' : (c.status === 'fail' ? '失败' : '注意') }}</span>
    </div>
  </div>

  <div v-if="report" class="card config-preview">
    <div class="row-between"><strong>诊断报告</strong><button class="btn secondary" @click="copyText(report)">再次复制</button></div>
    <pre class="code pre-wrap">{{ report }}</pre>
  </div>

  <div class="notice warn">
    说明：IP 显示为塞舌尔、香港、美国或其他地区，通常是 IP 数据库/ASN 归属问题；如果出口 IP 是你的服务器 IP，DNS 不显示中国，就不是本地泄漏。
  </div>
</template>
