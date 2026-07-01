<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'
const servers = ref<any[]>([])
const form = ref<any>({ name:'', ip:'', host:'', region:'US', provider:'' })
const error = ref('')
const message = ref('')
const selectedInstall = ref('')
const PACKAGE_NAME = 'zxy-panel-v0.7.6.4-install-speed-polish.zip'
const PACKAGE_DIR = 'zxy-panel-v0.7.6.4-install-speed-polish'

function publicPanelBase() {
  const base = (import.meta.env.BASE_URL || '/').replace(/\/$/, '')
  return `${location.origin}${base}`
}

async function load() { servers.value = await api('/api/servers') }
async function createServer() {
  error.value=''; message.value=''
  try {
    await api('/api/servers',{method:'POST',body:JSON.stringify(form.value)})
    form.value={name:'',ip:'',host:'',region:'US',provider:''}
    message.value='服务器已添加。下一步复制 Agent 安装命令到目标服务器执行。'
    await load()
  } catch(e:any){ error.value=e.message }
}
async function remove(id:string) { if(!confirm('确认删除这台服务器？')) return; await api(`/api/servers/${id}`,{method:'DELETE'}); await load() }
function fmtBytes(v:number) { if(!v) return '0 B'; const units=['B','KB','MB','GB','TB']; let n=v, i=0; while(n>=1024&&i<units.length-1){n/=1024;i++}; return `${n.toFixed(i?2:0)} ${units[i]}` }
function installCommand(s:any) {
  const base = publicPanelBase()
  return `cd /root && test -f ${PACKAGE_NAME} || { echo '请先把 ${PACKAGE_NAME} 上传到 /root'; exit 1; }; apt update && apt install -y unzip curl && rm -rf ${PACKAGE_DIR} && unzip -o ${PACKAGE_NAME} && cd ${PACKAGE_DIR} && chmod +x deploy/agent-install.sh && INSTALL_XRAY=true SETUP_XRAY_SERVICE=true APPLY_CONFIG=true PANEL_BASE='${base}' SERVER_ID='${s.id}' AGENT_TOKEN='${s.agent_token}' ./deploy/agent-install.sh`
}
async function copyInstall(s:any) {
  const cmd = installCommand(s)
  selectedInstall.value = cmd
  const ok = await copyText(cmd)
  if (ok) {
    message.value = '一键安装命令已复制。请先把当前 ZIP 包上传到目标服务器 /root，再在目标服务器 root 终端执行该命令。'
  } else {
    message.value = '浏览器禁止自动复制，已在下方显示完整命令，请手动选中复制。'
  }
}
onMounted(load)
</script>
<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">高级：服务器管理</h1>
      <p class="page-desc">V0.7.6.4 多服务器模式：本机服务器可作为主控/落地服务器，远程服务器复制一键命令后只安装 Agent 接入。</p>
    </div>
  </div>
  <div class="form">
    <input v-model="form.name" placeholder="服务器名称" />
    <input v-model="form.ip" placeholder="服务器 IP" />
    <input v-model="form.host" placeholder="域名/Host" />
    <input v-model="form.region" placeholder="地区" />
    <input v-model="form.provider" placeholder="服务商" />
    <div class="actions"><button class="btn" @click="createServer">新增服务器</button></div>
  </div>
  <div class="error" v-if="error">{{ error }}</div>
  <div class="success" v-if="message">{{ message }}</div>
  <div v-if="selectedInstall" class="card code config-preview">{{ selectedInstall }}</div>
  <table>
    <thead><tr><th>名称</th><th>IP / Host</th><th>地区</th><th>状态</th><th>资源</th><th>Xray / Agent</th><th>同步</th><th>操作</th></tr></thead>
    <tbody>
      <tr v-for="s in servers" :key="s.id">
        <td><strong>{{ s.name || '未命名服务器' }}</strong><br><span class="muted">{{ s.provider }}</span></td>
        <td>{{ s.ip }}<br><span class="code">{{ s.host }}</span></td>
        <td>{{ s.region }}</td>
        <td><span class="badge" :class="s.status==='online'?'online':''">{{ s.status }}</span></td>
        <td>CPU/负载：{{ s.cpu_usage || 0 }}<br>内存：{{ s.memory_usage || 0 }}%｜硬盘：{{ s.disk_usage || 0 }}%<br>上行：{{ fmtBytes(s.upload_total) }}｜下行：{{ fmtBytes(s.download_total) }}</td>
        <td><span class="code">{{ s.agent_version || '-' }}</span><br><span class="code">{{ s.xray_version || '-' }}</span></td>
        <td><span class="code">{{ s.config_hash ? s.config_hash.slice(0,12) : '-' }}</span><br><span class="muted">{{ s.last_sync_message || '-' }}</span></td>
        <td class="row-actions"><button class="btn secondary" @click="copyInstall(s)">复制一键安装命令</button> <button class="btn danger" @click="remove(s.id)">删除</button></td>
      </tr>
    </tbody>
  </table>
</template>
