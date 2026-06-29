import qrcode from 'qrcode-generator'

export type ShareFormat = 'v2rayn' | 'shadowrocket' | 'clash' | 'singbox'

export const shareFormatOptions: Array<{ value: ShareFormat; label: string; tip: string }> = [
  { value: 'v2rayn', label: 'V2rayN', tip: '通用 VLESS Reality 单节点链接，优先用于扫码导入。' },
  { value: 'shadowrocket', label: 'Shadowrocket', tip: 'iPhone 小火箭，使用同一条 VLESS Reality 通用链接。' },
  { value: 'clash', label: 'Clash Meta', tip: '生成 Clash Meta YAML 配置，可复制到配置文件。' },
  { value: 'singbox', label: 'sing-box', tip: '生成 sing-box JSON 配置，可用于 sing-box / SFI。' },
]


export function buildRelayVirtualNode(relay: any): any {
  return {
    id: relay.id,
    name: relay.name || `中转线路-${relay.relay_port}`,
    protocol: 'vless',
    host: relay.relay_host,
    port: relay.relay_port,
    transport: 'tcp',
    security: 'reality',
    sni: relay.relay_sni || 'www.intel.com',
    fingerprint: relay.relay_fingerprint || 'chrome',
    reality_public_key: relay.relay_reality_public_key || '',
    reality_short_id: relay.relay_reality_short_id || '',
    reality_spider_x: relay.relay_reality_spider_x || '/',
    is_relay_route: true,
    exit_label: relay.landing_mode === 'manual_socks5' ? `${relay.manual_socks_host}:${relay.manual_socks_port}` : relay.landing_node_id,
  }
}

export function clientCanUseRelay(client: any, relay: any): boolean {
  const ids = Array.isArray(client?.relay_route_ids) ? client.relay_route_ids : []
  return ids.includes(relay.id)
}

export function relayNodesForClient(client: any, relays: any[]): any[] {
  return relays.filter(r => r && r.enabled !== false && String(r.route_mode || '') === 'socks5_route' && clientCanUseRelay(client, r)).map(buildRelayVirtualNode)
}

export function qrImageUrl(text: string, size = 560): string {
  const data = String(text || '').trim()
  if (!data) return ''
  const targetSize = Math.max(260, Math.min(Number(size) || 560, 900))
  const qr = qrcode(0, 'M')
  qr.addData(data)
  qr.make()
  const margin = 8
  const moduleCount = qr.getModuleCount() || 41
  const cellSize = Math.max(3, Math.floor(targetSize / (moduleCount + margin * 2)))
  return qr.createDataURL(cellSize, margin)
}

function appBasePath(): string {
  const base = import.meta.env.BASE_URL || '/'
  return base.endsWith('/') ? base : `${base}/`
}

export function buildSubscriptionUrl(client: any): string {
  return `${location.origin}${appBasePath()}sub/${client.subscribe_token}`
}

export function buildShortNodeUrl(client: any, node: any): string {
  return `${location.origin}${appBasePath()}s/${client.subscribe_token}/${node.id}`
}

export function clientCanUseNode(client: any, node: any): boolean {
  const ids = Array.isArray(client?.node_ids) ? client.node_ids : []
  const relayIds = Array.isArray(client?.relay_route_ids) ? client.relay_route_ids : []
  // 固定出口客户如果没有显式绑定普通入站，不再回退到所有普通 VLESS 入站。
  // 这能避免客户分享时错误复制普通入站，导致出口变成中转服务器 IP。
  if (ids.length === 0 && relayIds.length > 0) return false
  return ids.length === 0 || ids.includes(node.id)
}

export function isClientShareNode(node: any): boolean {
  return String(node?.protocol || '').toLowerCase() === 'vless'
}

export function nodesForClient(client: any, nodes: any[]): any[] {
  // SOCKS5 入站是给中转服务器连接落地出口用的，不直接生成客户 VLESS 订阅/二维码。
  return nodes.filter(n => n && n.enabled !== false && isClientShareNode(n) && clientCanUseNode(client, n))
}

export function clientsForNode(node: any, clients: any[]): any[] {
  return clients.filter(c => c && c.enabled !== false && clientCanUseNode(c, node))
}

function valueOr(v: any, fallback: string): string {
  const s = String(v || '').trim()
  return s || fallback
}

function nodeHost(node: any): string {
  return valueOr(node.host || node.address || node.ip, '127.0.0.1')
}

function nodeName(node: any): string {
  return valueOr(node.name || node.remark, `${nodeHost(node)}:${Number(node.port || 443)}`)
}

function safeName(node: any, client?: any): string {
  const cn = client?.username ? `-${client.username}` : ''
  return `${nodeName(node)}${cn}`.replace(/[\r\n]+/g, ' ').trim()
}

function isReality(node: any): boolean {
  return String(node.security || '').toLowerCase() === 'reality'
}

function transportOf(node: any): string {
  return valueOr(node.transport, 'tcp').toLowerCase()
}

export function buildVlessLink(node: any, client: any, format: 'v2rayn' | 'shadowrocket' = 'v2rayn'): string {
  const host = nodeHost(node)
  const port = Number(node.port || 443)
  const transport = transportOf(node)
  const security = valueOr(node.security, 'none').toLowerCase()
  const q = new URLSearchParams()
  q.set('encryption', 'none')
  q.set('security', security)
  if (node.sni) q.set('sni', node.sni)
  if (security === 'reality') {
    q.set('flow', 'xtls-rprx-vision')
    q.set('fp', valueOr(node.fingerprint, 'chrome'))
    if (node.reality_public_key) q.set('pbk', node.reality_public_key)
    if (node.reality_short_id) q.set('sid', node.reality_short_id)
    q.set('spx', valueOr(node.reality_spider_x, '/'))
  }
  q.set('type', transport)
  if (transport === 'ws' && node.path) q.set('path', node.path)
  if (transport === 'grpc' && node.path) q.set('serviceName', String(node.path).replace(/^\/+/, ''))
  const label = encodeURIComponent(nodeName(node))
  return `vless://${client.uuid}@${host}:${port}?${q.toString()}#${label}`
}

function yamlQuote(v: any): string {
  return JSON.stringify(String(v ?? ''))
}

function clashProxy(node: any, client: any): string {
  const transport = transportOf(node)
  const lines: string[] = []
  lines.push(`  - name: ${yamlQuote(safeName(node))}`)
  lines.push(`    type: vless`)
  lines.push(`    server: ${yamlQuote(nodeHost(node))}`)
  lines.push(`    port: ${Number(node.port || 443)}`)
  lines.push(`    uuid: ${yamlQuote(client.uuid)}`)
  lines.push(`    network: ${yamlQuote(transport)}`)
  lines.push(`    udp: true`)
  if (isReality(node) || String(node.security || '').toLowerCase() === 'tls') {
    lines.push(`    tls: true`)
    if (node.sni) lines.push(`    servername: ${yamlQuote(node.sni)}`)
    lines.push(`    client-fingerprint: ${yamlQuote(valueOr(node.fingerprint, 'chrome'))}`)
  } else {
    lines.push(`    tls: false`)
  }
  if (isReality(node)) {
    lines.push(`    reality-opts:`)
    lines.push(`      public-key: ${yamlQuote(node.reality_public_key || '')}`)
    lines.push(`      short-id: ${yamlQuote(node.reality_short_id || '')}`)
  }
  if (transport === 'ws' && node.path) {
    lines.push(`    ws-opts:`)
    lines.push(`      path: ${yamlQuote(node.path)}`)
  }
  if (transport === 'grpc' && node.path) {
    lines.push(`    grpc-opts:`)
    lines.push(`      grpc-service-name: ${yamlQuote(String(node.path).replace(/^\/+/, ''))}`)
  }
  return lines.join('\n')
}

export function buildClashMetaConfig(nodes: any[], client: any, policy: any = {}): string {
  const enabled = nodes.filter(Boolean)
  const names = enabled.map(n => safeName(n))
  const proxyList = enabled.map(n => clashProxy(n, client)).join('\n')
  const groupProxies = names.length ? names.map(n => `      - ${yamlQuote(n)}`).join('\n') : '      - DIRECT'
  const nameservers = ['    - 1.1.1.1', '    - 8.8.8.8']
  if (policy?.clash_include_quad9) nameservers.push('    - 9.9.9.9')
  return [
    'mixed-port: 7890',
    'allow-lan: false',
    'mode: rule',
    'log-level: info',
    'ipv6: false',
    'dns:',
    '  enable: true',
    '  ipv6: false',
    '  enhanced-mode: fake-ip',
    '  nameserver:',
    ...nameservers,
    'proxies:',
    proxyList || '  []',
    'proxy-groups:',
    '  - name: PROXY',
    '    type: select',
    '    proxies:',
    groupProxies,
    'rules:',
    '  - MATCH,PROXY',
    '',
  ].join('\n')
}

function singBoxOutbound(node: any, client: any): any {
  const transport = transportOf(node)
  const outbound: any = {
    type: 'vless',
    tag: safeName(node),
    server: nodeHost(node),
    server_port: Number(node.port || 443),
    uuid: client.uuid,
    packet_encoding: 'xudp',
  }
  if (isReality(node) || String(node.security || '').toLowerCase() === 'tls') {
    outbound.tls = {
      enabled: true,
      server_name: valueOr(node.sni, nodeHost(node)),
      utls: { enabled: true, fingerprint: valueOr(node.fingerprint, 'chrome') },
    }
    if (isReality(node)) {
      outbound.tls.reality = {
        enabled: true,
        public_key: node.reality_public_key || '',
        short_id: node.reality_short_id || '',
      }
    }
  }
  if (transport === 'ws') {
    outbound.transport = { type: 'ws', path: valueOr(node.path, '/zxy') }
  }
  if (transport === 'grpc') {
    outbound.transport = { type: 'grpc', service_name: valueOr(String(node.path || '').replace(/^\/+/, ''), 'zxy') }
  }
  return outbound
}

export function buildSingBoxConfig(nodes: any[], client: any, policy: any = {}): string {
  const enabled = nodes.filter(Boolean)
  const outbounds = enabled.map(n => singBoxOutbound(n, client))
  const selector = {
    type: 'selector',
    tag: 'PROXY',
    outbounds: outbounds.map(o => o.tag),
    default: outbounds[0]?.tag || 'direct',
  }
  const config = {
    log: { level: 'info' },
    dns: {
      servers: [
        { tag: 'cloudflare', address: '1.1.1.1' },
        { tag: 'google', address: '8.8.8.8' },
        ...(policy?.sing_box_include_quad9 ? [{ tag: 'quad9', address: '9.9.9.9' }] : []),
      ],
      final: 'cloudflare',
      strategy: 'ipv4_only',
    },
    inbounds: [
      { type: 'mixed', tag: 'mixed-in', listen: '127.0.0.1', listen_port: 2080 },
    ],
    outbounds: [selector, ...outbounds, { type: 'direct', tag: 'direct' }],
    route: { final: 'PROXY' },
  }
  return JSON.stringify(config, null, 2)
}

export function buildClientShare(node: any, client: any, format: ShareFormat, policy: any = {}): string {
  if (format === 'shadowrocket') return buildVlessLink(node, client, 'shadowrocket')
  if (format === 'clash') return buildClashMetaConfig([node], client, policy)
  if (format === 'singbox') return buildSingBoxConfig([node], client, policy)
  return buildVlessLink(node, client, 'v2rayn')
}

export function buildClientMultiShare(nodes: any[], client: any, format: ShareFormat, policy: any = {}): string {
  const clientNodes = nodes.filter(isClientShareNode)
  if (format === 'clash') return buildClashMetaConfig(clientNodes, client, policy)
  if (format === 'singbox') return buildSingBoxConfig(clientNodes, client, policy)
  return clientNodes.map(n => buildVlessLink(n, client, format === 'shadowrocket' ? 'shadowrocket' : 'v2rayn')).join('\n')
}

export function isQrShareFormat(format: ShareFormat): boolean {
  return format === 'v2rayn' || format === 'shadowrocket'
}

export function shareFormatLabel(format: ShareFormat): string {
  return shareFormatOptions.find(x => x.value === format)?.label || format
}
