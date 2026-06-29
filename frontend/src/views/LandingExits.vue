<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'

const exits = ref<any[]>([])
const message = ref('')
const error = ref('')
const testingId = ref('')
const showEditor = ref(false)
const showBulk = ref(false)
const editingId = ref('')
const bulkText = ref('')
const form = ref<any>(defaultForm())

function defaultForm() {
  return { name:'', host:'', port:'', username:'', password:'', udp:false, region:'', provider:'', bandwidth_mbps:0, remark:'', enabled:true }
}
const enabledCount = computed(() => exits.value.filter((e:any)=>e.enabled !== false).length)
function fmtTime(v:string) { if(!v) return '-'; const d=new Date(v); return Number.isNaN(d.getTime()) ? v : d.toLocaleString() }
function exitLabel(e:any) { return `${e.name || e.host} / ${e.host}:${e.port}` }
async function load() { try { exits.value = await api('/api/landing-exits') } catch(e:any) { error.value=e.message || '加载失败' } }
function openCreate() { editingId.value=''; form.value=defaultForm(); showEditor.value=true }
function openEdit(e:any) { editingId.value=e.id; form.value={...e}; showEditor.value=true }
function validate() {
  if(!String(form.value.host||'').trim()) return '请填写出口 IP 或域名。'
  const p=Number(form.value.port); if(!p || p<1 || p>65535) return '请填写正确的 SOCKS5 端口。'
  if(!String(form.value.username||'').trim() || !String(form.value.password||'').trim()) return '请填写 SOCKS5 账号和密码。'
  return ''
}
async function save() {
  error.value=''; message.value=''
  const err=validate(); if(err){ error.value=err; return }
  const payload={...form.value, port:Number(form.value.port), bandwidth_mbps:Number(form.value.bandwidth_mbps||0)}
  try{
    if(editingId.value) await api(`/api/landing-exits/${editingId.value}`, {method:'PUT', body:JSON.stringify(payload)})
    else await api('/api/landing-exits', {method:'POST', body:JSON.stringify(payload)})
    message.value=editingId.value?'落地出口已保存。':'落地出口已新增。'
    showEditor.value=false
    await load()
  }catch(e:any){ error.value=e.message || '保存失败' }
}
async function remove(id:string) { if(!confirm('确认删除这个落地出口？已被中转线路使用的出口不能删除。')) return; try{ await api(`/api/landing-exits/${id}`,{method:'DELETE'}); message.value='落地出口已删除。'; await load() }catch(e:any){ error.value=e.message || '删除失败' } }
async function testExit(e:any) {
  testingId.value=e.id; error.value=''; message.value=''
  try{
    const res=await api(`/api/landing-exits/${e.id}/test`, {method:'POST'})
    if(res.ok) message.value=`出口检测成功：${e.host}:${e.port} → ${res.exit_ip}，耗时 ${res.latency_ms}ms。`
    else error.value=`出口检测失败：${res.message}`
    await load()
  }catch(err:any){ error.value=err.message || '测试失败' }
  finally{ testingId.value='' }
}
async function copyFirewall(e:any) {
  const cmd=`ufw allow from 中转服务器IP to any port ${Number(e.port)} proto tcp && ufw reload`
  const ok=await copyText(cmd)
  message.value=ok?'出口防火墙命令已复制，请把“中转服务器IP”替换为实际中转 IP。':'复制失败，请手动复制。'
}
async function bulkImport() {
  error.value=''; message.value=''
  if(!String(bulkText.value||'').trim()){ error.value='请粘贴批量导入内容。'; return }
  try{
    const res=await api('/api/landing-exits/bulk', {method:'POST', body:JSON.stringify({text:bulkText.value})})
    message.value=`批量导入完成：新增 ${res.created || 0} 个出口。${Array.isArray(res.errors)&&res.errors.length ? '有错误：' + res.errors.join('；') : ''}`
    showBulk.value=false
    bulkText.value=''
    await load()
  }catch(e:any){ error.value=e.message || '批量导入失败' }
}
onMounted(load)
</script>
<template>
  <div class="page-head">
    <div><h1 class="page-title">落地出口管理</h1><p class="page-desc">V0.7.5.9.1：集中保存 50 台出口 IP 的 SOCKS5 参数，客户绑定出口时直接选择，避免手动反复填写。</p></div>
    <div class="head-actions"><button class="btn secondary" @click="showBulk=true">批量导入</button><button class="btn" @click="openCreate">新增落地出口</button></div>
  </div>
  <div class="notice ok">推荐模式：一个客户绑定一条中转线路和一个固定出口 IP。不要把多个出口做成随机池，跨境账号更需要固定出口。</div>
  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>
  <div class="client-summary-grid">
    <div class="client-summary-card"><span>出口总数</span><strong>{{ exits.length }}</strong></div>
    <div class="client-summary-card"><span>启用出口</span><strong>{{ enabledCount }}</strong></div>
    <div class="client-summary-card"><span>最近检测</span><strong>{{ exits.filter((e:any)=>e.last_test_ip).length }}</strong></div>
    <div class="client-summary-card"><span>适合场景</span><strong>固定出口</strong></div>
  </div>
  <div class="card">
    <h2>落地出口列表</h2>
    <table>
      <thead><tr><th>出口</th><th>SOCKS5</th><th>地区/带宽</th><th>检测结果</th><th>状态</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="e in exits" :key="e.id">
          <td><strong>{{ e.name }}</strong><br><span class="muted">{{ e.remark || '-' }}</span></td>
          <td><span class="code">{{ e.host }}:{{ e.port }}</span><br><span class="muted">账号：{{ e.username }}｜UDP：{{ e.udp ? '开' : '关' }}</span></td>
          <td>{{ e.region || '-' }}<br><span class="muted">{{ e.bandwidth_mbps ? e.bandwidth_mbps + 'M' : '未填带宽' }}</span></td>
          <td><span class="code">{{ e.last_test_ip || '未检测' }}</span><br><span class="muted">{{ e.last_test_msg || '-' }} {{ e.last_test_at ? '｜' + fmtTime(e.last_test_at) : '' }}</span></td>
          <td><span class="badge" :class="e.enabled !== false ? 'online' : ''">{{ e.enabled !== false ? '启用' : '停用' }}</span></td>
          <td class="row-actions"><button class="btn secondary" :disabled="testingId===e.id" @click="testExit(e)">{{ testingId===e.id ? '检测中' : '检测出口' }}</button><button class="btn secondary" @click="copyFirewall(e)">防火墙命令</button><button class="btn secondary" @click="openEdit(e)">编辑</button><button class="btn danger" @click="remove(e.id)">删除</button></td>
        </tr>
        <tr v-if="!exits.length"><td colspan="6" class="muted">暂无落地出口。先新增或批量导入出口。</td></tr>
      </tbody>
    </table>
  </div>
  <div v-if="showEditor" class="modal-mask" @click.self="showEditor=false">
    <div class="modal-card relay-editor-modal">
      <div class="modal-head"><div><span class="eyebrow">LANDING EXIT</span><h2>{{ editingId ? '编辑落地出口' : '新增落地出口' }}</h2><p>填写出口服务器上的 SOCKS5 入站信息，后续客户固定出口会直接选择这里保存的出口。</p></div><button class="icon-btn" @click="showEditor=false">×</button></div>
      <div class="form grid-3 compact-form">
        <label><span>出口名称</span><input v-model="form.name" placeholder="例如：洛杉矶01" /></label>
        <label><span>出口 IP / 域名</span><input v-model="form.host" placeholder="203.0.113.10" /></label>
        <label><span>SOCKS5 端口</span><input v-model.number="form.port" type="number" /></label>
        <label><span>SOCKS5 账号</span><input v-model="form.username" /></label>
        <label><span>SOCKS5 密码</span><input v-model="form.password" /></label>
        <label><span>UDP</span><select v-model="form.udp"><option :value="false">关闭（推荐）</option><option :value="true">开启</option></select></label>
        <label><span>地区</span><input v-model="form.region" placeholder="美国洛杉矶" /></label>
        <label><span>服务商</span><input v-model="form.provider" placeholder="NTT / Fast Data" /></label>
        <label><span>带宽 M</span><input v-model.number="form.bandwidth_mbps" type="number" placeholder="150" /></label>
        <label class="wide"><span>备注</span><input v-model="form.remark" placeholder="客户A / 账号用途 / 线路说明" /></label>
        <label><span>状态</span><select v-model="form.enabled"><option :value="true">启用</option><option :value="false">停用</option></select></label>
      </div>
      <div class="modal-actions"><button class="btn" @click="save">保存出口</button><button class="btn secondary" @click="showEditor=false">取消</button></div>
    </div>
  </div>
  <div v-if="showBulk" class="modal-mask" @click.self="showBulk=false">
    <div class="modal-card share-modal-card">
      <div class="modal-head"><div><span class="eyebrow">BULK IMPORT</span><h2>批量导入落地出口</h2><p>每行一个出口，格式：名称,IP,端口,账号,密码,地区,备注。也支持用 | 分隔。</p></div><button class="icon-btn" @click="showBulk=false">×</button></div>
      <textarea v-model="bulkText" class="bulk-textarea" placeholder="洛杉矶01,203.0.113.10,33668,user01,pass01,美国洛杉矶,客户A\n洛杉矶02,203.0.113.11,33668,user02,pass02,美国洛杉矶,客户B"></textarea>
      <div class="modal-actions"><button class="btn" @click="bulkImport">开始导入</button><button class="btn secondary" @click="showBulk=false">取消</button></div>
    </div>
  </div>
</template>
