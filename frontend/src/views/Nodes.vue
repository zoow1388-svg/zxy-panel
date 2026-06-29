<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'
import { buildClientShare, clientsForNode, isQrShareFormat, qrImageUrl, shareFormatLabel, shareFormatOptions, type ShareFormat } from '../share'

const nodes = ref<any[]>([])
const servers = ref<any[]>([])
const clients = ref<any[]>([])
const config = ref('')
const error = ref('')
const message = ref('')
const saving = ref(false)
const editingId = ref('')
const mode = ref<'test'|'recommended'|'socks5'|'advanced'>('recommended')
const shareNodeData = ref<any | null>(null)
const shareClientId = ref('')
const shareLink = ref('')
const shareFormat = ref<ShareFormat>('v2rayn')
const showEditor = ref(false)
const qrZoomText = ref('')
const qrZoomTitle = ref('')
const socksNodeData = ref<any | null>(null)
const socksPasswordVisible = ref(false)

const realityPresets = [
  { label: 'Intel', dest: 'www.intel.com:443', sni: 'www.intel.com' },
  { label: 'Microsoft', dest: 'www.microsoft.com:443', sni: 'www.microsoft.com' },
  { label: 'Apple', dest: 'www.apple.com:443', sni: 'www.apple.com' },
  { label: 'Cloudflare', dest: 'www.cloudflare.com:443', sni: 'www.cloudflare.com' },
]

function randomRecommendedPort() {
  return Math.floor(10000 + Math.random() * 50000)
}
function randomPassword(len = 20) {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const arr = new Uint32Array(len)
  crypto.getRandomValues(arr)
  return Array.from(arr).map(n => chars[n % chars.length]).join('')
}
const emptyForm = () => ({
  server_id: '',
  name: '',
  protocol: 'vless',
  host: '',
  port: randomRecommendedPort(),
  transport: 'tcp',
  security: 'reality',
  sni: 'www.intel.com',
  path: '',
  fingerprint: 'chrome',
  reality_dest: 'www.intel.com:443',
  reality_private_key: '',
  reality_public_key: '',
  reality_short_id: '',
  reality_spider_x: '/',
  socks_username: 'zxy',
  socks_password: '',
  socks_udp: false,
  remark: '',
  enabled: true,
})
const form = ref<any>(emptyForm())

const selectedServer = computed(() => servers.value.find((s:any) => s.id === form.value.server_id))
const shareClients = computed(() => shareNodeData.value ? clientsForNode(shareNodeData.value, clients.value) : [])
const modeTip = computed(() => {
  if (mode.value === 'test') return '测试模式：VLESS + TCP + none，用于验证连通性，不建议正式长期使用。'
  if (mode.value === 'recommended') return '推荐模式：VLESS + Reality + TCP，自动生成密钥、Short ID 和客户端链接参数。'
  if (mode.value === 'socks5') return 'SOCKS5 协议：用于落地服务器创建 SOCKS5 入站，供中转服务器作为固定出口连接。必须设置账号密码，并建议只允许中转服务器 IP 访问。'
  return '高级模式：保留更多字段，适合懂 Xray 配置的用户手动调整。建议节点端口使用 10000-60000，部分服务器低端口外部不可达。'
})

function normalizeApiError(e:any) {
  try { const data = JSON.parse(e.message); return data.error || e.message } catch { return e?.message || '操作失败' }
}
function fillFromServer() {
  const s = selectedServer.value
  if (!s) return
  if (!form.value.host) form.value.host = s.host || s.ip || ''
}
watch(() => form.value.server_id, fillFromServer)
watch(() => form.value.protocol, () => {
  if (form.value.protocol === 'socks') applySocks5(false)
})
watch(() => form.value.security, () => {
  if (form.value.security === 'none') form.value.sni = ''
  if (form.value.security === 'reality') applyRecommended(false)
})
watch(() => form.value.transport, () => {
  if (form.value.transport === 'tcp') form.value.path = ''
  if (form.value.transport === 'ws' && !form.value.path) form.value.path = '/zxy'
  if (form.value.transport === 'grpc' && !form.value.path) form.value.path = 'zxy'
})
watch(shareClientId, updateShareLink)
watch(shareFormat, updateShareLink)

async function load() {
  try {
    nodes.value = await api('/api/nodes')
    servers.value = await api('/api/servers')
    clients.value = await api('/api/clients')
    if (!nodes.value.length) showEditor.value = true
    if (!form.value.server_id && servers.value[0]) {
      form.value.server_id = servers.value[0].id
      fillFromServer()
    }
    if (mode.value === 'recommended') await ensureRealityKeys(false)
    updateShareLink()
  } catch(e:any) { error.value = normalizeApiError(e) }
}

async function ensureRealityKeys(force = false) {
  if (form.value.security !== 'reality') return
  if (!force && form.value.reality_private_key && form.value.reality_public_key && form.value.reality_short_id) return
  const res = await api('/api/nodes/reality-keys', { method: 'POST' })
  form.value.reality_private_key = res.private_key
  form.value.reality_public_key = res.public_key
  form.value.reality_short_id = res.short_id
  form.value.reality_spider_x = res.spider_x || '/'
  form.value.fingerprint = res.fingerprint || 'chrome'
  if (!form.value.reality_dest) form.value.reality_dest = res.dest || 'www.intel.com:443'
  if (!form.value.sni) form.value.sni = res.sni || 'www.intel.com'
}

function applyPreset(p:any) {
  form.value.reality_dest = p.dest
  form.value.sni = p.sni
}
async function applyRecommended(generate = true) {
  form.value.protocol = 'vless'
  form.value.transport = 'tcp'
  form.value.security = 'reality'
  form.value.path = ''
  form.value.fingerprint = form.value.fingerprint || 'chrome'
  form.value.reality_dest = form.value.reality_dest || 'www.intel.com:443'
  form.value.sni = form.value.sni || 'www.intel.com'
  form.value.reality_spider_x = form.value.reality_spider_x || '/'
  if (generate) await ensureRealityKeys(false)
}
function applySocks5(generate = true) {
  form.value.protocol = 'socks'
  form.value.transport = 'tcp'
  form.value.security = 'none'
  form.value.path = ''
  form.value.sni = ''
  form.value.fingerprint = ''
  form.value.reality_dest = ''
  form.value.reality_private_key = ''
  form.value.reality_public_key = ''
  form.value.reality_short_id = ''
  form.value.reality_spider_x = ''
  form.value.socks_username = form.value.socks_username || 'zxy'
  if (generate || !form.value.socks_password) form.value.socks_password = randomPassword(22)
}
async function setMode(next:'test'|'recommended'|'socks5'|'advanced') {
  mode.value = next
  if (next === 'test') {
    form.value.protocol = 'vless'
    form.value.transport = 'tcp'
    form.value.security = 'none'
    form.value.sni = ''
    form.value.path = ''
  }
  if (next === 'recommended') await applyRecommended(true)
  if (next === 'socks5') applySocks5(true)
}
function detectMode(n:any) {
  if ((n.protocol || '').toLowerCase() === 'socks') return 'socks5'
  if ((n.security || 'none') === 'reality') return 'recommended'
  if ((n.protocol || 'vless') === 'vless' && (n.transport || 'tcp') === 'tcp' && (n.security || 'none') === 'none') return 'test'
  return 'advanced'
}
function validateLocal() {
  if (!form.value.server_id && servers.value.length === 0) return '单机服务器尚未初始化，请到系统检测查看或重新运行安装脚本。'
  if (!String(form.value.name || '').trim()) return '请填写入站名称，例如：美国01。'
  if (!String(form.value.host || '').trim()) return '请填写入站域名/Host。没有域名时可先填服务器公网 IP。'
  const port = Number(form.value.port)
  if (!port || port < 1 || port > 65535) return '端口必须在 1-65535 之间。'
  if (port < 10000 && !confirm('当前端口低于 10000，部分服务器商或机房会限制低端口外部访问，可能导致 V2rayN / 小火箭连接失败。建议改用 10000-60000。仍然继续保存吗？')) return '已取消保存，请把端口改为 10000-60000 之间再试。'
  if (form.value.protocol === 'socks') {
    if (!String(form.value.socks_username || '').trim()) return 'SOCKS5 入站必须填写账号。'
    if (!String(form.value.socks_password || '').trim()) return 'SOCKS5 入站必须填写密码。'
  }
  if (form.value.security === 'reality' && !String(form.value.reality_dest || '').includes(':443')) return 'Reality 目标建议填写类似 www.intel.com:443。'
  return ''
}
function resetForm() {
  const sid = form.value.server_id || servers.value[0]?.id || ''
  form.value = emptyForm()
  form.value.server_id = sid
  editingId.value = ''
  mode.value = 'recommended'
  fillFromServer()
  applyRecommended(true)
}
async function saveNode() {
  error.value = ''; message.value = ''
  const err = validateLocal()
  if (err) { error.value = err; return }
  saving.value = true
  try {
    if (form.value.protocol !== 'socks' && form.value.security === 'reality') await ensureRealityKeys(false)
    const payload = { ...form.value, port: Number(form.value.port) }
    if (payload.transport === 'tcp') payload.path = ''
    if (editingId.value) {
      await api(`/api/nodes/${editingId.value}`, { method:'PUT', body:JSON.stringify(payload) })
      message.value = form.value.protocol === 'socks' ? 'SOCKS5 入站已保存。Agent 同步后会下发到对应落地服务器。' : '入站已保存。Reality/订阅配置会在 Agent 下一次同步时更新。'
    } else {
      await api('/api/nodes', { method:'POST', body:JSON.stringify(payload) })
      message.value = form.value.protocol === 'socks' ? 'SOCKS5 落地入站已新增。请放行端口，并建议只允许中转服务器 IP 访问。' : '入站已新增。推荐模式会自动生成 Reality 客户端链接参数。'
    }
    resetForm()
    await load()
    showEditor.value = false
  } catch(e:any) { error.value = normalizeApiError(e) } finally { saving.value = false }
}
function editNode(n:any) {
  showEditor.value = true
  editingId.value = n.id
  mode.value = detectMode(n) as any
  form.value = {
    server_id: n.server_id,
    name: n.name,
    protocol: n.protocol || 'vless',
    host: n.host || '',
    port: n.port || randomRecommendedPort(),
    transport: n.transport || 'tcp',
    security: n.security || 'none',
    sni: n.sni || '',
    path: n.transport === 'tcp' ? '' : (n.path || (n.transport === 'ws' ? '/zxy' : 'zxy')),
    fingerprint: n.fingerprint || 'chrome',
    reality_dest: n.reality_dest || 'www.intel.com:443',
    reality_private_key: n.reality_private_key || '',
    reality_public_key: n.reality_public_key || '',
    reality_short_id: n.reality_short_id || '',
    reality_spider_x: n.reality_spider_x || '/',
    socks_username: n.socks_username || 'zxy',
    socks_password: n.socks_password || '',
    socks_udp: n.socks_udp === true,
    remark: n.remark || '',
    enabled: n.enabled !== false,
  }
  config.value = ''
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
async function remove(id:string) {
  if(!confirm('确认删除这个入站？删除后，客户订阅里也会移除这个入站。')) return
  error.value = ''; message.value = ''
  try {
    await api(`/api/nodes/${id}`,{method:'DELETE'})
    if (editingId.value === id) resetForm()
    if (shareNodeData.value?.id === id) closeShare()
    message.value = '入站已删除。'
    await load()
  } catch(e:any) { error.value = normalizeApiError(e) }
}
async function preview(id:string) {
  error.value = ''; message.value = ''
  try { const res = await api(`/api/nodes/${id}/xray-config`); config.value = JSON.stringify(res,null,2) } catch(e:any) { error.value = normalizeApiError(e) }
}
function updateShareLink() {
  if (!shareNodeData.value || !shareClientId.value) { shareLink.value = ''; return }
  const c = clients.value.find((x:any) => x.id === shareClientId.value)
  if (!c) { shareLink.value = ''; return }
  shareLink.value = buildClientShare(shareNodeData.value, c, shareFormat.value)
}
function openShare(n:any) {
  if (String(n?.protocol || '').toLowerCase() === 'socks') {
    message.value = 'SOCKS5 入站是落地出口，不生成客户二维码；后续中转服务器通过 SOCKS5 出站连接它。'
    return
  }
  shareNodeData.value = n
  const available = clientsForNode(n, clients.value)
  shareClientId.value = available[0]?.id || ''
  updateShareLink()
}
function closeShare() { shareNodeData.value = null; shareClientId.value = ''; shareLink.value = '' }
function openSocksInfo(n:any) { socksNodeData.value = n; socksPasswordVisible.value = false }
function closeSocksInfo() { socksNodeData.value = null; socksPasswordVisible.value = false }
function socksPasswordDisplay(n:any) {
  const pwd = String(n?.socks_password || '')
  if (!pwd) return '未设置'
  return socksPasswordVisible.value ? pwd : '••••••••••••••••'
}
function socksOutboundParams(n:any) {
  return [
    `协议：SOCKS5`,
    `地址：${n?.host || ''}`,
    `端口：${n?.port || ''}`,
    `用户：${n?.socks_username || ''}`,
    `密码：${n?.socks_password || ''}`,
    `UDP：${n?.socks_udp ? '开启' : '关闭'}`,
  ].join('\n')
}
function socksFirewallCommand(n:any) {
  const port = Number(n?.port || 0)
  if (!port) return ''
  const lines = [`ufw allow ${port}/tcp`]
  if (n?.socks_udp) lines.push(`ufw allow ${port}/udp`)
  lines.push('ufw reload')
  return lines.join('\n')
}
async function copyValue(label:string, value:any) {
  const text = String(value || '')
  if (!text) { message.value = `${label}为空，无法复制。`; return }
  const ok = await copyText(text)
  message.value = ok ? `${label}已复制。` : `浏览器禁止自动复制，请手动复制${label}。`
}
function openQrZoom(text:string, title='节点二维码') { qrZoomText.value = text || ''; qrZoomTitle.value = title }
function closeQrZoom() { qrZoomText.value = ''; qrZoomTitle.value = '' }
async function copyShareLink() {
  if (!shareLink.value) return
  const ok = await copyText(shareLink.value)
  message.value = ok ? `${shareFormatLabel(shareFormat.value)} 内容已复制。` : '浏览器禁止自动复制，已在分享卡片中显示完整内容，请手动复制。'
}
async function copyRealityPublic() {
  if (!form.value.reality_public_key) return
  const ok = await copyText(form.value.reality_public_key)
  message.value = ok ? 'Reality 公钥已复制。' : '请手动复制 Reality 公钥。'
}
onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">入站管理</h1>
      <p class="page-desc">V0.7.5.5 入站管理清理版：协议下拉选择 VLESS 或 SOCKS5，快捷模板只负责填默认参数。</p>
    </div>
  </div>

  <div class="notice ok">新增入站默认绑定选中的服务器。客户节点建议使用 VLESS Reality；落地出口可创建 SOCKS5 入站，供后续中转服务器连接。</div>
  <div class="notice warn">部分服务器商或机房会限制 10000 以下低端口外部访问。Xray 即使显示已监听，客户端也可能连不上；建议新节点统一使用 10000-60000。</div>

  <div class="panel node-list-panel">
    <div class="section-head">
      <div><h2>入站列表</h2><p>先看已有入站，新增或编辑时再展开配置表单，避免创建后找不到结果。</p></div>
      <div class="row-actions">
        <button class="btn" @click="resetForm(); showEditor = true">新增入站</button>
        <button class="btn secondary" @click="load">刷新</button>
      </div>
    </div>
    <div class="table-wrap">
<table>
    <thead><tr><th>入站</th><th>协议</th><th>地址</th><th>端口</th><th>传输</th><th>安全</th><th>状态</th><th>操作</th></tr></thead>
    <tbody>
      <tr v-for="n in nodes" :key="n.id">
        <td><strong>{{ n.name || '未命名入站' }}</strong><br><span class="muted">{{ n.remark }}</span></td>
        <td><span class="badge">{{ n.protocol }}</span><br><span v-if="String(n.protocol || '').toLowerCase() === 'socks'" class="muted">{{ n.socks_username || '无账号' }}</span></td>
        <td>{{ n.host }}</td><td>{{ n.port }}<br><span v-if="Number(n.port) < 10000" class="muted danger-text">低端口风险</span><span v-else class="muted">推荐端口</span></td><td>{{ n.transport }}</td>
        <td><span class="badge" :class="n.security==='reality'?'online':''">{{ String(n.protocol || '').toLowerCase() === 'socks' ? (n.socks_udp ? 'UDP开' : 'UDP关') : n.security }}</span><br><span v-if="n.security==='reality'" class="muted">{{ n.sni }}</span></td>
        <td><span class="badge" :class="n.enabled?'online':''">{{ n.enabled ? '启用' : '停用' }}</span></td>
        <td class="row-actions"><button class="btn secondary" @click="String(n.protocol || '').toLowerCase() === 'socks' ? openSocksInfo(n) : openShare(n)">{{ String(n.protocol || '').toLowerCase() === 'socks' ? '落地出口' : '链接/二维码' }}</button><button class="btn secondary" @click="editNode(n)">编辑</button><button class="btn secondary" @click="preview(n.id)">配置预览</button><button class="btn danger" @click="remove(n.id)">删除</button></td>
      </tr>
    </tbody></table>
    </div>
  </div>

  <div v-if="showEditor" class="modal-mask" @click.self="showEditor = false">
    <div class="modal-card node-editor-modal node-editor">
      <div class="modal-head">
        <div>
          <span class="eyebrow">入站配置</span>
          <h2>{{ editingId ? '编辑入站' : '新增入站' }}</h2>
          <p>先选择协议，再选择快捷模板。客户节点推荐 VLESS Reality；落地出口协议选择 SOCKS5。</p>
        </div>
        <button class="icon-btn" @click="showEditor = false">×</button>
      </div>
    <div class="mode-tabs">
      <button class="mode-tab" :class="{active:mode==='recommended'}" @click="setMode('recommended')"><strong>推荐模板</strong><span>VLESS / Reality / TCP</span></button>
      <button class="mode-tab" :class="{active:mode==='test'}" @click="setMode('test')"><strong>测试模板</strong><span>VLESS / TCP / none</span></button>
      <button class="mode-tab" :class="{active:mode==='advanced'}" @click="setMode('advanced')"><strong>高级模板</strong><span>手动调整字段</span></button>
    </div>
    <div class="notice warn mode-tip">{{ modeTip }}</div>

    <div class="form node-form clean-form">
      <label class="field"><span>入站名称</span><input v-model="form.name" placeholder="例如：美国01 Reality" /></label>
      <label class="field"><span>入站域名 / Host</span><input v-model="form.host" placeholder="服务器公网 IP 或域名" /></label>
      <label class="field"><span>端口</span><input v-model.number="form.port" placeholder="例如 25642" /><em v-if="Number(form.port) && Number(form.port) < 10000" class="field-tip danger-tip">当前端口低于 10000，部分服务器外部不可达，建议改为 10000-60000。</em><em v-else class="field-tip">推荐 10000-60000；创建后用该端口生成客户端链接。</em></label>
      <label class="field"><span>协议</span><select v-model="form.protocol"><option value="vless">VLESS</option><option value="socks">SOCKS5</option><option v-if="mode==='advanced'" value="vmess">VMess</option><option v-if="mode==='advanced'" value="trojan">Trojan</option><option v-if="mode==='advanced'" value="shadowsocks">Shadowsocks</option></select><em class="field-tip">协议决定入站类型；SOCKS5 主要用于落地出口。</em></label>
      <label class="field"><span>传输方式</span><select v-model="form.transport" :disabled="mode==='recommended' || form.protocol==='socks'"><option>tcp</option><option v-if="mode==='advanced'">ws</option><option v-if="mode==='advanced'">grpc</option></select></label>
      <label class="field"><span>安全方式</span><select v-model="form.security" :disabled="mode==='recommended' || form.protocol==='socks'"><option>none</option><option v-if="mode==='advanced'">tls</option><option>reality</option></select></label>
      <label class="field" v-if="form.protocol !== 'socks' && form.transport !== 'tcp'"><span>{{ form.transport === 'grpc' ? '服务名称' : '路径' }}</span><input v-model="form.path" /></label>
      <label class="field" v-else><span>路径</span><input :value="form.protocol === 'socks' ? 'SOCKS5 不需要填写路径' : 'TCP 模式不需要填写路径'" disabled /></label>
      <label class="field wide"><span>备注</span><input v-model="form.remark" placeholder="例如：正式客户 / TikTok 专线 / AI 工具" /></label>
    </div>

    <div v-if="form.protocol === 'socks'" class="reality-box">
      <div class="row-between">
        <div><h2>SOCKS5 落地入站</h2><p class="page-desc">用于落地服务器提供 SOCKS5 入口。正式中转时，中转服务器会通过 SOCKS5 出站连接这里，平台最终看到落地服务器 IP。</p></div>
        <button class="btn secondary" @click="form.socks_password = randomPassword(22)">重新生成密码</button>
      </div>
      <div class="notice warn">安全提醒：SOCKS5 不应裸奔。必须设置账号密码，服务器安全组建议只允许中转服务器 IP 访问该端口。</div>
      <div class="form node-form clean-form reality-form">
        <label class="field"><span>SOCKS5 账号</span><input v-model="form.socks_username" placeholder="zxy" /></label>
        <label class="field"><span>SOCKS5 密码</span><input v-model="form.socks_password" placeholder="自动生成或手动填写" /></label>
        <label class="field"><span>UDP 转发</span><select v-model="form.socks_udp"><option :value="false">关闭 UDP（推荐先测 TCP）</option><option :value="true">开启 UDP（实验）</option></select><em class="field-tip">当前只是 SOCKS5 入站 UDP 开关，不等于 VLESS Reality 原生 UDP。</em></label>
        <label class="field wide"><span>给中转服务器填写</span><input :value="`${form.host || '落地IP'}:${form.port} / ${form.socks_username} / ${form.socks_password}`" readonly /></label>
      </div>
    </div>

    <div v-if="form.protocol !== 'socks' && form.security==='reality'" class="reality-box">
      <div class="row-between">
        <div><h2>Reality 推荐配置</h2><p class="page-desc">用于提升连接层 TLS 特征自然度。账号稳定性仍取决于 IP、DNS、设备环境和操作行为。</p></div>
        <button class="btn secondary" @click="ensureRealityKeys(true)">重新生成密钥</button>
      </div>
      <div class="preset-row"><button v-for="p in realityPresets" :key="p.dest" class="btn secondary" @click="applyPreset(p)">{{ p.label }}</button></div>
      <div class="form node-form clean-form reality-form">
        <label class="field"><span>伪装目标</span><input v-model="form.reality_dest" placeholder="www.intel.com:443" /></label>
        <label class="field"><span>SNI</span><input v-model="form.sni" placeholder="www.intel.com" /></label>
        <label class="field"><span>uTLS 指纹</span><select v-model="form.fingerprint"><option>chrome</option><option>firefox</option><option>safari</option><option>ios</option><option>randomized</option></select></label>
        <label class="field"><span>SpiderX</span><input v-model="form.reality_spider_x" placeholder="/" /></label>
        <label class="field wide"><span>公钥（客户端使用）</span><input v-model="form.reality_public_key" readonly @click="copyRealityPublic" /></label>
        <label class="field wide"><span>私钥（仅服务端使用）</span><input v-model="form.reality_private_key" readonly /></label>
        <label class="field"><span>Short ID</span><input v-model="form.reality_short_id" /></label>
      </div>
    </div>

    <div class="actions editor-actions"><button class="btn" @click="saveNode" :disabled="saving">{{ editingId ? '保存修改' : '新增入站' }}</button><button v-if="editingId" class="btn secondary" @click="resetForm">取消编辑</button><button class="btn secondary" @click="showEditor = false">关闭</button></div>
    </div>
  </div>

  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>


  <div v-if="shareNodeData" class="panel share-panel">
    <div class="row-between"><div><h2>客户端分享：{{ shareNodeData.name }}</h2><p class="page-desc">选择客户和客户端类型后，生成专属节点链接、二维码或配置文件。</p></div><button class="btn secondary" @click="closeShare">关闭</button></div>
    <div v-if="shareClients.length === 0" class="notice warn">当前没有可用于该入站的启用客户，请先到客户管理新增客户。</div>
    <div v-else>
      <div class="share-toolbar">
        <label class="field"><span>选择客户</span><select v-model="shareClientId"><option v-for="c in shareClients" :key="c.id" :value="c.id">{{ c.username }}｜{{ c.email || c.uuid }}</option></select></label>
        <label class="field"><span>客户端类型</span><select v-model="shareFormat"><option v-for="opt in shareFormatOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option></select></label>
      </div>
      <div class="notice warn">{{ shareFormatOptions.find(x => x.value === shareFormat)?.tip }}</div>
      <div class="share-grid">
        <div class="share-box"><strong>{{ shareFormatLabel(shareFormat) }}</strong><div class="code share-code">{{ shareLink }}</div><div class="share-actions"><button class="btn" @click="copyShareLink">复制{{ shareFormatLabel(shareFormat) }}</button></div></div>
        <div class="qr-box" v-if="shareLink && isQrShareFormat(shareFormat)"><img :src="qrImageUrl(shareLink, 560)" alt="节点二维码" /><span>{{ shareFormatLabel(shareFormat) }} 单节点二维码</span><button class="btn" @click="openQrZoom(shareLink, shareFormatLabel(shareFormat) + ' 单节点二维码')">放大扫码</button></div>
      </div>
    </div>
  </div>



  <div v-if="socksNodeData" class="modal-mask" @click.self="closeSocksInfo">
    <div class="modal-card socks-modal-card">
      <div class="modal-head">
        <div>
          <span class="eyebrow">SOCKS5 落地出口</span>
          <h2>落地出口：{{ socksNodeData.name }}</h2>
          <p>这是落地服务器上的 SOCKS5 入站信息。给中转服务器配置 SOCKS5 出站时，直接填写下面这些参数。</p>
        </div>
        <button class="icon-btn" @click="closeSocksInfo">×</button>
      </div>

      <div class="notice warn modal-tip">安全提醒：SOCKS5 只建议给中转服务器使用。正式环境建议在云安全组或防火墙里只允许中转服务器 IP 访问该端口。</div>

      <div class="detail-grid socks-detail-grid">
        <div><span>协议</span><strong>SOCKS5</strong></div>
        <div><span>落地服务器 IP / Host</span><strong>{{ socksNodeData.host }}</strong></div>
        <div><span>端口</span><strong>{{ socksNodeData.port }}</strong></div>
        <div><span>UDP 状态</span><strong>{{ socksNodeData.socks_udp ? '开启' : '关闭' }}</strong></div>
        <div><span>账号</span><strong>{{ socksNodeData.socks_username || '未设置' }}</strong></div>
        <div><span>密码</span><strong>{{ socksPasswordDisplay(socksNodeData) }}</strong></div>
      </div>

      <div class="modal-actions socks-copy-actions">
        <button class="btn secondary" @click="copyValue('SOCKS5 地址', socksNodeData.host)">复制地址</button>
        <button class="btn secondary" @click="copyValue('SOCKS5 端口', socksNodeData.port)">复制端口</button>
        <button class="btn secondary" @click="copyValue('SOCKS5 账号', socksNodeData.socks_username)">复制账号</button>
        <button class="btn secondary" @click="copyValue('SOCKS5 密码', socksNodeData.socks_password)">复制密码</button>
        <button class="btn secondary" @click="socksPasswordVisible = !socksPasswordVisible">{{ socksPasswordVisible ? '隐藏密码' : '显示密码' }}</button>
      </div>

      <div class="share-modal-body socks-modal-body">
        <div class="share-config-box">
          <div class="share-config-head"><strong>中转出站填写参数</strong><button class="btn secondary" @click="copyValue('中转出站参数', socksOutboundParams(socksNodeData))">复制参数</button></div>
          <pre class="share-pre">{{ socksOutboundParams(socksNodeData) }}</pre>
        </div>
        <div class="share-config-box">
          <div class="share-config-head"><strong>防火墙放行命令</strong><button class="btn secondary" @click="copyValue('防火墙命令', socksFirewallCommand(socksNodeData))">复制命令</button></div>
          <pre class="share-pre">{{ socksFirewallCommand(socksNodeData) }}</pre>
        </div>
      </div>
    </div>
  </div>

  <div v-if="qrZoomText" class="modal-mask qr-zoom-mask" @click.self="closeQrZoom">
    <div class="modal-card qr-zoom-card">
      <div class="modal-head"><div><span class="eyebrow">放大二维码</span><h2>{{ qrZoomTitle }}</h2><p>请让二维码尽量充满屏幕，V2rayN 扫屏幕时优先使用这个放大二维码。</p></div><button class="icon-btn" @click="closeQrZoom">×</button></div>
      <div class="qr-zoom-body"><div class="qr-white-stage"><img :src="qrImageUrl(qrZoomText, 760)" alt="放大二维码" /></div><div class="qr-zoom-actions"><button class="btn" @click="copyShareLink">复制当前链接</button><button class="btn secondary" @click="closeQrZoom">关闭</button></div></div>
    </div>
  </div>

  <pre v-if="config" class="card code config-preview">{{ config }}</pre>
</template>
