import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const project = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const checker = join(project, 'scripts/check-version-consistency.mjs')
const fixturePaths = [
  'VERSION', 'README.md', 'CHANGELOG.md', 'example.version.json', 'version.json',
  'frontend/package.json', 'frontend/package-lock.json', 'frontend/src/version.ts', 'frontend/src/views',
  'backend/cmd/server/main.go', 'backend/internal/api/system.go', 'backend/internal/api/health.go',
  'backend/internal/api/tools.go', 'backend/internal/api/node_diagnosis.go',
  'agent/cmd/agent/main.go', 'deploy/install.sh', 'scripts/build-fast-release.sh'
]

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'zxy-version-consistency-'))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  for (const path of fixturePaths) {
    mkdirSync(dirname(join(root, path)), { recursive: true })
    cpSync(join(project, path), join(root, path), { recursive: true })
  }
  return root
}

function check(root, ...args) {
  const result = spawnSync(process.execPath, [checker, '--root', root, ...args], { encoding: 'utf8' })
  assert.ifError(result.error)
  return { status: result.status, output: result.stdout + result.stderr }
}

function mutate(root, path, transform) {
  const file = join(root, path)
  const previous = readFileSync(file, 'utf8')
  const next = transform(previous)
  assert.notEqual(next, previous, `Fixture mutation must change ${path}`)
  writeFileSync(file, next)
}

function candidate(root) {
  const manifest = JSON.parse(readFileSync(join(root, 'example.version.json'), 'utf8'))
  const packagePath = join(root, 'dist-release', manifest.package)
  mkdirSync(dirname(packagePath), { recursive: true })
  const bytes = Buffer.from('release package hash test fixture')
  writeFileSync(packagePath, bytes)
  manifest.sha256 = createHash('sha256').update(bytes).digest('hex')
  writeFileSync(join(root, 'candidate-manifest.json'), JSON.stringify(manifest))
  return { manifest, packagePath }
}

test('development permits an older release manifest and ignores historical records', (t) => {
  const root = fixture(t)
  mutate(root, 'CHANGELOG.md', (text) => `${text}\n## V0.1.0 historical\nOld release notes.\n`)
  mkdirSync(join(root, 'releases', 'v0.1.0'), { recursive: true })
  writeFileSync(join(root, 'releases', 'v0.1.0', 'version.json'), 'not valid JSON')
  const result = check(root)
  assert.equal(result.status, 0, result.output)
  assert.match(result.output, /PASS dev/)
})

test('detects a stale backend runtime version', (t) => {
  const root = fixture(t)
  mutate(root, 'backend/internal/api/system.go', (text) => text.replace(/const panelVersion = "[^"]+"/, 'const panelVersion = "0.7.7.6-bbr-optimization-agent-xray"'))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /system\.go: panelVersion/)
})

test('detects a stale independent health version', (t) => {
  const root = fixture(t)
  mutate(root, 'backend/internal/api/health.go', (text) => text.replace('"version": panelVersion', '"version": "0.7.7.1-clash-import-polish-agent-xray"'))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /health\.go: version/)
})

test('detects a stale Agent version without inspecting protocol fields', (t) => {
  const root = fixture(t)
  mutate(root, 'agent/cmd/agent/main.go', (text) => text.replace(/const version = "[^"]+"/, 'const version = "0.7.7.6-bbr-optimization-agent-xray"'))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /Agent version/)
})

test('detects a stale frontend lockfile root version', (t) => {
  const root = fixture(t)
  mutate(root, 'frontend/package-lock.json', (text) => {
    const lock = JSON.parse(text)
    lock.packages[''].version = '0.7.7.6'
    return JSON.stringify(lock)
  })
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /packages\[""\]/)
})

test('detects a stale visible dashboard version', (t) => {
  const root = fixture(t)
  mutate(root, 'frontend/src/views/Dashboard.vue', (text) => text.replace(/ZXY Panel V[\d.]+/, 'ZXY Panel V0.7.7.2'))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /Dashboard\.vue: displayed version/)
})

test('detects a stale Agent installation package filename', (t) => {
  const root = fixture(t)
  mutate(root, 'frontend/src/views/Servers.vue', (text) => text.replace(/const PACKAGE_NAME = '[^']+'/, "const PACKAGE_NAME = 'zxy-panel-v0.7.7.6-bbr-optimization.zip'"))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /PACKAGE_NAME/)
})

test('detects a stale hardcoded build codename', (t) => {
  const root = fixture(t)
  mutate(root, 'scripts/build-fast-release.sh', (text) => text.replace('CODENAME="${2:-$SOURCE_CODENAME}"', 'CODENAME="${2:-bbr-optimization}"'))
  const result = check(root)
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /stale default codename/)
})

test('development rejects a newer published version using numeric comparison', (t) => {
  const root = fixture(t)
  candidate(root)
  mutate(root, 'candidate-manifest.json', (text) => text.replaceAll('0.7.8', '0.7.10'))
  const result = check(root, '--mode', 'dev', '--manifest', 'candidate-manifest.json')
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /published version is newer/)
})

test('release rejects the unchanged older published manifest', (t) => {
  const result = check(fixture(t), '--mode', 'release')
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /release version must exactly match VERSION/)
})

test('release accepts a matching candidate manifest and actual package hash', (t) => {
  const root = fixture(t)
  const { packagePath } = candidate(root)
  const result = check(root, '--mode', 'release', '--manifest', 'candidate-manifest.json', '--package', packagePath)
  assert.equal(result.status, 0, result.output)
  assert.match(result.output, /PASS release/)
})

test('release rejects a missing package', (t) => {
  const root = fixture(t)
  const { packagePath } = candidate(root)
  rmSync(packagePath)
  const result = check(root, '--mode', 'release', '--manifest', 'candidate-manifest.json')
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /ENOENT/)
})

test('release rejects a package whose bytes no longer match SHA256', (t) => {
  const root = fixture(t)
  const { packagePath } = candidate(root)
  writeFileSync(packagePath, 'changed release package bytes')
  const result = check(root, '--mode', 'release', '--manifest', 'candidate-manifest.json')
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /SHA256 does not match/)
})

test('release rejects a download URL pointing to a different tag', (t) => {
  const root = fixture(t)
  candidate(root)
  mutate(root, 'candidate-manifest.json', (text) => text.replace('/download/v0.7.8/', '/download/v0.7.7.5/'))
  const result = check(root, '--mode', 'release', '--manifest', 'candidate-manifest.json')
  assert.equal(result.status, 1, result.output)
  assert.match(result.output, /download_url tag\/package mismatch/)
})
