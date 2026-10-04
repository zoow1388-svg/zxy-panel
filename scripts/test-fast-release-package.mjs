import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'

const archive = process.argv[2]
if (!archive) {
  console.error('Usage: node scripts/test-fast-release-package.mjs <release.zip>')
  process.exit(2)
}

const python = process.env.PYTHON3 || 'python3'
const check = String.raw`
import json
import posixpath
import stat
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as archive:
    assert archive.testzip() is None, 'archive CRC failure'
    names = archive.namelist()
    assert len(names) == len(set(names)), 'duplicate archive entries'
    roots = {name.split('/')[0] for name in names}
    assert len(roots) == 1, 'expected one package root'
    root = roots.pop()
    for name in names:
        assert '\\' not in name and not name.startswith('/'), 'non-POSIX archive path'
        assert name == posixpath.normpath(name) and '..' not in name.split('/'), 'unsafe archive path'
        info = archive.getinfo(name)
        executable = name.endswith(('.sh', '/zxy-panel', '/zxy-netopt')) or '/bin/' in name
        assert info.create_system == 3, name + ': expected Unix creator metadata'
        assert stat.S_IMODE(info.external_attr >> 16) == (0o755 if executable else 0o644), name + ': incorrect file mode'
    executables = ['deploy/install.sh', 'deploy/agent-install.sh', 'scripts/zxy-panel',
                   'bin/zxy-panel-api-linux-amd64', 'bin/zxy-agent-linux-amd64']
    for path in executables:
        info = archive.getinfo(root + '/' + path)
        assert info.create_system == 3, path + ': executable mode requires Unix creator metadata'
        assert stat.S_IMODE(info.external_attr >> 16) == 0o755, path + ': expected 0755'
    html = archive.read(root + '/frontend/dist/index.html').decode('utf-8')
    from html.parser import HTMLParser
    class Assets(HTMLParser):
        def __init__(self):
            super().__init__()
            self.urls = []
        def handle_starttag(self, tag, attrs):
            attrs = dict(attrs)
            if tag == 'script' and 'src' in attrs:
                self.urls.append(attrs['src'])
            if tag == 'link' and attrs.get('rel') == 'stylesheet':
                self.urls.append(attrs['href'])
    assets = Assets()
    assets.feed(html)
    assert len(assets.urls) >= 2, 'missing frontend script or stylesheet'
    for url in assets.urls:
        assert url.startswith('/assets/'), 'unexpected frontend base path: ' + url
        assert root + '/frontend/dist' + url in names, 'missing referenced frontend asset'
    print(json.dumps({'result': 'PASS', 'entries': len(names), 'executableModes': len(executables),
                      'frontendAssets': assets.urls}))
`
const result = spawnSync(python, ['-c', check, resolve(archive)], { encoding: 'utf8' })
if (result.stdout) process.stdout.write(result.stdout)
if (result.stderr) process.stderr.write(result.stderr)
if (result.error) {
  console.error(result.error.message)
  process.exit(1)
}
process.exit(result.status ?? 1)
