import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { extname, resolve, sep } from 'node:path'
import { pathToFileURL } from 'node:url'

const options = {}
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i].replace(/^--/, '')
  assert(['url', 'playwright-module', 'output-dir', 'dist-dir'].includes(key) && process.argv[i + 1], 'Expected --url, --playwright-module, --output-dir and optional --dist-dir')
  options[key] = process.argv[i + 1]
}
assert(options.url && options['output-dir'], 'A local preview URL and an output directory are required')
const base = new URL(options.url)
assert(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'Only loopback previews are allowed; do not use a production panel')
const { chromium } = options['playwright-module']
  ? await import(pathToFileURL(resolve(options['playwright-module'])).href)
  : await import('playwright')
const output = resolve(options['output-dir'])
const dist = options['dist-dir'] && resolve(options['dist-dir'])
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ channel: 'chrome', headless: true })
const results = []
try {
  for (const viewport of [{ name: 'desktop', width: 1280, height: 900 }, { name: 'mobile', width: 390, height: 844 }]) {
    const context = await browser.newContext({ viewport })
    const page = await context.newPage()
    const pageErrors = []
    page.on('pageerror', error => pageErrors.push(error.message))
    await page.addInitScript(() => localStorage.setItem('zxy_token', 'synthetic-browser-fixture-token'))
    let items = [
      { id: 'srv_local', name: 'Local fixture', host: 'local.invalid', status: 'online' },
      { id: 'srv_remote', name: 'Remote fixture', host: 'remote.invalid', status: 'offline' },
    ]
    let deletes = 0
    let serverReads = 0
    let outcome = 409
    let conflict = 'Deletion blocked: a relay route still references this server.'
    await page.route('**/*', async route => {
      const request = route.request()
      const url = new URL(request.url())
      assert.equal(url.origin, base.origin, 'Fixture pages must not contact external services')
      const path = url.pathname
      if (path === '/api/dashboard') return route.fulfill({ json: { servers: items.length } })
      if (path === '/api/servers' && request.method() === 'GET') {
        serverReads++
        return route.fulfill({ json: items })
      }
      if (path === '/api/servers/srv_remote' && request.method() === 'DELETE') {
        deletes++
        if (outcome === 409) return route.fulfill({ status: 409, json: { error: conflict, code: 'server_delete_conflict' } })
        items = items.filter(server => server.id !== 'srv_remote')
        return route.fulfill({ json: { ok: true } })
      }
      if (path.startsWith('/api/')) throw new Error(`Unexpected fixture request: ${request.method()} ${path}`)
      if (!dist) return route.continue()
      const file = resolve(dist, path === '/servers' || path === '/' ? 'index.html' : decodeURIComponent(path).slice(1))
      assert(file.startsWith(dist + sep), 'Asset request escaped the local dist directory')
      const contentType = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[extname(file)]
      return route.fulfill({ path: file, contentType })
    })
    page.on('dialog', dialog => dialog.accept())
    try {
      await page.goto(new URL('/servers', base).href)
      const remote = page.locator('tbody tr').filter({ hasText: 'Remote fixture' })
      await remote.waitFor({ state: 'visible' })
      const alert = page.locator('.error')
      await remote.locator('button.danger').click()
      await alert.waitFor({ state: 'visible', timeout: 5000 })
      assert.equal(await alert.innerText(), conflict)
      assert.equal(await page.locator('tbody tr').count(), 2)
      assert.equal(serverReads, 1, 'A rejected delete must not reload or remove rows')
      assert.equal(deletes, 1)
      assert.equal(pageErrors.length, 0, `Unhandled error: ${pageErrors.join('; ')}`)
      const box = await alert.boundingBox()
      assert(box && box.height > 0, 'The conflict message must occupy visible layout space')
      await page.screenshot({ path: resolve(output, `${viewport.name}-409.png`), fullPage: true })

      conflict = 'Deletion blocked: a BBR action is still pending.'
      await remote.locator('button.danger').click()
      await page.waitForFunction(expected => document.querySelector('.error')?.textContent === expected, conflict)
      assert.equal(await page.locator('tbody tr').count(), 2)
      assert.equal(deletes, 2)
      assert.equal(serverReads, 1)

      outcome = 200
      await remote.locator('button.danger').click()
      await remote.waitFor({ state: 'detached' })
      assert.equal(await page.locator('tbody tr').count(), 1)
      assert.equal(await alert.count(), 0, 'A subsequent successful delete must clear the old conflict')
      assert.equal(serverReads, 2)
      assert.equal(deletes, 3)
      assert.equal(pageErrors.length, 0)
      results.push({ viewport: viewport.name, conflictShown: true, rejectedRowsPreserved: true, repeatedConflictUpdated: true, successClearsConflict: true })
    } catch (error) {
      await page.screenshot({ path: resolve(output, `${viewport.name}-failure.png`), fullPage: true })
      results.push({ viewport: viewport.name, failed: true, error: error.message, pageErrors })
      throw error
    } finally {
      await context.close()
    }
  }
  console.log('PASS: desktop/mobile HTTP 409 display, row preservation and subsequent successful deletion')
} finally {
  await writeFile(resolve(output, 'results.json'), JSON.stringify(results, null, 2))
  await browser.close()
}
