<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const relays = ref<any[]>([])
const servers = ref<any[]>([])
const nodes = ref<any[]>([])
const clients = ref<any[]>([])
const message = ref('')
const error = ref('')
const selectedRelayId = ref('')
const selectedClientId = ref('')
const testResult = ref<any>(null)
const testingSocks = ref(false)
const savedOutlets = ref<any[]>([])
const selectedSavedOutletId = ref('')
const showRelayEditor = ref(false)
const relayDetail = ref<any | null>(null)

const OUTLET_STORE_KEY = 'zxy_socks5_outlet_bookmarks_v1'

function randomRelayPort() { return Math.floor(10000 + Math.random() * 50000) }
function defaultForm() {
  return {
    name: '',
    route_mode: 'socks5_route',
    relay_server_id: servers.value[0]?.id || '',
    landing_node_id: '',
    landing_mode: 'manual_socks5',
    manual_socks_host: '',
    manual_socks_port: '',
    manual_socks_username: '',
    manual_socks_password: '',
    manual_socks_udp: false,
    relay_host: '',
    relay_port: randomRelayPort(),
    relay_network: 'tcp',
    relay_reality_dest: 'www.intel.com:443',
    relay_sni: 'www.intel.com',
    relay_fingerprint: 'chrome',
    relay_reality_spider_x: '/',
    remark: '',
    enabled: true
  }
}
const form = ref<any>(defaultForm())

const vlessRealityNodes = computed(() => nodes.value.filter((n:any) => n.enabled !== false && String(n.protocol).toLowerCase() === 'vless' && String(n.security).toLowerCase() === 'reality'))
const socksNodes = computed(() => nodes.value.filter((n:any) => n.enabled !== false && String(n.protocol).toLowerCase() === 'socks'))
const landingNodes = computed(() => form.value.route_mode === 'socks5_route' ? socksNodes.value : vlessRealityNodes.value)
const selectedServer = computed(() => servers.value.find((s:any) => s.id === form.value.relay_server_id))
const selectedRelay = computed(() => relays.value.find((r:any) => r.id === selectedRelayId.value))
const selectedClient = computed(() => clients.value.find((c:any) => c.id === selectedClientId.value))
const selectedLanding = computed(() => selectedRelay.value ? nodes.value.find((n:any) => n.id === selectedRelay.value.landing_node_id) : null)
const formLanding = computed(() => nodes.value.find((n:any) => n.id === form.value.landing_node_id))
const formIsVlessRealityTCP = computed(() => isVlessRealityTCP(formLanding.value))
const tcpUdpRelays = computed(() => relays.value.filter((r:any) => routeMode(r) === 'tcp_forward' && String(r.relay_network || 'tcp') === 'tcp,udp'))
const tcpUdpRelayNames = computed(() => tcpUdpRelays.value.map((r:any) => r.name).join('、'))
const socks5RouteRelays = computed(() => relays.value.filter((r:any) => routeMode(r) === 'socks5_route'))
const formUsesManualSocks = computed(() => form.value.route_mode === 'socks5_route' && String(form.value.landing_mode || 'manual_socks5') === 'manual_socks5')
const formUsesPanelSocks = computed(() => form.value.route_mode === 'socks5_route' && String(form.value.landing_mode || '') === 'panel_node')

function routeMode(r:any) { return String(r?.route_mode || 'tcp_forward') }
function isVlessRealityTCP(n:any) {
  if (!n) return false
  const transport = String(n.transport || 'tcp').toLowerCase()
  return String(n.protocol || '').toLowerCase() === 'vless' && String(n.security || '').toLowerCase() === 'reality' && (!transport || transport === 'tcp')
}

function serverName(id:string) { const s = servers.value.find((x:any) => x.id === id); return s ? `${s.name || '服务器'} / ${s.host || s.ip}` : id }
function nodeName(id:string) { const n = nodes.value.find((x:any) => x.id === id); return n ? `${n.name} / ${n.host}:${n.port}` : id }
function landingLabel(r:any) {
  if (!r) return '-'
  if (routeMode(r) === 'socks5_route' && String(r.landing_mode || '') === 'manual_socks5') return `手动远程 SOCKS5 / ${r.manual_socks_host}:${r.manual_socks_port}`
  return nodeName(r.landing_node_id)
}
function socksTargetLabel(r:any) {
  if (!r) return '-'
  if (routeMode(r) === 'socks5_route') {
    if (String(r.landing_mode || '') === 'manual_socks5') return `${r.manual_socks_host}:${r.manual_socks_port}`
    const n = nodes.value.find((x:any) => x.id === r.landing_node_id)
    return n ? `${n.host}:${n.port}` : '-'
  }
  return landingLabel(r)
}
function relayFlowSummary(r:any) {
  if (!r) return '-'
  if (routeMode(r) === 'socks5_route') return `入口 ${r.relay_host}:${r.relay_port} → 出口 ${socksTargetLabel(r)}`
  return `入口 ${r.relay_host}:${r.relay_port} → 落地 ${landingLabel(r)}`
}
function valueOr(v:any, fallback:string) { const s = String(v || '').trim(); return s || fallback }
function fillRelayHost() {
  const s = selectedServer.value
  if (s && !form.value.relay_host) form.value.relay_host = s.host || s.ip || ''
}
function ensureLandingDefault() {
  if (form.value.route_mode === 'socks5_route' && String(form.value.landing_mode || 'manual_socks5') === 'manual_socks5') {
    form.value.landing_node_id = ''
    return
  }
  const exists = landingNodes.value.find((n:any) => n.id === form.value.landing_node_id)
  if (!exists) form.value.landing_node_id = landingNodes.value[0]?.id || ''
}
watch(() => form.value.relay_server_id, fillRelayHost)
watch(() => form.value.route_mode, () => {
  form.value.relay_network = form.value.route_mode === 'socks5_route' ? 'tcp' : (form.value.relay_network || 'tcp')
  if (form.value.route_mode === 'socks5_route' && !form.value.landing_mode) form.value.landing_mode = 'manual_socks5'
  ensureLandingDefault()
})
watch(() => form.value.landing_mode, ensureLandingDefault)

async function load() {
  try {
    relays.value = await api('/api/relays')
    servers.value = await api('/api/servers')
    nodes.value = await api('/api/nodes')
    clients.value = await api('/api/clients')
    if (!form.value.relay_server_id && servers.value[0]) form.value.relay_server_id = servers.value[0].id
    fillRelayHost()
    ensureLandingDefault()
    if (!selectedRelayId.value && relays.value[0]) selectedRelayId.value = relays.value[0].id
    if (!selectedClientId.value && clients.value[0]) selectedClientId.value = clients.value[0].id
  } catch(e:any) { error.value = e.message || '加载失败' }
}

function loadSavedOutlets() {
  try {
    const raw = localStorage.getItem(OUTLET_STORE_KEY)
    savedOutlets.value = raw ? JSON.parse(raw) : []
  } catch {
    savedOutlets.value = []
  }
}
function persistSavedOutlets() { localStorage.setItem(OUTLET_STORE_KEY, JSON.stringify(savedOutlets.value.slice(0, 50))) }
function manualSocksValidate() {
  if (!String(form.value.manual_socks_host || '').trim()) return '请填写远程落地 SOCKS5 地址，例如 203.0.113.10。'
  const sp = Number(form.value.manual_socks_port)
  if (!sp || sp < 1 || sp > 65535) return '请填写正确的远程落地 SOCKS5 端口。'
  if (!String(form.value.manual_socks_username || '').trim()) return '请填写远程落地 SOCKS5 账号。'
  if (!String(form.value.manual_socks_password || '').trim()) return '请填写远程落地 SOCKS5 密码。'
  return ''
}
function saveCurrentOutlet() {
  error.value = ''; message.value = ''
  const err = manualSocksValidate()
  if (err) { error.value = err; return }
  const host = String(form.value.manual_socks_host || '').trim()
  const port = Number(form.value.manual_socks_port)
  const id = `${host}:${port}:${String(form.value.manual_socks_username || '').trim()}`
  const item = {
    id,
    name: `${host}:${port}`,
    host,
    port,
    username: String(form.value.manual_socks_username || '').trim(),
    password: String(form.value.manual_socks_password || ''),
    udp: !!form.value.manual_socks_udp,
    updated_at: new Date().toISOString()
  }
  savedOutlets.value = [item, ...savedOutlets.value.filter((x:any) => x.id !== id)].slice(0, 30)
  selectedSavedOutletId.value = id
  persistSavedOutlets()
  message.value = '已保存到本浏览器的落地出口库。下次可直接选择套用。'
}
function applySelectedOutlet() {
  const item = savedOutlets.value.find((x:any) => x.id === selectedSavedOutletId.value)
  if (!item) return
  form.value.manual_socks_host = item.host
  form.value.manual_socks_port = Number(item.port)
  form.value.manual_socks_username = item.username
  form.value.manual_socks_password = item.password
  form.value.manual_socks_udp = !!item.udp
  message.value = `已套用落地出口：${item.host}:${item.port}`
}
function removeSavedOutlet() {
  if (!selectedSavedOutletId.value) { error.value = '请先选择要删除的落地出口。'; return }
  savedOutlets.value = savedOutlets.value.filter((x:any) => x.id !== selectedSavedOutletId.value)
  selectedSavedOutletId.value = ''
  persistSavedOutlets()
  message.value = '已从本浏览器落地出口库删除。'
}

function validate() {
  if (!String(form.value.name || '').trim()) return '请填写中转名称，例如：154中转-128出口测试。'
  if (!form.value.relay_server_id) return '请选择中转服务器。'
  const p = Number(form.value.relay_port)
  if (!p || p < 10000 || p > 60000) return '中转端口建议使用 10000-60000。'
  if (form.value.route_mode === 'socks5_route') {
    if (formUsesManualSocks.value) return manualSocksValidate()
    if (!form.value.landing_node_id) return '请选择本面板 SOCKS5 落地入站，或者切换为手动填写远程 SOCKS5。'
    if (!formLanding.value || String(formLanding.value.protocol).toLowerCase() !== 'socks') return 'SOCKS5 路由中转必须选择 SOCKS5 落地入站。'
    if (!String(formLanding.value.socks_username || '').trim() || !String(formLanding.value.socks_password || '').trim()) return '落地 SOCKS5 必须设置账号和密码。'
    return ''
  }
  if (!form.value.landing_node_id) return 'TCP 透传中转请选择 VLESS Reality 落地节点。'
  if (!formLanding.value || !isVlessRealityTCP(formLanding.value)) return 'TCP 透传中转请选择 VLESS Reality TCP 落地节点。'
  if (formIsVlessRealityTCP.value && form.value.relay_network === 'udp') return '当前落地节点是 VLESS Reality TCP，不支持 UDP-only 中转。请改用 TCP。'
  if (formIsVlessRealityTCP.value && form.value.relay_network === 'tcp,udp' && !confirm('当前落地节点是 VLESS Reality TCP。TCP+UDP 只是实验模式，可能导致速度下降，正式使用建议选 TCP。是否继续创建？')) return '已取消创建。'
  return ''
}

async function testManualSocks() {
  error.value = ''; message.value = ''; testResult.value = null
  const err = manualSocksValidate()
  if (err) { error.value = err; return }
  testingSocks.value = true
  try {
    const res = await api('/api/tools/test-socks5', { method:'POST', body: JSON.stringify({
      host: String(form.value.manual_socks_host || '').trim(),
      port: Number(form.value.manual_socks_port),
      username: String(form.value.manual_socks_username || '').trim(),
      password: String(form.value.manual_socks_password || '')
    }) })
    testResult.value = res
    if (res.ok) message.value = `远程 SOCKS5 连通，检测出口 IP：${res.exit_ip}，耗时 ${res.latency_ms}ms。`
    else error.value = `远程 SOCKS5 测试失败：${res.message}`
  } catch(e:any) {
    error.value = e.message || '远程 SOCKS5 测试失败'
  } finally {
    testingSocks.value = false
  }
}

async function createRelay() {
  error.value = ''; message.value = ''
  const err = validate()
  if (err) { error.value = err; return }
  try {
    const payload = { ...form.value, relay_port: Number(form.value.relay_port), manual_socks_port: Number(form.value.manual_socks_port || 0), relay_network: form.value.route_mode === 'socks5_route' ? 'tcp' : (form.value.relay_network || 'tcp') }
    await api('/api/relays', { method:'POST', body: JSON.stringify(payload) })
    message.value = form.value.route_mode === 'socks5_route' ? 'SOCKS5 路由中转已创建。本机 Agent 同步后，中转服务器会生成 VLESS Reality 入站、SOCKS5 出站和路由绑定。' : 'TCP 透传中转线路已创建。Agent 同步后，中转服务器会监听该端口并转发到落地 Reality 节点。'
    const keepServer = form.value.relay_server_id
    const keepHost = form.value.relay_host
    form.value = defaultForm()
    form.value.relay_server_id = keepServer
    form.value.relay_host = keepHost
    fillRelayHost()
    ensureLandingDefault()
    showRelayEditor.value = false
    await load()
  } catch(e:any) { error.value = e.message || '创建失败' }
}

async function removeRelay(id:string) {
  if (!confirm('确认删除这条中转线路？')) return
  await api(`/api/relays/${id}`, { method:'DELETE' })
  message.value = '中转线路已删除。'
  if (selectedRelayId.value === id) selectedRelayId.value = ''
  await load()
}

function buildRelayVlessLink() {
  const r = selectedRelay.value
  const c = selectedClient.value
  if (!r || !c) return ''
  const n = selectedLanding.value
  const q = new URLSearchParams()
  q.set('encryption', 'none')
  q.set('type', 'tcp')
  q.set('security', 'reality')
  // V0.7.6.1: default relay QR/link should stay universal; do not force xudp.
  if (routeMode(r) === 'socks5_route') {
    q.set('sni', valueOr(r.relay_sni, 'www.intel.com'))
    q.set('fp', valueOr(r.relay_fingerprint, 'chrome'))
    if (r.relay_reality_public_key) q.set('pbk', r.relay_reality_public_key)
    if (r.relay_reality_short_id) q.set('sid', r.relay_reality_short_id)
    q.set('spx', valueOr(r.relay_reality_spider_x, '/'))
  } else {
    if (!n) return ''
    q.set('type', valueOr(n.transport, 'tcp'))
    q.set('security', valueOr(n.security, 'reality'))
    if (n.sni) q.set('sni', n.sni)
    if (String(n.security).toLowerCase() === 'reality') {
      q.set('fp', valueOr(n.fingerprint, 'chrome'))
      if (n.reality_public_key) q.set('pbk', n.reality_public_key)
      if (n.reality_short_id) q.set('sid', n.reality_short_id)
      q.set('spx', valueOr(n.reality_spider_x, '/'))
    }
  }
  const suffix = routeMode(r) === 'socks5_route' ? 'SOCKS5路由中转' : 'TCP透传中转'
  const label = encodeURIComponent(`${r.name}-${suffix}`)
  return `vless://${c.uuid}@${r.relay_host}:${Number(r.relay_port)}?${q.toString()}#${label}`
}

function showRelayDetail(r:any) { relayDetail.value = r }
function closeRelayDetail() { relayDetail.value = null }
function buildFirewallCommand(r:any) {
  if (!r) return ''
  return `ufw allow ${Number(r.relay_port)}/tcp && ufw reload`
}
function buildOutletFirewallCommandFromForm() {
  const relayIP = String(form.value.relay_host || selectedServer.value?.host || selectedServer.value?.ip || '').trim()
  const port = Number(form.value.manual_socks_port || 0)
  if (!relayIP || !port) return ''
  return `ufw allow from ${relayIP} to any port ${port} proto tcp && ufw reload`
}
async function copyRelayLink() {
  const link = buildRelayVlessLink()
  if (!link) { error.value = '请选择中转线路和客户。'; return }
  const ok = await copyText(link)
  message.value = ok ? '中转节点链接已复制。客户端连接中转服务器，SOCKS5 路由中转最终从落地 IP 出口访问。' : '复制失败，请手动复制下方链接。'
}
async function copyFirewall() {
  const cmd = buildFirewallCommand(selectedRelay.value)
  if (!cmd) { error.value = '请先选择中转线路。'; return }
  const ok = await copyText(cmd)
  message.value = ok ? '中转端口放行命令已复制。请在中转服务器和云安全组同时放行该 TCP 端口。' : '复制失败，请手动复制下方命令。'
}
async function copyOutletFirewall() {
  const cmd = buildOutletFirewallCommandFromForm()
  if (!cmd) { error.value = '请先填写中转 Host 和远程 SOCKS5 端口。'; return }
  const ok = await copyText(cmd)
  message.value = ok ? '出口 SOCKS5 端口放行命令已复制。请在出口服务器执行，并同步云安全组。' : '复制失败，请手动复制下方命令。'
}

onMounted(() => { loadSavedOutlets(); load() })
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">中转管理</h1>
      <p class="page-desc">V0.7.6.1 中转管理清理版：这里只作为线路运维视图，客户节点统一到客户管理里分享。</p>
    </div>
    <div class="head-actions"><button class="btn" @click="showRelayEditor = true">新增中转线路</button></div>
  </div>

  <div class="notice ok">推荐使用 SOCKS5 路由中转：客户连接中转服务器 VLESS Reality 入站，Xray 路由到落地 SOCKS5，平台看到落地服务器 IP。</div>
  <div class="notice warn">双服务器部署时，先在出口服务器创建 SOCKS5 入站，再在这里绑定远程 SOCKS5，并先测试连通性。</div>
  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>

  <div v-if="showRelayEditor" class="modal-mask" @click.self="showRelayEditor = false">
    <div class="modal-card relay-editor-modal">
      <div class="modal-head">
        <div>
          <span class="eyebrow">中转配置</span>
          <h2>新增中转线路</h2>
          <p>按 标准 SOCKS5 路由中转逻辑配置：中转入站 → SOCKS5 出站 → 路由绑定。普通测试只需要填写远程 SOCKS5 和中转端口。</p>
        </div>
        <button class="icon-btn" @click="showRelayEditor = false">×</button>
      </div>

      <div class="relay-step"><strong>1. 选择中转模式</strong><span>推荐 SOCKS5 路由中转；旧 TCP 透传保留给已测通的 Reality 落地线路。</span></div>
      <div class="form grid-3 compact-form">
        <label><span>中转类型</span><select v-model="form.route_mode"><option value="socks5_route">SOCKS5 路由中转（推荐）</option><option value="tcp_forward">TCP 透传中转（旧稳定模式）</option></select></label>
        <label><span>中转名称</span><input v-model="form.name" placeholder="例如：154中转-128出口测试" /></label>
        <label><span>中转服务器</span><select v-model="form.relay_server_id"><option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name || s.host || s.ip }}</option></select></label>
      </div>

      <div class="relay-step"><strong>2. 配置落地出口</strong><span>双服务器部署时，选择手动填写远程 SOCKS5。</span></div>
      <div class="form grid-3 compact-form">
        <label v-if="form.route_mode === 'socks5_route'"><span>SOCKS5 落地方式</span><select v-model="form.landing_mode"><option value="manual_socks5">手动填写远程 SOCKS5（双服务器）</option><option value="panel_node">选择本面板 SOCKS5 入站（主控/Agent模式）</option></select></label>
        <label v-if="form.route_mode === 'tcp_forward'"><span>落地 Reality 节点</span><select v-model="form.landing_node_id"><option v-for="n in landingNodes" :key="n.id" :value="n.id">{{ n.name }} / {{ n.host }}:{{ n.port }}</option></select></label>
        <label v-if="formUsesPanelSocks"><span>落地 SOCKS5 入站</span><select v-model="form.landing_node_id"><option v-for="n in socksNodes" :key="n.id" :value="n.id">{{ n.name }} / {{ n.host }}:{{ n.port }}</option></select></label>
        <label v-if="formUsesManualSocks"><span>常用落地出口库</span><select v-model="selectedSavedOutletId" @change="applySelectedOutlet"><option value="">选择已保存出口</option><option v-for="o in savedOutlets" :key="o.id" :value="o.id">{{ o.name }} / {{ o.username }}</option></select></label>

        <label v-if="formUsesManualSocks"><span>远程 SOCKS5 地址</span><input v-model="form.manual_socks_host" placeholder="例如：203.0.113.10" /></label>
        <label v-if="formUsesManualSocks"><span>远程 SOCKS5 端口</span><input v-model.number="form.manual_socks_port" type="number" placeholder="例如：33668" /></label>
        <label v-if="formUsesManualSocks"><span>远程 SOCKS5 账号</span><input v-model="form.manual_socks_username" placeholder="出口面板生成的账号" /></label>
        <label v-if="formUsesManualSocks"><span>远程 SOCKS5 密码</span><input v-model="form.manual_socks_password" placeholder="出口面板生成的密码" /></label>
        <label v-if="formUsesManualSocks"><span>远程 SOCKS5 UDP</span><select v-model="form.manual_socks_udp"><option :value="false">关闭（推荐先测 TCP）</option><option :value="true">开启</option></select></label>
      </div>

      <div class="relay-step"><strong>3. 配置客户连接入口</strong><span>客户连接中转 Host 和中转端口，出口检测应显示远程 SOCKS5 所在服务器。</span></div>
      <div class="form grid-3 compact-form">
        <label><span>中转 Host</span><input v-model="form.relay_host" placeholder="中转服务器公网 IP 或域名" /></label>
        <label><span>中转端口</span><input v-model.number="form.relay_port" type="number" /></label>
        <label v-if="form.route_mode === 'tcp_forward'"><span>中转协议</span><select v-model="form.relay_network"><option value="tcp">TCP 稳定模式（推荐）</option><option value="tcp,udp">TCP + UDP 实验模式（可能变慢）</option><option value="udp" :disabled="formIsVlessRealityTCP">UDP-only（仅原生 UDP 落地）</option></select></label>
        <label v-if="form.route_mode === 'socks5_route'"><span>Reality 伪装目标</span><input v-model="form.relay_reality_dest" placeholder="www.intel.com:443" /></label>
        <label v-if="form.route_mode === 'socks5_route'"><span>SNI</span><input v-model="form.relay_sni" placeholder="www.intel.com" /></label>
        <label v-if="form.route_mode === 'socks5_route'"><span>Fingerprint</span><input v-model="form.relay_fingerprint" placeholder="chrome" /></label>
        <label><span>备注</span><input v-model="form.remark" placeholder="例如：给客户扫码使用" /></label>
      </div>

      <div class="notice ok" v-if="formUsesManualSocks && testResult && testResult.ok">远程 SOCKS5 测试通过：出口 IP {{ testResult.exit_ip }}，目标 {{ testResult.target }}，耗时 {{ testResult.latency_ms }}ms。</div>
      <div class="notice warn" v-if="formUsesManualSocks && testResult && !testResult.ok">远程 SOCKS5 测试未通过：{{ testResult.message }}</div>
      <div class="notice ok" v-if="form.route_mode === 'socks5_route'">创建后系统会在当前中转服务器生成 VLESS Reality 入站、SOCKS5 出站和路由绑定，无需手动配置入站/出站关联。</div>
      <div class="notice warn" v-if="formUsesManualSocks">请确保出口服务器防火墙/安全组已允许当前中转 IP 访问远程 SOCKS5 端口。</div>
      <div class="notice warn code" v-if="formUsesManualSocks && buildOutletFirewallCommandFromForm()">{{ buildOutletFirewallCommandFromForm() }}</div>
      <div class="notice warn" v-if="formUsesPanelSocks && !socksNodes.length">当前面板还没有 SOCKS5 落地入站；双服务器部署请切换为“手动填写远程 SOCKS5”。</div>
      <div class="notice warn" v-if="form.route_mode === 'tcp_forward' && formIsVlessRealityTCP && form.relay_network === 'tcp,udp'">当前落地节点为 VLESS Reality TCP，TCP+UDP 可能导致连接变慢，正式使用建议改回 TCP。</div>
      <div class="notice warn" v-if="form.route_mode === 'tcp_forward' && formIsVlessRealityTCP && form.relay_network === 'udp'">当前落地节点为 VLESS Reality TCP，不支持 UDP-only 中转。</div>
      <div class="modal-actions relay-modal-actions">
        <button class="btn secondary" v-if="formUsesManualSocks" :disabled="testingSocks" @click="testManualSocks">{{ testingSocks ? '测试中...' : '测试远程 SOCKS5' }}</button>
        <button class="btn secondary" v-if="formUsesManualSocks" @click="saveCurrentOutlet">保存到落地出口库</button>
        <button class="btn secondary" v-if="formUsesManualSocks && savedOutlets.length" @click="removeSavedOutlet">删除所选出口</button>
        <button class="btn secondary" v-if="formUsesManualSocks" @click="copyOutletFirewall">复制出口端口放行命令</button>
        <button class="btn" @click="createRelay">新增中转线路</button>
      </div>
    </div>
  </div>

  <div class="card">
    <h2>中转线路列表</h2>
    <table>
      <thead><tr><th>名称</th><th>类型</th><th>链路</th><th>协议</th><th>落地出口</th><th>状态</th><th>备注</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="r in relays" :key="r.id">
          <td><strong>{{ r.name }}</strong><br><span class="muted">{{ serverName(r.relay_server_id) }}</span></td>
          <td><span class="badge" :class="routeMode(r) === 'socks5_route' ? 'online' : ''">{{ routeMode(r) === 'socks5_route' ? 'SOCKS5路由' : 'TCP透传' }}</span></td>
          <td><span class="code">{{ relayFlowSummary(r) }}</span></td>
          <td><span class="badge" :class="String(r.relay_network || 'tcp') === 'tcp' ? 'online' : ''">{{ r.relay_network || 'tcp' }}</span><br><span class="muted" v-if="String(r.relay_network || 'tcp') === 'tcp,udp'">实验模式</span></td>
          <td>{{ landingLabel(r) }}</td>
          <td><span class="badge" :class="r.enabled ? 'online' : ''">{{ r.enabled ? '启用' : '停用' }}</span></td>
          <td>{{ r.remark || '-' }}</td>
          <td class="row-actions"><button class="btn secondary" @click="showRelayDetail(r)">查看链路</button><button class="btn secondary" @click="selectedRelayId = r.id; copyFirewall()">复制放行命令</button><button class="btn danger" @click="removeRelay(r.id)">删除</button></td>
        </tr>
        <tr v-if="!relays.length"><td colspan="8" class="muted">暂无中转线路。先创建一条中转线路。</td></tr>
      </tbody>
    </table>
  </div>

  <div v-if="relayDetail" class="modal-mask" @click.self="closeRelayDetail">
    <div class="modal-card relay-detail-modal">
      <div class="modal-head">
        <div><span class="eyebrow">中转链路</span><h2>{{ relayDetail.name }}</h2><p>这里只展示线路关系。客户节点请到“客户管理 → 分享”复制，避免选错客户或出口。</p></div>
        <button class="icon-btn" @click="closeRelayDetail">×</button>
      </div>
      <div class="relay-flow-view">
        <div class="notice ok">客户连接入口：{{ relayDetail.relay_host }}:{{ relayDetail.relay_port }}</div>
        <div class="arrow">→</div>
        <div class="notice ok">固定出口：{{ socksTargetLabel(relayDetail) }}</div>
      </div>
      <div class="detail-grid">
        <div><span>线路类型</span><strong>{{ routeMode(relayDetail) === 'socks5_route' ? 'SOCKS5 路由中转' : 'TCP 透传中转' }}</strong></div>
        <div><span>中转服务器</span><strong>{{ serverName(relayDetail.relay_server_id) }}</strong></div>
        <div><span>协议</span><strong>{{ relayDetail.relay_network || 'tcp' }}</strong></div>
        <div><span>状态</span><strong>{{ relayDetail.enabled ? '启用' : '停用' }}</strong></div>
      </div>
      <div class="share-config-box">
        <div class="share-config-head"><strong>中转入口端口放行命令</strong><button class="btn secondary" @click="selectedRelayId = relayDetail.id; copyFirewall()">复制命令</button></div>
        <pre class="share-pre">{{ buildFirewallCommand(relayDetail) }}</pre>
      </div>
      <div class="modal-actions"><button class="btn secondary" @click="closeRelayDetail">关闭</button></div>
    </div>
  </div>

  <div class="notice ok" v-if="socks5RouteRelays.length">已创建 {{ socks5RouteRelays.length }} 条 SOCKS5 路由中转线路。</div>
  <div class="notice warn" v-if="tcpUdpRelays.length">检测到 TCP+UDP 实验线路：{{ tcpUdpRelayNames }}。VLESS Reality 正式使用建议改为 TCP。</div>
</template>
