const ROOT_ROUTE_SEGMENTS = new Set([
  'login', 'dashboard', 'servers', 'nodes', 'relays', 'landing-exits', 'clients', 'logs', 'settings', 'diagnostics', 'updates', 'network-policy',
  'api', 'assets', 'sub', 's'
])

export function runtimeBasePath() {
  const viteBase = import.meta.env.BASE_URL || '/'
  if (viteBase && viteBase !== '/') return viteBase.endsWith('/') ? viteBase : `${viteBase}/`
  const parts = location.pathname.split('/').filter(Boolean)
  if (!parts.length) return '/'
  const first = parts[0]
  if (ROOT_ROUTE_SEGMENTS.has(first)) return '/'
  return `/${first}/`
}
