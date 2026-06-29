<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

type CheckItem = { key:string; label:string; status:string; message:string; detail?:string }
type PortItem = { name:string; kind:string; host:string; port:number; server:string; status:string; message:string }

const data = ref<any>(null)
const error = ref('')
const loading = ref(false)
const message = ref('')
const report = ref('')
const copying = ref(false)

function statusText(status:string) {
  if (status === 'ok') return '正常'
  if (status === 'fail') return '风险'
  return '注意'
}

function statusClass(status:string) {
  if (status === 'ok') return 'online'
  if (status === 'fail') return 'fail'
  return 'warn'
}

async function runDiagnosis() {
  error.value = ''
  message.value = ''
  loading.value = true
  data.value = null
  report.value = ''
  try {
    data.value = await api(`/api/diagnostics/run?t=${Date.now()}`)
    message.value = `一键体检完成，评分 ${data.value.score} 分，检测时间 ${data.value.checked_at}。`
  } catch(e:any) {
    error.value = e?.message || '节点体检失败'
  } finally {
    loading.value = false
  }
}

async function copyReport() {
  copying.value = true
  error.value = ''
  message.value = ''
  try {
    const text = await api(`/api/diagnostics/report?t=${Date.now()}`)
    report.value = text
    const ok = await copyText(text)
    message.value = ok ? '节点诊断报告已复制。' : '浏览器禁止自动复制，已在下方显示，可手动复制。'
  } catch(e:any) {
    error.value = e?.message || '生成诊断报告失败'
  } finally {
    copying.value = false
  }
}

function itemsOf(key:string): CheckItem[] {
  return data.value?.[key] || []
}

onMounted(runDiagnosis)
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">节点诊断与一键体检中心</h1>
      <p class="page-desc">V0.7.5.8.1：区分宿主机 DNS、Xray DNS 与客户端 DNS，DNS/IPv6 注意项不再直接等同节点泄漏。</p>
    </div>
    <div class="head-actions">
      <button class="btn secondary" @click="copyReport" :disabled="copying || loading">{{ copying ? '生成中...' : '复制诊断报告' }}</button>
      <button class="btn" @click="runDiagnosis" :disabled="loading">{{ loading ? '体检中...' : '一键重新体检' }}</button>
    </div>
  </div>

  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>

  <div v-if="loading" class="notice">正在重新读取系统状态、网络策略、Nginx/Xray 配置和节点端口，请稍等...</div>

  <div v-if="data" class="diagnosis-hero card">
    <div>
      <div class="label">体检结论</div>
      <h2>{{ data.summary }}</h2>
      <p class="muted">检测时间：{{ data.checked_at }} ｜ 安装模式：{{ data.install_mode || '-' }} ｜ 面板端口：{{ data.panel_port || '-' }}</p>
    </div>
    <div class="score-ring" :class="data.score >= 85 ? 'ok' : (data.score >= 65 ? 'warn' : 'fail')">
      <strong>{{ data.score }}</strong>
      <span>健康分</span>
    </div>
  </div>

  <div v-if="data" class="cards diag-cards">
    <div class="card"><div class="label">当前版本</div><div class="value small code">{{ data.version }}</div></div>
    <div class="card"><div class="label">服务器 / 节点</div><div class="value">{{ data.counts.servers }} / {{ data.counts.nodes }}</div></div>
    <div class="card"><div class="label">客户 / 中转</div><div class="value">{{ data.counts.clients }} / {{ data.counts.relays }}</div></div>
    <div class="card"><div class="label">检测端口</div><div class="value">{{ data.counts.ports }}</div></div>
  </div>

  <div v-if="data" class="diagnosis-grid">
    <div class="card diagnosis-section">
      <div class="section-head"><div><h2>一、运行状态</h2><p>API、Agent、Xray、Nginx 与本机端口。</p></div></div>
      <div class="check-list compact-list">
        <div v-for="c in itemsOf('runtime_checks')" :key="c.key" class="check-item" :class="c.status">
          <div class="check-left"><strong>{{ c.label }}</strong><p>{{ c.message }}</p></div>
          <span class="badge" :class="statusClass(c.status)">{{ statusText(c.status) }}</span>
        </div>
      </div>
    </div>

    <div class="card diagnosis-section">
      <div class="section-head"><div><h2>二、网络与泄漏风险</h2><p>DNS、IPv6、出口 IP、网络策略强度。</p></div></div>
      <div class="check-list compact-list">
        <div v-for="c in itemsOf('network_checks')" :key="c.key" class="check-item" :class="c.status">
          <div class="check-left"><strong>{{ c.label }}</strong><p>{{ c.message }}</p></div>
          <span class="badge" :class="statusClass(c.status)">{{ statusText(c.status) }}</span>
        </div>
      </div>
    </div>
  </div>

  <div v-if="data" class="card diagnosis-section">
    <div class="section-head"><div><h2>三、配置一致性</h2><p>Nginx 反代、随机后台路径、Xray 配置和节点端口总览。</p></div></div>
    <div class="check-list compact-list">
      <div v-for="c in itemsOf('config_checks')" :key="c.key" class="check-item" :class="c.status">
        <div class="check-left"><strong>{{ c.label }}</strong><p>{{ c.message }}</p></div>
        <span class="badge" :class="statusClass(c.status)">{{ statusText(c.status) }}</span>
      </div>
    </div>
  </div>

  <div v-if="data" class="card diagnosis-section">
    <div class="section-head"><div><h2>四、节点端口检测</h2><p>本机端口会直接检测监听；远程服务器端口会根据 Agent 状态给出人工确认提示。</p></div></div>
    <div class="table-wrap" v-if="data.port_checks?.length">
      <table>
        <thead><tr><th>类型</th><th>名称</th><th>服务器</th><th>地址</th><th>状态</th><th>说明</th></tr></thead>
        <tbody>
          <tr v-for="p in data.port_checks" :key="`${p.kind}-${p.name}-${p.port}`">
            <td>{{ p.kind }}</td>
            <td><strong>{{ p.name }}</strong></td>
            <td>{{ p.server }}</td>
            <td class="code">{{ p.host }}:{{ p.port }}</td>
            <td><span class="badge" :class="statusClass(p.status)">{{ statusText(p.status) }}</span></td>
            <td class="small">{{ p.message }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="notice warn">还没有启用的入站或中转端口。创建节点后再运行体检，可以看到端口监听结果。</div>
  </div>

  <div v-if="data" class="card diagnosis-section">
    <div class="section-head"><div><h2>五、处理建议</h2><p>按优先级处理，先修红色失败项，再处理黄色注意项。</p></div></div>
    <ul class="diagnosis-tips">
      <li v-for="tip in data.recommendations" :key="tip">{{ tip }}</li>
    </ul>
  </div>

  <div v-if="report" class="card config-preview">
    <div class="row-between"><strong>诊断报告</strong><button class="btn secondary" @click="copyText(report)">再次复制</button></div>
    <pre class="code pre-wrap">{{ report }}</pre>
  </div>

  <div class="notice warn">
    排障顺序建议：先看面板端口和 /api/ 反代，再看 Agent/Xray，再看节点端口安全组，最后再判断 DNS、IPv6、QUIC 和出口 IP 归属。
  </div>
</template>
