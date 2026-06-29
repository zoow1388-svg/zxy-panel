<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'
import { buildClientMultiShare, buildShortNodeUrl, buildSubscriptionUrl, isClientShareNode, isQrShareFormat, nodesForClient, qrImageUrl, relayNodesForClient, shareFormatLabel, shareFormatOptions, type ShareFormat } from '../share'

type ShareTab = ShareFormat | 'subscription'

const clients = ref<any[]>([])
const nodes = ref<any[]>([])
const relays = ref<any[]>([])
const exits = ref<any[]>([])
const servers = ref<any[]>([])
const networkPolicy = ref<any>({})
const error = ref('')
const message = ref('')
const editingId = ref('')
const detailClient = ref<any | null>(null)
const shareClient = ref<any | null>(null)
const shareTab = ref<ShareTab>('v2rayn')
const shareText = ref('')
const shareLinks = ref<any[]>([])
const qrZoomText = ref('')
const qrZoomTitle = ref('')
const copyToast = ref('')
let copyToastTimer: any = null
const form = ref<any>({ username:'', email:'', traffic_limit_gb:100, expire_at:'', enabled:true, node_ids:[], relay_route_ids:[] })
const showAdvancedClientForm = ref(false)
const showFixedClientModal = ref(false)
const clientCreateMode = ref<'direct' | 'fixed'>('fixed')
const fixedExpireMode = ref<'long' | 'custom'>('long')
const directExpireMode = ref<'long' | 'custom'>('long')
const fixedForm = ref<any>({ username:'', email:'', traffic_limit_gb:100, expire_at:'', relay_server_id:'', relay_host:'', relay_port:randomRelayPort(), landing_exit_id:'', route_name:'', relay_reality_dest:'www.intel.com:443', relay_sni:'www.intel.com', relay_fingerprint:'chrome', remark:'' })
const directForm = ref<any>({ username:'', email:'', traffic_limit_gb:100, expire_at:'', node_id:'', enabled:true })

const clientStats = computed(() => {
  const total = clients.value.length
  const enabled = clients.value.filter((c:any) => c.enabled !== false).length
  const disabled = total - enabled
  const linked = clients.value.filter((c:any) => (Array.isArray(c.node_ids) && c.node_ids.length > 0) || (Array.isArray(c.relay_route_ids) && c.relay_route_ids.length > 0)).length
  return { total, enabled, disabled, linked }
})
const shareTabs = computed(() => [
  ...shareFormatOptions,
  { value: 'subscription' as ShareTab, label: '订阅', tip: '通用订阅地址，适合客户端批量导入或后续统一更新。' },
])
const shareTip = computed(() => shareTabs.value.find(x => x.value === shareTab.value)?.tip || '')
const shareTitle = computed(() => shareClient.value ? `${shareClient.value.username || '客户'} 的分享配置` : '')
const clientBindableNodes = computed(() => nodes.value.filter(isClientShareNode))
const clientBindableRelays = computed(() => relays.value.filter((r:any) => r.enabled !== false && String(r.route_mode || '') === 'socks5_route'))
const shareAvailableNodes = computed(() => shareClient.value ? [...nodesForClient(shareClient.value, nodes.value), ...relayNodesForClient(shareClient.value, relays.value)] : [])

function normalizeApiError(e:any) {
  try { const data = JSON.parse(e.message); return data.error || e.message } catch { return e?.message || '操作失败' }
}
function showCopyToast(text:string) {
  copyToast.value = text
  if (copyToastTimer) clearTimeout(copyToastTimer)
  copyToastTimer = setTimeout(() => { copyToast.value = '' }, 2200)
}

async function load() {
  clients.value = await api('/api/clients')
  nodes.value = await api('/api/nodes')
  relays.value = await api('/api/relays')
  exits.value = await api('/api/landing-exits')
  servers.value = await api('/api/servers')
  try { networkPolicy.value = (await api('/api/network-policy')).policy || {} } catch { networkPolicy.value = {} }
  if (!fixedForm.value.relay_server_id && servers.value[0]) fixedForm.value.relay_server_id = servers.value[0].id
  if (!fixedForm.value.landing_exit_id && exits.value[0]) fixedForm.value.landing_exit_id = exits.value[0].id
  fillFixedRelayHost()
  if (shareClient.value) {
    const latest = clients.value.find((c:any) => c.id === shareClient.value.id)
    if (latest) shareClient.value = latest
    rebuildClientShare()
  }
  if (detailClient.value) {
    const latest = clients.value.find((c:any) => c.id === detailClient.value.id)
    if (latest) detailClient.value = latest
  }
}

function resetForm() {
  editingId.value = ''
  form.value = { username:'', email:'', traffic_limit_gb:100, expire_at:'', enabled:true, node_ids:[], relay_route_ids:[] }
}

function toLocalInput(v:string) {
  if (!v) return ''
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n:number) => String(n).padStart(2,'0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fmtTime(v:string) {
  if (!v) return '长期可用'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  if (d.getFullYear() <= 1) return '长期可用'
  return d.toLocaleString()
}

function editClient(c:any) {
  editingId.value = c.id
  form.value = {
    username: c.username || '',
    email: c.email || '',
    traffic_limit_gb: c.traffic_limit_gb || 0,
    expire_at: toLocalInput(c.expire_at),
    enabled: c.enabled !== false,
    node_ids: Array.isArray(c.node_ids) ? [...c.node_ids] : [],
    relay_route_ids: Array.isArray(c.relay_route_ids) ? [...c.relay_route_ids] : [],
    uuid: c.uuid,
    subscribe_token: c.subscribe_token,
  }
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function randomRelayPort() { return Math.floor(10000 + Math.random() * 50000) }
function fillFixedRelayHost() {
  const s = servers.value.find((x:any)=>x.id === fixedForm.value.relay_server_id)
  if (s && !fixedForm.value.relay_host) fixedForm.value.relay_host = s.host || s.ip || ''
}
function resetFixedForm() {
  const serverId = fixedForm.value.relay_server_id || servers.value[0]?.id || ''
  fixedExpireMode.value = 'long'
  fixedForm.value = { username:'', email:'', traffic_limit_gb:100, expire_at:'', relay_server_id:serverId, relay_host:'', relay_port:randomRelayPort(), landing_exit_id:exits.value[0]?.id || '', route_name:'', relay_reality_dest:'www.intel.com:443', relay_sni:'www.intel.com', relay_fingerprint:'chrome', remark:'' }
  fillFixedRelayHost()
}
function resetDirectForm() {
  directExpireMode.value = 'long'
  directForm.value = { username:'', email:'', traffic_limit_gb:100, expire_at:'', node_id: clientBindableNodes.value[0]?.id || '', enabled:true }
}
function openFixedClientModal(mode: 'direct' | 'fixed' = 'fixed') {
  clientCreateMode.value = mode
  resetFixedForm()
  resetDirectForm()
  showFixedClientModal.value = true
}
function closeFixedClientModal() {
  showFixedClientModal.value = false
}
async function createDirectClient() {
  error.value=''; message.value=''
  if (!String(directForm.value.username || '').trim()) { error.value='请填写客户名称。'; return }
  if (!directForm.value.node_id) { error.value='请选择直连入站。请先到入站管理创建 VLESS Reality 入站。'; return }
  try {
    const body={
      username: directForm.value.username,
      email: directForm.value.email,
      traffic_limit_gb:Number(directForm.value.traffic_limit_gb || 100),
      expire_at: directExpireMode.value === 'custom' && directForm.value.expire_at ? new Date(directForm.value.expire_at).toISOString() : undefined,
      enabled:true,
      node_ids:[directForm.value.node_id],
      relay_route_ids:[]
    }
    const res = await api('/api/clients', {method:'POST', body:JSON.stringify(body)})
    const node = nodes.value.find((n:any)=>n.id === directForm.value.node_id)
    message.value = `直连客户已创建：${res.username}，入口 ${node ? node.host + ':' + node.port : '已绑定直连入站'}，出口为当前服务器。`
    showFixedClientModal.value = false
    resetDirectForm()
    await load()
  } catch(e:any){ error.value=normalizeApiError(e) }
}
async function createFixedExitClient() {
  error.value=''; message.value=''
  if (!String(fixedForm.value.username || '').trim()) { error.value='请填写客户名称。'; return }
  if (!fixedForm.value.relay_server_id) { error.value='请选择中转服务器。'; return }
  if (!fixedForm.value.landing_exit_id) { error.value='请选择落地出口。'; return }
  const port = Number(fixedForm.value.relay_port)
  if (!port || port < 10000 || port > 60000) { error.value='中转端口建议使用 10000-60000。'; return }
  try {
    const body={...fixedForm.value, relay_port:port, traffic_limit_gb:Number(fixedForm.value.traffic_limit_gb || 100), expire_at: fixedExpireMode.value === 'custom' && fixedForm.value.expire_at ? new Date(fixedForm.value.expire_at).toISOString() : undefined}
    const res = await api('/api/clients/create-socks5-relay', {method:'POST', body:JSON.stringify(body)})
    message.value = `固定出口客户已创建：${res.client.username}，入口 ${res.relay.relay_host}:${res.relay.relay_port} → 出口 ${res.exit.host}:${res.exit.port}。该客户固定绑定这一条线路，不会随机切换出口。`
    showFixedClientModal.value = false
    resetFixedForm()
    await load()
  } catch(e:any){ error.value=normalizeApiError(e) }
}

async function saveClient() {
  error.value=''; message.value=''
  try {
    const body={...form.value, expire_at: form.value.expire_at ? new Date(form.value.expire_at).toISOString() : undefined}
    if (editingId.value) {
      await api(`/api/clients/${editingId.value}`,{method:'PUT',body:JSON.stringify(body)})
      message.value='客户已保存。Agent 会在下一次同步时更新 Xray clients。'
    } else {
      await api('/api/clients',{method:'POST',body:JSON.stringify(body)})
      message.value='客户已新增。请在客户列表点击“分享”获取二维码和客户端配置。'
    }
    resetForm()
    await load()
  } catch(e:any){ error.value=normalizeApiError(e) }
}
async function remove(id:string) { if(!confirm('确认删除这个客户？')) return; await api(`/api/clients/${id}`,{method:'DELETE'}); await load() }
async function reset(id:string) { if(!confirm('确认重置订阅？旧订阅链接会失效。')) return; await api(`/api/clients/${id}/reset-token`,{method:'POST'}); await load() }
function subUrl(c:any) { return buildSubscriptionUrl(c) }
async function copySub(c:any) {
  const ok = await copyText(subUrl(c))
  message.value = ok ? '完整订阅链接已复制。' : '浏览器禁止自动复制，请点击“分享”后手动复制订阅链接。'
  showCopyToast(message.value)
}
function showDetail(c:any) { detailClient.value = c }
function closeDetail() { detailClient.value = null }
function showShare(c:any, tab: ShareTab = 'v2rayn') {
  shareClient.value = c
  shareTab.value = tab
  rebuildClientShare()
}
function closeShare() {
  shareClient.value = null
  shareText.value = ''
  shareLinks.value = []
  shareTab.value = 'v2rayn'
}
function openQrZoom(text:string, title='二维码扫码') {
  qrZoomText.value = text || ''
  qrZoomTitle.value = title
}
function closeQrZoom() {
  qrZoomText.value = ''
  qrZoomTitle.value = ''
}
function rebuildClientShare() {
  const c = shareClient.value
  if (!c) return
  const available = [...nodesForClient(c, nodes.value), ...relayNodesForClient(c, relays.value)]
  if (shareTab.value === 'subscription') {
    shareText.value = subUrl(c)
    shareLinks.value = []
    return
  }
  shareText.value = buildClientMultiShare(available, c, shareTab.value as ShareFormat, networkPolicy.value)
  shareLinks.value = shareTab.value === 'v2rayn' || shareTab.value === 'shadowrocket'
    ? available.map((n:any) => ({ node:n, link: buildClientMultiShare([n], c, shareTab.value as ShareFormat, networkPolicy.value) }))
    : []
}
watch(shareTab, () => rebuildClientShare())
watch(() => fixedForm.value.relay_server_id, () => { fixedForm.value.relay_host = ""; fillFixedRelayHost() })
async function copyAny(text:string, label='内容') {
  const ok = await copyText(text)
  message.value = ok ? `${label}已复制。` : `浏览器禁止自动复制，请手动复制弹窗里的完整${label}。`
  showCopyToast(message.value)
}
function nodeNames(ids:string[]) {
  if (!ids || ids.length === 0) return '未绑定入站'
  return ids.map(id => nodes.value.find((n:any)=>n.id===id)?.name || id).join('、')
}
function relayNames(ids:string[]) {
  if (!ids || ids.length === 0) return '未绑定固定出口'
  return ids.map(id => relays.value.find((r:any)=>r.id===id)?.name || id).join('、')
}
function relayForClient(c:any) {
  const ids = Array.isArray(c.relay_route_ids) ? c.relay_route_ids : []
  if (!ids.length) return null
  return relays.value.find((r:any)=>r.id === ids[0]) || null
}
function relayEntryExitText(c:any) {
  const r = relayForClient(c)
  if (!r) return ''
  const exit = r.manual_socks_host || r.landing_node_id || '未知出口'
  const port = r.manual_socks_port ? `:${r.manual_socks_port}` : ''
  return `入口 ${r.relay_host}:${r.relay_port} → 出口 ${exit}${port}`
}
function clientBindSummary(c:any) {
  const normal = Array.isArray(c.node_ids) && c.node_ids.length ? nodeNames(c.node_ids) : ''
  const fixed = Array.isArray(c.relay_route_ids) && c.relay_route_ids.length ? relayNames(c.relay_route_ids) : ''
  return [normal, fixed].filter(Boolean).join('；') || '未指定'
}
function clientPrimaryNode(c:any) {
  const list = nodesForClient(c, nodes.value)
  return list[0]
}
function statusText(c:any) { return c.enabled === false ? '停用' : '启用' }
function trafficText(c:any) { return `${c.traffic_used_gb || 0} / ${c.traffic_limit_gb || 0} GB` }
function setShareTab(v: ShareTab) { shareTab.value = v }
function setAdvancedRelayRoute(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  form.value.relay_route_ids = value ? [value] : []
}
function shareHeading() {
  if (shareTab.value === 'subscription') return '通用订阅链接'
  return `${shareFormatLabel(shareTab.value)} 配置`
}
function shareCopyLabel() {
  if (shareTab.value === 'subscription') return '订阅链接'
  return shareFormatLabel(shareTab.value as ShareFormat)
}
function showQrForMainShare() {
  return shareTab.value === 'subscription' || isQrShareFormat(shareTab.value as ShareFormat)
}
function qrLabel() {
  if (shareTab.value === 'subscription') return '订阅二维码'
  return `${shareFormatLabel(shareTab.value as ShareFormat)} 二维码`
}
function qrTextForNode(item:any) {
  if (!item) return ''
  if (shareTab.value === 'v2rayn' && shareClient.value) return buildShortNodeUrl(shareClient.value, item.node)
  return item.link || ''
}
function mainQrText() {
  if (shareTab.value === 'subscription') return shareText.value
  if (shareLinks.value.length) return qrTextForNode(shareLinks.value[0])
  return shareText.value
}
function qrModeHint() {
  if (shareTab.value === 'v2rayn') return 'V2rayN 使用短链接二维码，降低二维码密度；复制按钮仍复制完整节点链接。'
  if (shareTab.value === 'subscription') return '订阅二维码用于批量导入和后续统一更新。'
  return '当前二维码为单节点配置二维码。'
}
onMounted(load)
</script>
<template>
  <div v-if="copyToast" class="copy-toast">{{ copyToast }}</div>
  <div class="page-head"><div><h1 class="page-title">客户管理</h1><p class="page-desc">V0.7.5.8.1 客户管理 UI 清理版：固定出口客户通过弹窗创建，客户入口与出口关系更清晰。</p></div></div>

  <div class="client-summary-grid">
    <div class="client-summary-card"><span>客户总数</span><strong>{{ clientStats.total }}</strong></div>
    <div class="client-summary-card"><span>启用客户</span><strong>{{ clientStats.enabled }}</strong></div>
    <div class="client-summary-card"><span>绑定线路</span><strong>{{ clientStats.linked }}</strong></div>
    <div class="client-summary-card"><span>停用客户</span><strong>{{ clientStats.disabled }}</strong></div>
  </div>

  <div class="panel fixed-client-entry-panel">
    <div class="section-head">
      <div>
        <h2>客户创建</h2>
        <p>支持直连客户和固定出口客户。直连客户直接使用本机入站；固定出口客户绑定一个中转入口和一个落地出口。</p>
      </div>
      <div class="inline-actions"><button class="btn secondary" @click="openFixedClientModal('direct')">新增直连客户</button><button class="btn" @click="openFixedClientModal('fixed')">新增固定出口客户</button></div>
    </div>
    <div class="fixed-exit-preview compact-preview">
      <div class="notice ok">标准链路：客户连接中转入口</div>
      <div class="arrow">→</div>
      <div class="notice ok">平台看到固定落地出口 IP</div>
    </div>
  </div>

  <div v-if="showFixedClientModal" class="modal-mask" @click.self="closeFixedClientModal">
    <div class="modal-card fixed-client-modal">
      <div class="modal-head">
        <div>
          <span class="eyebrow">新增客户</span>
          <h2>{{ clientCreateMode === 'direct' ? '新增直连客户' : '新增固定出口客户' }}</h2>
          <p>{{ clientCreateMode === 'direct' ? '直连客户直接使用本机入站，出口 IP 为当前服务器。' : '固定出口客户连接中转入口，出口 IP 固定为所选落地出口。' }}</p>
        </div>
        <button class="icon-btn" @click="closeFixedClientModal">×</button>
      </div>
      <div class="mode-tabs">
        <button :class="['mode-tab', {active: clientCreateMode === 'direct'}]" @click="clientCreateMode='direct'">直连客户<span>本机入站 → 本机出口</span></button>
        <button :class="['mode-tab', {active: clientCreateMode === 'fixed'}]" @click="clientCreateMode='fixed'">固定出口客户<span>中转入口 → 落地出口</span></button>
      </div>

      <template v-if="clientCreateMode === 'direct'">
        <div class="relay-step"><strong>1. 客户信息</strong><span>到期时间不填时，默认长期可用。</span></div>
        <div class="form grid-2 compact-form">
          <label><span>客户名</span><input v-model="directForm.username" placeholder="客户A" /></label>
          <label><span>邮箱</span><input v-model="directForm.email" placeholder="可空" /></label>
          <label><span>流量 GB</span><input v-model.number="directForm.traffic_limit_gb" type="number" /></label>
          <label><span>到期设置</span><select v-model="directExpireMode"><option value="long">长期可用</option><option value="custom">指定到期时间</option></select><em class="field-tip">不设置到期时间时，客户默认长期可用。</em></label>
          <label v-if="directExpireMode === 'custom'" class="wide"><span>到期时间</span><input v-model="directForm.expire_at" type="datetime-local" /></label>
        </div>
        <div class="relay-step"><strong>2. 选择直连入站</strong><span>客户连接本机入站，出口 IP 为当前服务器 IP。</span></div>
        <div class="form compact-form">
          <label><span>直连入站</span><select v-model="directForm.node_id"><option value="">请选择直连入站</option><option v-for="n in clientBindableNodes" :key="n.id" :value="n.id">{{ n.name }} / {{ n.host }}:{{ n.port }}</option></select><em class="field-tip">如果没有可选入站，请先到“入站管理”创建 VLESS Reality 入站。</em></label>
        </div>
        <div class="fixed-exit-preview" v-if="directForm.node_id">
          <div class="notice ok">客户连接入口：{{ nodes.find((n:any)=>n.id===directForm.node_id)?.host }}:{{ nodes.find((n:any)=>n.id===directForm.node_id)?.port }}</div>
          <div class="arrow">→</div>
          <div class="notice ok">出口：当前服务器</div>
        </div>
        <div class="modal-actions"><button class="btn" @click="createDirectClient">创建直连客户</button><button class="btn secondary" @click="resetDirectForm">重置</button><button class="btn secondary" @click="closeFixedClientModal">关闭</button></div>
      </template>

      <template v-else>
      <div class="relay-step"><strong>1. 客户信息</strong><span>到期时间不填时，默认长期可用。</span></div>
      <div class="form grid-2 compact-form">
        <label><span>客户名</span><input v-model="fixedForm.username" placeholder="客户A" /></label>
        <label><span>邮箱</span><input v-model="fixedForm.email" placeholder="可空" /></label>
        <label><span>流量 GB</span><input v-model.number="fixedForm.traffic_limit_gb" type="number" /></label>
        <label><span>到期设置</span><select v-model="fixedExpireMode"><option value="long">长期可用</option><option value="custom">指定到期时间</option></select><em class="field-tip">不设置到期时间时，客户默认长期可用。</em></label>
        <label v-if="fixedExpireMode === 'custom'" class="wide"><span>到期时间</span><input v-model="fixedForm.expire_at" type="datetime-local" /></label>
      </div>

      <div class="relay-step"><strong>2. 线路选择</strong><span>每个固定出口客户只绑定一个落地出口，避免出口 IP 乱跳。</span></div>
      <div class="form grid-2 compact-form">
        <label><span>中转服务器</span><select v-model="fixedForm.relay_server_id"><option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name || s.host || s.ip }} / {{ s.host || s.ip }}</option></select></label>
        <label><span>中转入口 Host</span><input v-model="fixedForm.relay_host" placeholder="中转服务器公网 IP" /></label>
        <label><span>中转入口端口</span><input v-model.number="fixedForm.relay_port" type="number" /></label>
        <label><span>落地出口</span><select v-model="fixedForm.landing_exit_id"><option value="">请选择落地出口</option><option v-for="e in exits" :key="e.id" :value="e.id">{{ e.name }} / {{ e.host }}:{{ e.port }}</option></select></label>
      </div>

      <div class="relay-step"><strong>3. Reality 参数</strong><span>默认参数适合大多数网页、AI 和海外业务访问场景。</span></div>
      <div class="form grid-2 compact-form">
        <label><span>线路名称</span><input v-model="fixedForm.route_name" placeholder="可空，默认客户名-出口IP" /></label>
        <label><span>Reality 伪装目标</span><input v-model="fixedForm.relay_reality_dest" /></label>
        <label><span>SNI</span><input v-model="fixedForm.relay_sni" /></label>
        <label><span>Fingerprint</span><input v-model="fixedForm.relay_fingerprint" /></label>
      </div>

      <div class="fixed-exit-preview" v-if="fixedForm.landing_exit_id">
        <div class="notice ok">客户连接入口：{{ fixedForm.relay_host || '中转IP' }}:{{ fixedForm.relay_port }}</div>
        <div class="arrow">→</div>
        <div class="notice ok">固定出口：{{ exits.find((e:any)=>e.id===fixedForm.landing_exit_id)?.host || '出口IP' }}</div>
      </div>
      <div class="modal-actions"><button class="btn" @click="createFixedExitClient">创建固定出口客户</button><button class="btn secondary" @click="resetFixedForm">重置</button><button class="btn secondary" @click="closeFixedClientModal">关闭</button></div>
      </template>
    </div>
  </div>

  <div class="advanced-client-toggle"><button class="btn secondary" @click="showAdvancedClientForm = !showAdvancedClientForm">{{ showAdvancedClientForm ? '隐藏高级手动客户' : '高级手动客户' }}</button><span>仅供运维手动绑定直连入站或已有线路；普通用户请使用“新增固定出口客户”。</span></div>
  <div v-if="showAdvancedClientForm" class="form client-form compact-client-form advanced-client-form">
    <label class="field"><span>客户名</span><input v-model="form.username" placeholder="客户名" /></label>
    <label class="field"><span>邮箱</span><input v-model="form.email" placeholder="邮箱，可空" /></label>
    <label class="field"><span>流量 GB</span><input v-model.number="form.traffic_limit_gb" placeholder="流量 GB" /></label>
    <label class="field"><span>到期时间</span><input v-model="form.expire_at" type="datetime-local" /></label>
    <label class="field"><span>状态</span><select v-model="form.enabled"><option :value="true">启用</option><option :value="false">停用</option></select></label>
    <label class="field wide"><span>关联直连入站</span><select v-model="form.node_ids" multiple class="wide"><option v-for="n in clientBindableNodes" :value="n.id" :key="n.id">{{ n.name }}｜{{ n.host }}:{{ n.port }}</option></select></label>
    <label class="field wide"><span>绑定固定出口中转线路（只能选一条）</span><select :value="form.relay_route_ids?.[0] || ''" @change="setAdvancedRelayRoute" class="wide"><option value="">不绑定固定出口</option><option v-for="r in clientBindableRelays" :value="r.id" :key="r.id">{{ r.name }}｜入口 {{ r.relay_host }}:{{ r.relay_port }} → 出口 {{ r.manual_socks_host || r.landing_node_id }}</option></select></label>
    <div class="actions"><button class="btn" @click="saveClient">{{ editingId ? '保存客户' : '新增客户' }}</button><button v-if="editingId" class="btn secondary" @click="resetForm">取消编辑</button></div>
  </div>
  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>

  <div class="panel client-table-panel">
    <div class="section-head">
      <div><h2>客户列表</h2><p>分享、详情、编辑等操作集中在每一行，避免页面下方堆叠二维码和长配置。</p></div>
      <button class="btn secondary" @click="load">刷新</button>
    </div>
    <div class="table-wrap">
      <table class="client-table">
        <thead><tr><th>客户</th><th>关联入站</th><th>流量</th><th>到期时间</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="c in clients" :key="c.id">
            <td><strong>{{ c.username }}</strong><br><span class="muted">{{ c.email || '未填写邮箱' }}</span><br><span class="code muted">{{ c.uuid }}</span></td>
            <td>{{ clientBindSummary(c) }}<br><span v-if="relayEntryExitText(c)" class="muted fixed-route-line">{{ relayEntryExitText(c) }}</span><span v-else-if="clientPrimaryNode(c)" class="muted">直连入口：{{ clientPrimaryNode(c).host }}:{{ clientPrimaryNode(c).port }}</span></td>
            <td>{{ trafficText(c) }}</td>
            <td>{{ fmtTime(c.expire_at) }}</td>
            <td><span class="badge" :class="c.enabled?'online':''">{{ statusText(c) }}</span></td>
            <td class="row-actions client-row-actions">
              <button class="btn secondary" @click="showDetail(c)">详情</button>
              <button class="btn" @click="showShare(c)">分享</button>
              <button class="btn secondary" @click="copySub(c)">复制订阅</button>
              <button class="btn secondary" @click="editClient(c)">编辑</button>
              <button class="btn secondary" @click="reset(c.id)">重置</button>
              <button class="btn danger" @click="remove(c.id)">删除</button>
            </td>
          </tr>
          <tr v-if="!clients.length"><td colspan="6"><div class="empty-state">暂无客户。先创建客户，再点击“分享”获取二维码和配置。</div></td></tr>
        </tbody>
      </table>
    </div>
  </div>

  <div v-if="detailClient" class="modal-mask" @click.self="closeDetail">
    <div class="modal-card client-detail-modal">
      <div class="modal-head"><div><span class="eyebrow">客户详情</span><h2>客户端详情 — {{ detailClient.username }}</h2></div><button class="icon-btn" @click="closeDetail">×</button></div>
      <div class="detail-grid">
        <div><span>状态</span><strong><span class="badge" :class="detailClient.enabled?'online':''">{{ statusText(detailClient) }}</span></strong></div>
        <div><span>邮箱</span><strong>{{ detailClient.email || '未填写' }}</strong></div>
        <div><span>UUID</span><strong class="code">{{ detailClient.uuid }}</strong></div>
        <div><span>订阅 Token</span><strong class="code">{{ detailClient.subscribe_token }}</strong></div>
        <div><span>流量</span><strong>{{ trafficText(detailClient) }}</strong></div>
        <div><span>到期时间</span><strong>{{ fmtTime(detailClient.expire_at) }}</strong></div>
        <div><span>直连入站</span><strong>{{ nodeNames(detailClient.node_ids) }}</strong></div>
        <div><span>固定出口线路</span><strong>{{ relayNames(detailClient.relay_route_ids) }}</strong></div>
        <div><span>创建时间</span><strong>{{ fmtTime(detailClient.created_at) }}</strong></div>
        <div><span>更新时间</span><strong>{{ fmtTime(detailClient.updated_at) }}</strong></div>
      </div>
      <div class="modal-actions"><button class="btn" @click="showShare(detailClient)">打开分享</button><button class="btn secondary" @click="copySub(detailClient)">复制订阅</button><button class="btn secondary" @click="editClient(detailClient); closeDetail()">编辑客户</button></div>
    </div>
  </div>

  <div v-if="shareClient" class="modal-mask" @click.self="closeShare">
    <div class="modal-card share-modal-card">
      <div class="modal-head"><div><span class="eyebrow">客户分享</span><h2>{{ shareTitle }}</h2><p>选择客户端类型后复制链接、二维码或配置文件。新手只需要点击“分享”即可找到全部分发内容。</p></div><button class="icon-btn" @click="closeShare">×</button></div>
      <div class="share-client-info">
        <div><span>客户</span><strong>{{ shareClient.username }}</strong></div>
        <div><span>状态</span><strong><span class="badge" :class="shareClient.enabled?'online':''">{{ statusText(shareClient) }}</span></strong></div>
        <div><span>绑定关系</span><strong>{{ clientBindSummary(shareClient) }}</strong></div>
        <div><span>可用节点</span><strong>{{ shareAvailableNodes.length }}</strong></div>
      </div>
      <div class="share-tabs">
        <button v-for="opt in shareTabs" :key="opt.value" :class="['share-tab', {active: shareTab === opt.value}]" @click="setShareTab(opt.value)">
          <strong>{{ opt.label }}</strong><span>{{ opt.tip }}</span>
        </button>
      </div>
      <div class="notice warn modal-tip">{{ shareTip }}<br>请优先扫描单节点二维码；订阅二维码用于客户端后续统一更新。</div>
      <div v-if="!shareAvailableNodes.length && shareTab !== 'subscription'" class="empty-state">该客户暂无可用入站，请先新增入站或检查客户关联入站。</div>
      <div v-else class="share-modal-body">
        <div class="share-config-box">
          <div class="share-config-head"><strong>{{ shareHeading() }}</strong><button class="btn" @click="copyAny(shareText, shareCopyLabel())">复制{{ shareCopyLabel() }}</button></div>
          <pre class="code share-pre">{{ shareText }}</pre>
        </div>
        <div v-if="showQrForMainShare()" class="qr-box share-modal-qr big-share-qr">
            <img :src="qrImageUrl(mainQrText())" alt="二维码" />
            <span>{{ qrLabel() }}｜优先扫这个<br><small>{{ qrModeHint() }}</small></span>
            <button class="btn" @click="openQrZoom(mainQrText(), qrLabel())">放大扫码</button>
          </div>
      </div>
      <div v-if="shareLinks.length" class="node-link-list">
        <div class="node-link-row" v-for="item in shareLinks" :key="item.node.id">
          <div><strong>{{ item.node.name }}</strong><span>{{ item.node.host }}:{{ item.node.port }}<template v-if="item.node.exit_label"> → 出口 {{ item.node.exit_label }}</template></span></div>
          <div class="node-row-actions">
            <button class="btn secondary" @click="copyAny(item.link, '节点链接')">复制链接</button>
            <button class="btn secondary" @click="openQrZoom(qrTextForNode(item), item.node.name + ' 单节点二维码')">放大扫码</button>
          </div>
        </div>
      </div>
    </div>
  </div>


  <div v-if="qrZoomText" class="modal-mask qr-zoom-mask" @click.self="closeQrZoom">
    <div class="modal-card qr-zoom-card">
      <div class="modal-head">
        <div><span class="eyebrow">扫码二维码</span><h2>{{ qrZoomTitle }}</h2><p>V2rayN 二维码采用短链接模式，识别率更高；复制按钮仍可复制完整节点链接。</p></div>
        <button class="icon-btn" @click="closeQrZoom">×</button>
      </div>
      <div class="qr-zoom-body">
        <div class="qr-white-stage"><img :src="qrImageUrl(qrZoomText, 760)" alt="放大二维码" /></div>
        <div class="qr-zoom-actions">
          <button class="btn" @click="copyAny(qrZoomText, '二维码内容')">复制二维码内容</button>
          <button class="btn secondary" @click="closeQrZoom">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>
