<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, clearToken } from './api'
import { APP_VERSION } from './version'

const route = useRoute()
const router = useRouter()

const mainNav = [
  { to: '/', label: '仪表盘', icon: '⌁', desc: '总览' },
  { to: '/nodes', label: '入站管理', icon: '↧', desc: '协议与端口' },
  { to: '/relays', label: '中转管理', icon: '⇄', desc: '加速与落地' },
  { to: '/landing-exits', label: '落地出口', icon: '◎', desc: '出口池' },
  { to: '/clients', label: '客户管理', icon: '◉', desc: '订阅与二维码' },
]
const opsNav = [
  { to: '/diagnostics', label: '系统检测', icon: '✓', desc: '健康检查' },
  { to: '/logs', label: '系统日志', icon: '≡', desc: '操作记录' },
  { to: '/settings', label: '系统设置', icon: '⚙', desc: '密码与安全' },
  { to: '/updates', label: '系统升级', icon: '⬆', desc: '版本与核心' },
]
const advancedNav = [
  { to: '/servers', label: '高级：服务器管理', icon: '◇', desc: '多机接入' },
  { to: '/network-policy', label: '高级：网络策略', icon: '◈', desc: 'DNS / QUIC / IPv6' },
]

const dashboard = ref<any>({})
async function loadLayoutState() {
  try { dashboard.value = await api('/api/dashboard') } catch { dashboard.value = {} }
}
const modeLabel = computed(() => Number(dashboard.value?.servers || 0) <= 1 ? '单机模式' : '专线模式')
const modeClass = computed(() => Number(dashboard.value?.servers || 0) <= 1 ? 'single' : 'multi')
onMounted(loadLayoutState)
watch(() => route.fullPath, loadLayoutState)

const pageName = computed(() => {
  const all = [...mainNav, ...opsNav, ...advancedNav]
  return all.find(item => item.to === route.path)?.label || 'ZXY Panel'
})
function logout() {
  clearToken()
  router.push('/login')
}
</script>

<template>
  <router-view v-if="route.path === '/login'" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand-block">
        <div class="brand-mark">Z</div>
        <div>
          <div class="brand">ZXY Panel</div>
          <div class="brand-sub">专线云节点面板</div>
        </div>
      </div>

      <div class="nav-section">
        <div class="nav-title">常用功能</div>
        <router-link v-for="item in mainNav" :key="item.to" :to="item.to" class="nav-link">
          <span class="nav-icon">{{ item.icon }}</span>
          <span><strong>{{ item.label }}</strong><em>{{ item.desc }}</em></span>
        </router-link>
      </div>

      <div class="nav-section">
        <div class="nav-title">运维工具</div>
        <router-link v-for="item in opsNav" :key="item.to" :to="item.to" class="nav-link">
          <span class="nav-icon">{{ item.icon }}</span>
          <span><strong>{{ item.label }}</strong><em>{{ item.desc }}</em></span>
        </router-link>
      </div>

      <div class="nav-section compact">
        <div class="nav-title">高级模式</div>
        <router-link v-for="item in advancedNav" :key="item.to" :to="item.to" class="nav-link">
          <span class="nav-icon">{{ item.icon }}</span>
          <span><strong>{{ item.label }}</strong><em>{{ item.desc }}</em></span>
        </router-link>
      </div>

      <div class="sidebar-footer">
        <div class="version-pill">{{ APP_VERSION }}</div>
        <button class="logout-btn" @click="logout">退出登录</button>
      </div>
    </aside>

    <main class="content">
      <header class="topbar">
        <div>
          <div class="crumb">ZXY Panel / {{ pageName }}</div>
          <h1>{{ pageName }}</h1>
        </div>
        <div class="topbar-actions" :class="modeClass">
          <span class="status-dot"></span>
          <span>{{ modeLabel }}</span>
        </div>
      </header>
      <router-view />
    </main>
  </div>
</template>
