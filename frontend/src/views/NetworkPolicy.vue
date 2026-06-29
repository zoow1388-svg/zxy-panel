<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const status = ref<any>(null)
const preview = ref<any>(null)
const policy = ref<any>({})

const modeTips: Record<string, string> = {
  compat: '默认推荐：保留 V0.7.5.8 稳定行为，公共 DNS + UseIPv4，不启用 53 阻断、不禁用 fallback、不阻断 QUIC。',
  public_dns: '使用 1.1.1.1 / 8.8.8.8 / 9.9.9.9 和 UseIPv4，不启用强阻断，适合 AI / Google / 海外社媒。',
  dns_leak_guard: '偏向减少 DNS 漂移，但不强制阻断主链路。建议在 DNS 检测偶发异常时使用。',
  strict: '严格模式可能导致网速变慢、解析失败或软路由兼容性下降。仅建议确认 DNS 泄漏后手动启用。',
  custom: '高级用户自定义。请谨慎启用阻断 53、禁用 fallback、阻断 QUIC 等选项。'
}

const dnsText = computed({
  get() { return (policy.value?.dns_servers || []).join('\n') },
  set(v: string) { policy.value.dns_servers = v.split(/[\n,，\s]+/).map(x => x.trim()).filter(Boolean) }
})

async function load() {
  loading.value = true
  error.value = ''
  message.value = ''
  try {
    status.value = await api(`/api/network-policy?t=${Date.now()}`)
    policy.value = JSON.parse(JSON.stringify(status.value.policy || {}))
    preview.value = null
  } catch (e: any) {
    error.value = e?.message || '加载网络策略失败'
  } finally {
    loading.value = false
  }
}

function setMode(mode: string) {
  policy.value.mode = mode
  if (mode === 'compat') {
    policy.value.public_dns = true
    policy.value.dns_servers = ['1.1.1.1', '8.8.8.8', '9.9.9.9']
    policy.value.query_strategy = 'UseIPv4'
    policy.value.disable_fallback = false
    policy.value.disable_fallback_if_match = false
    policy.value.block_dns_53 = false
    policy.value.block_china_dns = false
    policy.value.block_quic = false
    policy.value.ipv6_strategy = 'keep'
    policy.value.clash_include_quad9 = false
    policy.value.sing_box_include_quad9 = false
  }
  if (mode === 'public_dns') {
    policy.value.public_dns = true
    policy.value.dns_servers = ['1.1.1.1', '8.8.8.8', '9.9.9.9']
    policy.value.query_strategy = 'UseIPv4'
    policy.value.disable_fallback = false
    policy.value.disable_fallback_if_match = false
    policy.value.block_dns_53 = false
    policy.value.block_china_dns = false
    policy.value.block_quic = false
    policy.value.ipv6_strategy = 'keep'
    policy.value.clash_include_quad9 = true
    policy.value.sing_box_include_quad9 = true
  }
  if (mode === 'dns_leak_guard') {
    policy.value.public_dns = true
    policy.value.dns_servers = ['1.1.1.1', '8.8.8.8', '9.9.9.9']
    policy.value.query_strategy = 'UseIPv4'
    policy.value.disable_fallback = false
    policy.value.disable_fallback_if_match = false
    policy.value.block_dns_53 = false
    policy.value.block_china_dns = false
    policy.value.block_quic = false
    policy.value.ipv6_strategy = 'warn'
    policy.value.clash_include_quad9 = true
    policy.value.sing_box_include_quad9 = true
  }
  if (mode === 'strict') {
    policy.value.public_dns = true
    policy.value.dns_servers = ['1.1.1.1', '8.8.8.8', '9.9.9.9']
    policy.value.query_strategy = 'UseIPv4'
    policy.value.disable_fallback = true
    policy.value.disable_fallback_if_match = true
    policy.value.block_dns_53 = true
    policy.value.block_china_dns = true
    policy.value.block_quic = true
    policy.value.ipv6_strategy = 'disable_hint'
    policy.value.clash_include_quad9 = true
    policy.value.sing_box_include_quad9 = true
  }
}

async function makePreview() {
  loading.value = true
  error.value = ''
  message.value = ''
  try {
    preview.value = await api('/api/network-policy/preview', { method: 'POST', body: JSON.stringify({ policy: policy.value }) })
    message.value = '已生成预览，尚未修改配置。'
  } catch (e: any) {
    error.value = e?.message || '生成预览失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  error.value = ''
  message.value = ''
  const strict = policy.value.mode === 'strict' || policy.value.block_dns_53 || policy.value.block_quic || policy.value.disable_fallback
  if (strict) {
    const ok = confirm('当前策略包含严格选项，可能导致网速变慢、DNS 解析失败或软路由兼容性下降。确认要应用吗？')
    if (!ok) return
  } else {
    const ok = confirm('应用网络策略会重新生成 Xray 配置，Agent 下一次同步后会重启网络核心，当前连接可能短暂中断。确认应用吗？')
    if (!ok) return
  }
  saving.value = true
  try {
    const res = await api('/api/network-policy?confirm=yes', { method: 'PUT', body: JSON.stringify({ policy: policy.value }) })
    message.value = res?.message || '网络策略已保存。'
    status.value = res
    policy.value = JSON.parse(JSON.stringify(res.policy || policy.value))
    preview.value = res
  } catch (e: any) {
    error.value = e?.message || '保存网络策略失败'
  } finally {
    saving.value = false
  }
}

async function rollback() {
  const ok = confirm('确认回滚到上一次网络策略吗？回滚后 Agent 下一次同步会重新应用配置。')
  if (!ok) return
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const res = await api('/api/network-policy/rollback', { method: 'POST', body: '{}' })
    message.value = res?.message || '已回滚。'
    await load()
  } catch (e: any) {
    error.value = e?.message || '回滚失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">网络策略中心</h1>
      <p class="page-desc">V0.7.5.8：DNS、IPv6、QUIC、UDP、53 端口阻断全部由用户手动调配。升级不会自动启用强阻断，不会覆盖现网策略。</p>
    </div>
    <div class="head-actions">
      <button class="btn secondary" @click="load" :disabled="loading">刷新状态</button>
      <button class="btn secondary" @click="makePreview" :disabled="loading">预览配置</button>
      <button class="btn" @click="save" :disabled="saving">{{ saving ? '应用中...' : '应用策略' }}</button>
    </div>
  </div>

  <div v-if="error" class="error">{{ error }}</div>
  <div v-if="message" class="success">{{ message }}</div>

  <div class="notice warn">
    产品规则：网络策略只提供能力，不在版本升级时替用户强制决定。阻断 53、禁用 fallback、阻断 QUIC 等严格选项可能影响速度，只建议在确认泄漏时手动启用。
  </div>

  <div class="cards diag-cards" v-if="policy">
    <div class="card"><div class="label">当前模式</div><div class="value small">{{ policy.mode || 'compat' }}</div></div>
    <div class="card"><div class="label">DNS 服务器</div><div class="value small code">{{ (policy.dns_servers || []).join(' / ') || '跟随默认' }}</div></div>
    <div class="card"><div class="label">查询策略</div><div class="value small">{{ policy.query_strategy || 'AsIs' }}</div></div>
    <div class="card"><div class="label">严格选项</div><div class="value small">{{ (policy.block_dns_53 || policy.block_quic || policy.disable_fallback) ? '有启用' : '未启用' }}</div></div>
  </div>

  <div class="card update-card">
    <div class="row-between"><div><h2>一、选择网络策略模式</h2><p class="muted">默认建议使用兼容稳定模式。其它模式需要用户自己确认后应用。</p></div></div>
    <div class="mode-tabs policy-tabs">
      <button v-for="m in status?.modes || []" :key="m.value" class="mode-tab" :class="{active: policy.mode === m.value}" @click="setMode(m.value)">
        <strong>{{ m.label }}</strong>
        <span>{{ m.desc }}</span>
      </button>
    </div>
    <div class="notice" :class="policy.mode === 'strict' ? 'warn' : 'ok'">{{ modeTips[policy.mode] || modeTips.compat }}</div>
  </div>

  <div class="card update-card">
    <div class="row-between"><div><h2>二、高级手动开关</h2><p class="muted">只有自定义或严格排查场景才建议手动调整。普通客户不要随便开强阻断。</p></div></div>
    <div class="form node-form clean-form">
      <label class="field"><span>启用公共 DNS</span><select v-model="policy.public_dns"><option :value="true">开启</option><option :value="false">关闭</option></select></label>
      <label class="field"><span>queryStrategy</span><select v-model="policy.query_strategy"><option>AsIs</option><option>UseIPv4</option><option>UseIPv6</option><option>UseIP</option></select></label>
      <label class="field"><span>IPv6 策略</span><select v-model="policy.ipv6_strategy"><option value="keep">不处理</option><option value="warn">检测提醒</option><option value="disable_hint">建议禁用</option></select></label>
      <label class="field"><span>Clash Meta Quad9</span><select v-model="policy.clash_include_quad9"><option :value="true">加入</option><option :value="false">不加入</option></select></label>
      <label class="field wide"><span>DNS 服务器，一行一个</span><textarea class="textarea" v-model="dnsText" rows="4" placeholder="1.1.1.1&#10;8.8.8.8&#10;9.9.9.9"></textarea></label>
      <label class="field"><span>sing-box Quad9</span><select v-model="policy.sing_box_include_quad9"><option :value="true">加入</option><option :value="false">不加入</option></select></label>
      <label class="field"><span>禁用 fallback</span><select v-model="policy.disable_fallback"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
      <label class="field"><span>disableFallbackIfMatch</span><select v-model="policy.disable_fallback_if_match"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
      <label class="field"><span>阻断 tcp/udp 53</span><select v-model="policy.block_dns_53"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
      <label class="field"><span>阻断中国公共 DNS</span><select v-model="policy.block_china_dns"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
      <label class="field"><span>阻断 QUIC UDP 443</span><select v-model="policy.block_quic"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
    </div>
  </div>

  <div class="card update-card" v-if="preview || status">
    <div class="row-between">
      <div><h2>三、策略预览与风险提示</h2><p class="muted">这里只显示即将生成的 DNS 与路由策略。真正应用后由 Agent 测试并重启 Xray。</p></div>
      <button class="btn danger" @click="rollback" :disabled="saving">回滚上一次策略</button>
    </div>
    <div class="notice ok" v-if="(preview || status)?.summary?.length">
      <ul class="update-list"><li v-for="item in (preview || status).summary" :key="item">{{ item }}</li></ul>
    </div>
    <div class="notice warn" v-if="(preview || status)?.warnings?.length">
      <strong>风险提示：</strong>
      <ul class="update-list"><li v-for="item in (preview || status).warnings" :key="item">{{ item }}</li></ul>
    </div>
    <div class="config-preview">
      <strong>Xray DNS 预览</strong>
      <pre class="code pre-wrap">{{ JSON.stringify((preview || status)?.xray_dns_preview || {}, null, 2) }}</pre>
    </div>
    <div class="config-preview">
      <strong>路由策略预览</strong>
      <pre class="code pre-wrap">{{ JSON.stringify((preview || status)?.routing_preview || [], null, 2) }}</pre>
    </div>
  </div>
</template>
