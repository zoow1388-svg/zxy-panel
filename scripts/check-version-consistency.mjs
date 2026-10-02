import { createHash } from 'node:crypto'
import { readFileSync, statSync } from 'node:fs'
import { basename, dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const options = { mode: 'dev', root: resolve(dirname(fileURLToPath(import.meta.url)), '..') }
const allowedOptions = new Set(['mode', 'root', 'manifest', 'package'])

function requireValue(condition, message) {
  if (!condition) throw new Error(message)
}

function parseSource(value, label) {
  const match = /^(\d+(?:\.\d+){2,})-([a-z][a-z0-9]*(?:-[a-z0-9]+)*)$/.exec(value)
  requireValue(match, `${label}: expected a numeric version followed by a codename`)
  return { full: value, number: match[1], codename: match[2] }
}

function compareNumbers(a, b) {
  const left = a.split('.').map(Number)
  const right = b.split('.').map(Number)
  for (let i = 0; i < Math.max(left.length, right.length); i++) {
    const difference = (left[i] ?? 0) - (right[i] ?? 0)
    if (difference !== 0) return Math.sign(difference)
  }
  return 0
}

function checkManifest(manifest, label) {
  for (const key of ['latest', 'version', 'codename', 'package', 'download_url', 'sha256']) {
    requireValue(typeof manifest[key] === 'string' && manifest[key].length > 0, `${label}: missing ${key}`)
  }
  const parsed = parseSource(`${manifest.version}-${manifest.codename}`, label)
  requireValue(manifest.latest === parsed.full || manifest.latest === `${parsed.full}-agent-xray`, `${label}: latest does not match version/codename`)
  requireValue(manifest.package === `zxy-panel-v${parsed.full}.zip`, `${label}: package does not match version/codename`)
  const url = new URL(manifest.download_url)
  requireValue(url.protocol === 'https:' && url.pathname.endsWith(`/releases/download/v${parsed.number}/${manifest.package}`), `${label}: download_url tag/package mismatch`)
  return parsed
}

function run() {
  const args = process.argv.slice(2)
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--help') {
      console.log('Usage: node scripts/check-version-consistency.mjs [--mode dev|release] [--root DIR] [--manifest FILE] [--package ZIP]')
      return
    }
    const key = args[i].replace(/^--/, '')
    requireValue(args[i].startsWith('--') && allowedOptions.has(key), `Unknown option: ${args[i]}`)
    requireValue(args[i + 1] && !args[i + 1].startsWith('--'), `Missing value for ${args[i]}`)
    options[key] = args[++i]
  }
  requireValue(['dev', 'release'].includes(options.mode), '--mode must be dev or release')
  requireValue(options.mode === 'release' || !options.package, '--package requires release mode')
  options.root = resolve(options.root)
  const text = (path) => readFileSync(resolve(options.root, path), 'utf8')
  const json = (path) => JSON.parse(text(path))
  const source = parseSource(text('VERSION').trim(), 'VERSION')
  const expect = (path, pattern, expected, label) => {
    const match = pattern.exec(text(path))
    requireValue(match, `${path}: missing ${label}`)
    requireValue(match[1] === expected, `${path}: ${label} is ${match[1]}, expected ${expected}`)
  }

  const frontendPackage = json('frontend/package.json')
  const frontendLock = json('frontend/package-lock.json')
  for (const [label, version] of [
    ['frontend/package.json', frontendPackage.version],
    ['frontend/package-lock.json', frontendLock.version],
    ['frontend/package-lock.json packages[""]', frontendLock.packages?.['']?.version]
  ]) {
    requireValue(version === source.number, `${label}: version is ${version}, expected ${source.number}`)
  }

  const displayName = source.codename.split('-').map((word) => word[0].toUpperCase() + word.slice(1)).join(' ')
  expect('frontend/src/version.ts', /export const APP_VERSION = ['"]([^'"]+)['"]/, `V${source.number} ${displayName}`, 'APP_VERSION')
  expect('backend/internal/api/system.go', /const panelVersion\s*=\s*"([^"]+)"/, source.full, 'panelVersion')
  expect('backend/cmd/server/main.go', /ZXY Panel API v([^\s"]+) listening/, source.full, 'startup version')
  expect('agent/cmd/agent/main.go', /const version\s*=\s*"([^"]+)"/, source.full, 'Agent version')
  expect('deploy/install.sh', /^VERSION="([^"]+)"/m, source.full, 'installer version')
  expect('backend/internal/api/tools.go', /User-Agent: ZXY-Panel\/([^\\]+)/, source.full, 'User-Agent version')
  const health = /"version"\s*:\s*(panelVersion|"[^"]+")/.exec(text('backend/internal/api/health.go'))
  requireValue(health && (health[1] === 'panelVersion' || health[1] === JSON.stringify(source.full)), 'backend/internal/api/health.go: version must match panelVersion')

  const views = ['Clients', 'Dashboard', 'Diagnostics', 'LandingExits', 'NetworkPolicy', 'Nodes', 'Relays', 'Servers', 'Updates']
  for (const view of views) {
    const path = `frontend/src/views/${view}.vue`
    const template = /<template>([\s\S]*)<\/template>/.exec(text(path))
    requireValue(template, `${path}: missing template`)
    const labels = [...template[1].replace(/<!--[\s\S]*?-->/g, '').matchAll(/\bV(\d+(?:\.\d+){2,})\b/g)]
    requireValue(labels.length > 0, `${path}: missing displayed version`)
    for (const label of labels) requireValue(label[1] === source.number, `${path}: displayed version is ${label[1]}, expected ${source.number}`)
  }
  expect('frontend/src/views/NetworkPolicy.vue', /compat:\s*'[^'\r\n]*\bV(\d+(?:\.\d+){2,})/, source.number, 'compatibility label version')
  expect('frontend/src/views/Servers.vue', /const PACKAGE_NAME = '([^']+)'/, `zxy-panel-v${source.full}.zip`, 'PACKAGE_NAME')
  expect('frontend/src/views/Servers.vue', /const PACKAGE_DIR = '([^']+)'/, `zxy-panel-v${source.full}`, 'PACKAGE_DIR')
  for (const path of ['backend/internal/api/system.go', 'backend/internal/api/node_diagnosis.go']) {
    for (const match of text(path).matchAll(/\bV(\d+(?:\.\d+){2,}) 安装脚本/g)) {
      requireValue(match[1] === source.number, `${path}: installer advice version is ${match[1]}, expected ${source.number}`)
    }
  }

  const buildScript = text('scripts/build-fast-release.sh')
  const buildVersion = /VERSION="\$\{1:-([\d.]+)\}"/.exec(buildScript)
  const buildCodename = /CODENAME="\$\{2:-([a-z0-9-]+)\}"/.exec(buildScript)
  if (buildVersion) requireValue(buildVersion[1] === source.number, 'scripts/build-fast-release.sh: stale default version')
  if (buildCodename) requireValue(buildCodename[1] === source.codename, 'scripts/build-fast-release.sh: stale default codename')
  expect('README.md', /^- 源码开发版本：`([^`]+)`/m, source.full, 'source development version')
  const newestChangelog = /^## V([^\s]+) ([^\s]+)(?:\s|$)/m.exec(text('CHANGELOG.md'))
  requireValue(newestChangelog && newestChangelog[1] === source.number && newestChangelog[2] === source.codename, 'CHANGELOG.md: newest entry must match VERSION; historical entries are ignored')

  const example = json('example.version.json')
  const exampleVersion = checkManifest(example, 'example.version.json')
  requireValue(exampleVersion.full === source.full && example.latest === source.full, 'example.version.json: example version must match VERSION')
  const published = json('version.json')
  checkManifest(published, 'version.json')
  expect('README.md', /^- 最新已发布版本：`([^`]+)`/m, published.latest, 'latest published version')
  const manifestPath = options.manifest ?? 'version.json'
  const manifest = options.manifest ? json(manifestPath) : published
  const manifestVersion = checkManifest(manifest, manifestPath)
  requireValue(/^[a-f0-9]{64}$/i.test(manifest.sha256), `${manifestPath}: sha256 must contain 64 hexadecimal characters`)

  if (options.mode === 'dev') {
    const comparison = compareNumbers(manifestVersion.number, source.number)
    requireValue(comparison <= 0, `${manifestPath}: published version is newer than the source version`)
    requireValue(comparison !== 0 || manifest.latest === source.full, `${manifestPath}: equal numeric versions must identify the same source build`)
    console.log(`PASS dev: source=${source.full}; published=${manifest.latest}`)
    console.log('INFO: historical changelog entries, old releases, persistence metadata and existing build artifacts are excluded.')
  } else {
    requireValue(manifest.latest === source.full && manifestVersion.full === source.full, `${manifestPath}: release version must exactly match VERSION (${source.full})`)
    const packagePath = resolve(options.root, options.package ?? `dist-release/${manifest.package}`)
    requireValue(basename(packagePath) === manifest.package && statSync(packagePath).isFile(), 'Release package must exist with the manifest package filename')
    const actualHash = createHash('sha256').update(readFileSync(packagePath)).digest('hex')
    requireValue(actualHash === manifest.sha256.toLowerCase(), 'Release package SHA256 does not match the manifest')
    console.log(`PASS release: source=${source.full}; package=${manifest.package}; sha256=${actualHash}`)
  }
}

try {
  run()
} catch (error) {
  console.error(`FAIL: ${error.message}`)
  process.exitCode = 1
}
