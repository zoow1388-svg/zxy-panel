<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '../api'

const data = ref<any>({})
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  try {
    data.value = await api('/api/dashboard')
  } catch(e:any) {
    error.value = e?.message || '仪表盘加载失败'
  } finally {
    loading.value = false
  }
})

function gb(v:number) {
  const n = Number(v || 0) / 1024 / 1024 / 1024
  if (n >= 100) return Math.round(n).toString()
  return n.toFixed(1)
}
function pct(v:number) { return typeof v === 'number' && v > 0 ? `${v.toFixed(1)}%` : '-' }
function bar(v:number) { return Math.max(0, Math.min(100, Number(v || 0))) }
function formatTime(v:string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString()
}
function actionText(v:string) {
  const map:Record<string,string> = {
    'auth.login': '登录后台',
    'auth.login_failed': '登录失败',
    'node.create': '创建节点',
    'node.update': '修改节点',
    'node.delete': '删除节点',
    'client.create': '添加客户',
    'client.update': '修改客户',
    'client.delete': '删除客户',
    'server.sync': 'Agent 同步',
    'xray.apply': '应用配置',
    'relay.create': '创建中转',
    'relay.delete': '删除中转',
    'server.create': '添加服务器',
    'server.update': '修改服务器',
    'server.delete': '删除服务器',
  }
  return map[v] || v || '-'
}
function statusMessage(v:string) {
  const raw = String(v || '').trim()
  const map:Record<string,string> = {
    'config already up to date': '配置已是最新',
    'config updated': '配置已更新',
    'xray config applied': 'Xray 配置已下发',
    'agent started': 'Agent 已启动',
  }
  return map[raw] || raw || '正在等待运行数据'
}
const healthLabel = computed(() => {
  const s = data.value.primary_server
  if (!s) return '等待服务器接入'
  if (s.status === 'online') return '运行正常'
  return '服务器离线'
})
const healthClass = computed(() => data.value.primary_server?.status === 'online' ? 'ok' : 'warn')
</script>
<template>
  <div class="dashboard-shell">
    <div class="dashboard-hero">
      <div>
        <div class="eyebrow">ZXY Panel V0.7.5.8</div>
        <h2>跨境业务网络节点管理控制台</h2>
        <p>这一版开始进入 UI 产品化阶段：仪表盘、资源状态、快捷操作、最近入站、最近客户和操作记录集中展示。核心 Reality、Agent、Xray、订阅和客户端适配链路保持不动。</p>
        <div class="hero-actions">
          <RouterLink to="/nodes" class="btn">新建入站</RouterLink>
          <RouterLink to="/clients" class="btn secondary">添加客户</RouterLink>
          <RouterLink to="/diagnostics" class="btn secondary">系统检测</RouterLink>
        </div>
      </div>
      <div class="health-card" :class="healthClass">
        <span>系统状态</span>
        <strong>{{ healthLabel }}</strong>
        <em>{{ statusMessage(data.primary_server?.last_message) }}</em>
      </div>
    </div>

    <div class="error" v-if="error">{{ error }}</div>
    <div class="notice" v-if="loading">正在加载仪表盘数据...</div>

    <div class="metric-grid">
      <div class="metric-card accent-blue"><span>面板版本</span><strong class="code">{{ data.version || '-' }}</strong><small>当前运行版本</small></div>
      <div class="metric-card"><span>服务器</span><strong>{{ data.online_servers || 0 }} / {{ data.servers || 0 }}</strong><small>在线 / 总数</small></div>
      <div class="metric-card"><span>启用入站</span><strong>{{ data.enabled_nodes || 0 }}</strong><small>共 {{ data.nodes || 0 }} 个入站</small></div>
      <div class="metric-card"><span>启用客户</span><strong>{{ data.enabled_clients || 0 }}</strong><small>共 {{ data.clients || 0 }} 个客户</small></div>
      <div class="metric-card"><span>总上传</span><strong>{{ gb(data.upload_total) }} GB</strong><small>Agent 上报统计</small></div>
      <div class="metric-card"><span>总下载</span><strong>{{ gb(data.download_total) }} GB</strong><small>Agent 上报统计</small></div>
    </div>

    <div class="dashboard-grid">
      <div class="panel ops-panel" v-if="data.primary_server">
        <div class="section-head"><div><h2>本机服务器</h2><p>Agent、Xray 和资源状态</p></div><span class="badge online" v-if="data.primary_server.status==='online'">在线</span><span class="badge warn" v-else>离线</span></div>
        <div class="server-line"><span>公网入口</span><strong class="code">{{ data.primary_server.host || data.primary_server.ip }}</strong></div>
        <div class="server-line"><span>Agent</span><strong class="code">{{ data.primary_server.agent_version || '-' }}</strong></div>
        <div class="server-line"><span>Xray</span><strong class="code small">{{ data.primary_server.xray_version || '-' }}</strong></div>
        <div class="resource-stack">
          <div class="resource-row"><div><span>CPU</span><strong>{{ pct(data.primary_server.cpu_usage) }}</strong></div><i><b :style="{width: bar(data.primary_server.cpu_usage)+'%'}"></b></i></div>
          <div class="resource-row"><div><span>内存</span><strong>{{ pct(data.primary_server.memory_usage) }}</strong></div><i><b :style="{width: bar(data.primary_server.memory_usage)+'%'}"></b></i></div>
          <div class="resource-row"><div><span>磁盘</span><strong>{{ pct(data.primary_server.disk_usage) }}</strong></div><i><b :style="{width: bar(data.primary_server.disk_usage)+'%'}"></b></i></div>
        </div>
        <div class="muted">最后同步：{{ formatTime(data.primary_server.last_sync_at) }}</div>
      </div>

      <div class="panel ops-panel">
        <div class="section-head"><div><h2>快捷入口</h2><p>常用操作一步到位</p></div></div>
        <div class="quick-grid">
          <RouterLink to="/nodes" class="quick-action"><span>↧</span><strong>入站管理</strong><em>Reality / 端口 / 分享</em></RouterLink>
          <RouterLink to="/clients" class="quick-action"><span>◉</span><strong>客户管理</strong><em>订阅 / 二维码</em></RouterLink>
          <RouterLink to="/diagnostics" class="quick-action"><span>✓</span><strong>系统检测</strong><em>端口 / Agent / Xray</em></RouterLink>
          <RouterLink to="/logs" class="quick-action"><span>≡</span><strong>系统日志</strong><em>操作审计</em></RouterLink>
        </div>
      </div>
    </div>

    <div class="dashboard-grid lower">
      <div class="panel ops-panel">
        <div class="section-head"><div><h2>最近入站</h2><p>快速确认端口、协议和启用状态</p></div><RouterLink to="/nodes" class="text-link">查看全部</RouterLink></div>
        <div class="mini-list">
          <div class="mini-item" v-for="n in data.recent_nodes || []" :key="n.id">
            <div><strong>{{ n.name }}</strong><span class="code">{{ n.host }}:{{ n.port }}</span></div>
            <em>{{ n.protocol }}/{{ n.transport }}/{{ n.security }}</em>
            <span class="badge" :class="n.enabled ? 'online' : 'warn'">{{ n.enabled ? '启用' : '停用' }}</span>
          </div>
          <div class="muted" v-if="!(data.recent_nodes || []).length">暂无入站，请先创建 Reality 入站。</div>
        </div>
      </div>

      <div class="panel ops-panel">
        <div class="section-head"><div><h2>最近客户</h2><p>客户状态与流量额度</p></div><RouterLink to="/clients" class="text-link">查看全部</RouterLink></div>
        <div class="mini-list">
          <div class="mini-item" v-for="c in data.recent_clients || []" :key="c.id">
            <div><strong>{{ c.username }}</strong><span>{{ c.email || '未填写邮箱' }}</span></div>
            <em>{{ c.traffic_used_gb || 0 }} / {{ c.traffic_limit_gb || 0 }} GB</em>
            <span class="badge" :class="c.enabled ? 'online' : 'warn'">{{ c.enabled ? '启用' : '停用' }}</span>
          </div>
          <div class="muted" v-if="!(data.recent_clients || []).length">暂无客户，请先添加客户并生成订阅。</div>
        </div>
      </div>

      <div class="panel ops-panel timeline-panel">
        <div class="section-head"><div><h2>最近操作</h2><p>后台操作审计</p></div><RouterLink to="/logs" class="text-link">查看日志</RouterLink></div>
        <div class="timeline">
          <div class="timeline-item" v-for="l in data.recent_logs || []" :key="l.id">
            <span></span>
            <div><strong>{{ actionText(l.action) }}</strong><em>{{ formatTime(l.created_at) }} · {{ l.actor }} · {{ l.ip }}</em><p>{{ String(l.detail || '').replace(/srv_[0-9A-Za-z_]+/g, '服务器').replace(/node_[0-9A-Za-z_]+/g, '入站') }}</p></div>
          </div>
          <div class="muted" v-if="!(data.recent_logs || []).length">暂无操作日志。</div>
        </div>
      </div>
    </div>
  </div>
</template>
