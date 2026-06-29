import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import Login from './views/Login.vue'
import Dashboard from './views/Dashboard.vue'
import Servers from './views/Servers.vue'
import Nodes from './views/Nodes.vue'
import Relays from './views/Relays.vue'
import LandingExits from './views/LandingExits.vue'
import Clients from './views/Clients.vue'
import Logs from './views/Logs.vue'
import Settings from './views/Settings.vue'
import Diagnostics from './views/Diagnostics.vue'
import Updates from './views/Updates.vue'
import NetworkPolicy from './views/NetworkPolicy.vue'
import './style.css'
import { APP_VERSION } from './version'


const previousFrontendVersion = localStorage.getItem('zxy_frontend_version')
if (previousFrontendVersion && previousFrontendVersion !== APP_VERSION) {
  localStorage.removeItem('zxy_token')
}
localStorage.setItem('zxy_frontend_version', APP_VERSION)

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/login', component: Login },
    { path: '/', component: Dashboard },
    { path: '/servers', component: Servers },
    { path: '/nodes', component: Nodes },
    { path: '/relays', component: Relays },
    { path: '/landing-exits', component: LandingExits },
    { path: '/clients', component: Clients },
    { path: '/logs', component: Logs },
    { path: '/settings', component: Settings },
    { path: '/diagnostics', component: Diagnostics },
    { path: '/updates', component: Updates },
    { path: '/network-policy', component: NetworkPolicy }
  ]
})

router.beforeEach((to) => {
  if (to.path !== '/login' && !localStorage.getItem('zxy_token')) return '/login'
})

createApp(App).use(router).mount('#app')
