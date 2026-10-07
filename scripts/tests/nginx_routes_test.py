"""Finite Nginx routing model and isolated publication faults, NOT real Nginx."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from urllib.parse import urlsplit

REPO = Path(__file__).resolve().parents[2]


def locations(text):
    found = []
    lines = text.splitlines()
    for i, line in enumerate(lines):
        match = re.fullmatch(r"\s*location (.*?) \{\s*", line)
        if not match:
            continue
        end = i + 1
        while lines[end].strip() != "}":
            end += 1
        bits = match[1].split(" ", 1)
        modifier, pattern = bits if bits[0] in ("=", "^~", "~", "~*") else ("", match[1])
        found.append({"modifier": modifier, "pattern": pattern,
                      "body": "\n".join(lines[i + 1:end])})
    if not found:
        raise ValueError("No locations in fixture")
    return found


def route(text, request):
    """Only the declared flat locations/proxy forms used by these configs."""
    uri, query = urlsplit(request).path, urlsplit(request).query
    choices = locations(text)
    selected = next((c for c in choices if c["modifier"] == "=" and c["pattern"] == uri), None)
    capture = None
    if not selected:
        prefixes = [c for c in choices if c["modifier"] not in ("=", "~", "~*") and uri.startswith(c["pattern"])]
        best = max(prefixes, key=lambda c: len(c["pattern"])) if prefixes else None
        if best and best["modifier"] == "^~":
            selected = best
        else:
            for choice in choices:
                if choice["modifier"] in ("~", "~*"):
                    match = re.search(choice["pattern"], uri, re.I if choice["modifier"] == "~*" else 0)
                    if match:
                        selected, capture = choice, match
                        break
            selected = selected or best
    if not selected:
        raise ValueError("No explicit location for " + request)
    proxy = re.search(r"proxy_pass\s+([^;]+);", selected["body"])
    if not proxy:
        kind = "redirect" if "return 302" in selected["body"] else (
            "assets" if "/assets/" in selected["pattern"] else "spa")
        return kind, None, selected
    raw = proxy[1]
    dynamic = "$" in raw
    if capture:
        for index, value in enumerate(capture.groups(), 1):
            raw = raw.replace("$" + str(index), value or "")
    raw = raw.replace("$is_args", "?" if query else "").replace("$args", query)
    upstream = urlsplit(raw)
    if dynamic:
        declared = set(re.findall(r"upstream\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{", text))
        if upstream.hostname not in declared and not re.fullmatch(r"\d+\.\d+\.\d+\.\d+", upstream.hostname or ""):
            raise ValueError("Dynamic hostname has no declared upstream/resolver")
        forwarded = upstream.path + ("?" + upstream.query if upstream.query else "")
    elif not upstream.path:
        forwarded = request
    else:
        forwarded = upstream.path + uri[len(selected["pattern"]):] + ("?" + query if query else "")
    return "proxy", forwarded, selected


class NginxGuardrails(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="zxy-ops-nginx-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "app"
        self.config = self.root / "config"
        self.nginx = self.root / "nginx"
        self.bin = self.root / "bin"
        for folder in (self.app, self.config, self.nginx, self.bin):
            folder.mkdir()
        for command in ("bash", "python3", "date", "dirname", "grep", "head", "cut", "mkdir",
                        "rm", "cp", "mv", "chmod", "mktemp", "cat", "tr"):
            actual = shutil.which(command)
            self.assertIsNotNone(actual, "Existing tool required: " + command)
            (self.bin / command).symlink_to(actual)
        self.target = self.nginx / "zxy-panel.conf"
        self.info = self.config / "panel.info"
        self.info.write_text("PORT=49321\nWEB_BASE_PATH=reviewbase\nINSTALL_MODE=fast\nINSTALL_DIR=" + str(self.app) + "\n")
        (self.app / ".env").write_text("API_PORT=18088\nWEB_PORT=15173\n")
        source = (REPO / "deploy/install.sh").read_text(encoding="utf-8")
        self.assertEqual(source.count('\nmain "$@"'), 1)
        self.library = self.root / "install-library.sh"
        self.library.write_bytes(source.rsplit('\nmain "$@"', 1)[0].encode("utf-8"))
        self.calls = self.root / "calls.json"
        self.service = self.root / "service.json"
        self.calls.write_text("[]")
        self.service.write_text(json.dumps({"active": True, "enabled": True}))
        self.env = {"PATH": str(self.bin), "HOME": str(self.root), "TMPDIR": str(self.root),
                    "LAB": str(self.root), "APP_DIR": str(self.app), "CONFIG_DIR": str(self.config),
                    "PYTHONDONTWRITEBYTECODE": "1"}

    def snapshot(self, include_recovery=True):
        return {str(p.relative_to(self.root)): (hashlib.sha256(p.read_bytes()).hexdigest(),
                p.stat().st_mtime_ns, p.stat().st_mode)
                for folder in (self.app, self.config, self.nginx) for p in folder.rglob("*") if p.is_file()
                and (include_recovery or not any(part.startswith('.zxy-nginx-check-') for part in p.parts))}

    def shell(self, body, fault="", extra=""):
        stub = r'''
nginx() {
  python3 - "$LAB" nginx "$@" <<'PY_COMMAND'
import json, os, sys
from pathlib import Path
root = Path(sys.argv[1])
call = sys.argv[2:]
p = root / 'calls.json'
calls = json.loads(p.read_text())
calls.append(call)
p.write_text(json.dumps(calls))
fault = os.environ.get('FAULT', '')
kind = 'candidate' if '-c' in call else 'global'
if fault == kind:
    if kind == 'candidate' or sum(c[0] == 'nginx' and '-c' not in c for c in calls) == 1:
        raise SystemExit(2)
if '-c' in call:
    checked = Path(call[call.index('-c') + 1])
    assert checked.is_relative_to(root)
    assert checked.is_file()
    if fault == 'administrator-change':
        with (root / 'nginx/zxy-panel.conf').open('a') as changed:
            changed.write('\n# administrator change during validation\n')
raise SystemExit(0)
PY_COMMAND
}
systemctl() {
  python3 - "$LAB" systemctl "$@" <<'PY_SYSTEM'
import json, os, sys
from pathlib import Path
root = Path(sys.argv[1])
call = sys.argv[2:]
assert call[-1] == 'nginx'
p, statefile = root / 'calls.json', root / 'service.json'
calls, state = json.loads(p.read_text()), json.loads(statefile.read_text())
calls.append(call)
p.write_text(json.dumps(calls))
action = call[1]
if action == 'is-active':
    if '--quiet' not in call:
        print('active' if state['active'] else 'inactive')
    raise SystemExit(0 if state['active'] else 3)
if action == 'is-enabled':
    print('enabled' if state['enabled'] else 'disabled')
    raise SystemExit(0 if state['enabled'] else 1)
fault = os.environ.get('FAULT', '')
if fault in ('reload', 'start', 'enable') and action == fault:
    raise SystemExit(4)
if action in ('start', 'stop'):
    state['active'] = action == 'start'
elif action in ('enable', 'disable'):
    state['enabled'] = action == 'enable'
elif action != 'reload':
    raise SystemExit(91)
statefile.write_text(json.dumps(state))
PY_SYSTEM
}
'''
        env = dict(self.env, FAULT=fault)
        script = stub + '\nsource "' + str(self.library) + '"\n' + r'''
SRC_DIR="$LAB/source"
HOST_NGINX_FILE="$LAB/nginx/zxy-panel.conf"
INSTALL_MODE=fast
PANEL_PORT=49321
WEB_BASE_PATH=reviewbase
API_PORT=18088
WEB_PORT=15173
''' + "\n" + extra + "\n" + body
        result = subprocess.run(["bash", "-euc", script], env=env, capture_output=True, text=True)
        self.assertNotIn("command not found", result.stderr)
        return result

    def publish(self, mode="fast", fault=""):
        return self.shell("preflight_host_nginx; write_host_nginx_" + mode, fault=fault)

    def success(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("configuration validated and service active", result.stdout)

    def rendered(self, mode):
        self.success(self.publish(mode))
        content = self.target.read_text()
        self.target.unlink()
        self.calls.write_text("[]")
        return content

    def test_root_and_prefixed_api_queries(self):
        sources = {"fast": self.rendered("fast"), "host-docker": self.rendered("docker"),
                   "container": (REPO / "frontend/nginx.conf").read_text()}
        for mode, text in sources.items():
            for prefix in ("", "/reviewbase"):
                for endpoint in ("/api/health", "/api/nodes", "/sub/synthetic", "/s/synthetic/node"):
                    for query in ("", "?format=clash&download=1", "?value=a%26b%3Dc&name=%E4%B8%AD&empty=&dup=1&dup=2"):
                        request = prefix + endpoint + query
                        with self.subTest(mode=mode, request=request):
                            kind, forwarded, _ = route(text, request)
                            self.assertEqual(kind, "proxy")
                            self.assertEqual(forwarded, request if mode == "host-docker" else endpoint + query)

    def test_container_resolution_and_root_priority(self):
        text = (REPO / "frontend/nginx.conf").read_text()
        self.assertRegex(text, r"upstream\s+zxy_panel_api\s*\{\s*server\s+zxy-panel-api:8088;")
        for prefix in ("api", "sub", "s", "assets"):
            self.assertIn("location ^~ /" + prefix + "/", text)
        broken = re.sub(r"upstream\s+zxy_panel_api\s*\{.*?\}", "", text, flags=re.S)
        with self.assertRaisesRegex(ValueError, "no declared upstream"):
            route(broken, "/reviewbase/sub/synthetic?format=clash")
        with self.assertRaisesRegex(ValueError, "no declared upstream"):
            route(text.replace("http://zxy_panel_api/", "http://unknown-api:8088/"), "/reviewbase/api/health")

    def test_assets_and_spa_locations(self):
        for mode, text in (("fast", self.rendered("fast")),
                           ("container", (REPO / "frontend/nginx.conf").read_text())):
            for prefix in ("", "/reviewbase"):
                with self.subTest(mode=mode, prefix=prefix):
                    self.assertEqual(route(text, prefix + "/assets/index-synthetic.js")[0], "assets")
                    self.assertEqual(route(text, prefix + "/nodes")[0], "spa")
            self.assertEqual(route(text, "/index.html")[2]["modifier"], "=")

    def test_repeated_render_has_confirmed_scope(self):
        for mode in ("fast", "docker"):
            with self.subTest(mode=mode):
                self.success(self.publish(mode))
                before = self.target.read_text()
                self.success(self.publish(mode))
                self.assertEqual(self.target.read_text(), before)
                self.target.unlink()

    def test_candidate_validation_keeps_original_and_service(self):
        self.success(self.publish())
        self.calls.write_text("[]")
        before, state = self.snapshot(), self.service.read_bytes()
        result = self.publish(fault="candidate")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("candidate validation failed", result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(self.service.read_bytes(), state)
        self.assertTrue(all(c[0] == "nginx" for c in json.loads(self.calls.read_text())))

    def test_complete_config_failure_restores_contents_mtime_and_state(self):
        self.success(self.publish())
        self.calls.write_text("[]")
        before, state = self.snapshot(), self.service.read_bytes()
        result = self.publish(fault="global")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("publication failed", result.stderr)
        self.assertIn("actual service state=active", result.stderr)
        self.assertNotIn("configuration validated and service active", result.stdout)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(self.service.read_bytes(), state)
        self.assertFalse(list(self.nginx.glob(".zxy-nginx-check-*")))

    def test_reload_failure_returns_error_and_restores_file(self):
        self.success(self.publish())
        self.calls.write_text("[]")
        before = self.snapshot()
        result = self.publish(fault="reload")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rollback status=2", result.stderr)
        self.assertIn("actual service state=active", result.stderr)
        self.assertNotIn("configuration validated and service active", result.stdout)
        self.assertEqual(self.snapshot(include_recovery=False), before)
        copies = list(self.nginx.glob('.zxy-nginx-check-*/previous.conf'))
        self.assertEqual(len(copies), 1)
        self.assertEqual(copies[0].read_bytes(), self.target.read_bytes())

    def test_new_inactive_service_enable_start_faults_are_reverted(self):
        for fault in ("global", "enable", "start"):
            with self.subTest(fault=fault):
                self.service.write_text(json.dumps({"active": False, "enabled": False}))
                self.calls.write_text("[]")
                before = self.snapshot()
                result = self.publish(fault=fault)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.snapshot(), before)
                self.assertEqual(json.loads(self.service.read_text()), {"active": False, "enabled": False})
                self.assertIn("actual service state=inactive", result.stderr)
                self.assertNotIn("configuration validated and service active", result.stdout)
                if fault == "global":
                    self.assertFalse(any(c[0] == "systemctl" and c[1] in ("start", "stop", "enable", "disable") for c in json.loads(self.calls.read_text())))

    def test_unowned_configuration_never_replaced(self):
        for directive in ("listen 80;", "include /foreign/nginx.conf;",
                          "location /business/ {", "proxy_pass http://foreign.invalid/;"):
            with self.subTest(directive=directive):
                self.target.write_text("server {\n" + directive + "\n}\n")
                before = self.snapshot()
                self.calls.write_text("[]")
                result = self.publish()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("outside the confirmed panel scope", result.stderr)
                self.assertEqual(self.snapshot(), before)
                self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_same_port_without_panel_layout_is_not_owned(self):
        self.target.write_text('server {\nlisten 49321;\nserver_name _;\nclient_max_body_size 20m;\n}\n')
        before = self.snapshot()
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('outside the confirmed panel scope', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_required_proxy_must_match_its_own_port_and_segment(self):
        content = self.rendered('fast')
        for wrong in ('http://127.0.0.1:15173/api/', 'http://127.0.0.1:18088/sub/'):
            with self.subTest(wrong=wrong):
                self.target.write_text(content.replace('http://127.0.0.1:18088/api/', wrong, 1))
                before = self.snapshot()
                result = self.publish()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.snapshot(), before)
                self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_legacy_generated_layouts_are_accepted(self):
        fast = self.rendered('fast')
        for segment in ('api', 'sub', 's'):
            fast = fast.replace('location ^~ /reviewbase/' + segment + '/ {',
                                'location ~ ^/reviewbase/' + segment + '/(.*)$ {')
            marker = 'location ~ ^/reviewbase/' + segment + '/(.*)$ {'
            start = fast.index(marker)
            end = fast.index('\n    }', start)
            block = fast[start:end].replace('/' + segment + '/;', '/' + segment + '/$1;')
            fast = fast[:start] + block + fast[end:]
        self.target.write_text(fast)
        self.success(self.publish('fast'))
        self.target.unlink()
        docker = self.rendered('docker')
        start = docker.index('    location / {')
        end = docker.index('\n    }', start) + len('\n    }')
        self.target.write_text(docker[:start] + docker[end:])
        self.success(self.publish('docker'))

    def test_candidate_read_failure_under_conditional_call_stops_before_validation(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        before, state = self.snapshot(), self.service.read_bytes()
        extra = '''cat() {
  case "${1:-}" in */.zxy-panel-candidate-*) return 73;; esac
  command cat "$@"
}'''
        result = self.shell('preflight_host_nginx; if write_host_nginx_fast; then exit 99; else exit $?; fi', extra=extra)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotEqual(result.returncode, 99)
        self.assertIn('candidate could not be read', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(self.service.read_bytes(), state)
        self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_change_during_validation_is_not_overwritten(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        before = self.target.read_bytes()
        state = self.service.read_bytes()
        result = self.publish(fault='administrator-change')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('changed during validation', result.stderr)
        self.assertEqual(self.target.read_bytes(), before + b'\n# administrator change during validation\n')
        self.assertEqual(self.service.read_bytes(), state)
        self.assertFalse(any(c[0] == 'systemctl' and c[1] not in ('is-active', 'is-enabled')
                             for c in json.loads(self.calls.read_text())))

    def test_mismatched_recovery_copy_blocks_publication(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        before, state = self.snapshot(), self.service.read_bytes()
        extra = '''cp() {
  command cp "$@" || return
  case "${@: -1}" in */previous.conf) printf '\\n# corrupted copy\\n' >> "${@: -1}";; esac
}'''
        result = self.shell('preflight_host_nginx; write_host_nginx_fast', extra=extra)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('recovery copy does not match', result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(self.service.read_bytes(), state)
        self.assertFalse(any(c[0] == 'systemctl' and c[1] not in ('is-active', 'is-enabled')
                             for c in json.loads(self.calls.read_text())))

    def test_late_target_change_during_backup_check_is_not_overwritten(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        before, state = self.target.read_bytes(), self.service.read_bytes()
        extra = '''definition=$(declare -f nginx_file_fingerprint)
eval "${definition/nginx_file_fingerprint/original_fingerprint}"
nginx_file_fingerprint() {
  case "$1" in */previous.conf) printf '\\n# late administrator change\\n' >> "$HOST_NGINX_FILE";; esac
  original_fingerprint "$@"
}'''
        result = self.shell('preflight_host_nginx; write_host_nginx_fast', extra=extra)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('changed during validation', result.stderr)
        self.assertEqual(self.target.read_bytes(), before + b'\n# late administrator change\n')
        self.assertEqual(self.service.read_bytes(), state)
        self.assertFalse(any(c[0] == 'systemctl' and c[1] not in ('is-active', 'is-enabled')
                             for c in json.loads(self.calls.read_text())))

    def test_published_fingerprint_failure_keeps_copy_and_performs_no_service_action(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        old = self.target.read_bytes(), self.target.stat().st_mtime_ns
        state = self.service.read_bytes()
        extra = '''mv() {
  command mv "$@" || return
  printf 'published' > "$LAB/published-marker"
}
definition=$(declare -f nginx_file_fingerprint)
eval "${definition/nginx_file_fingerprint/original_fingerprint}"
nginx_file_fingerprint() {
  if [[ "$1" == "$HOST_NGINX_FILE" && -f "$LAB/published-marker" ]]; then return 75; fi
  original_fingerprint "$@"
}'''
        result = self.shell('preflight_host_nginx; write_host_nginx_fast', extra=extra)
        self.assertEqual(result.returncode, 2)
        self.assertIn('published Nginx file could not be confirmed', result.stderr)
        self.assertIn('actual service state=active', result.stderr)
        self.assertNotIn('configuration validated and service active', result.stdout)
        copies = list(self.nginx.glob('.zxy-nginx-check-*/previous.conf'))
        self.assertEqual(len(copies), 1)
        self.assertEqual((copies[0].read_bytes(), copies[0].stat().st_mtime_ns), old)
        self.assertEqual(self.service.read_bytes(), state)
        self.assertFalse(any(c[0] == 'systemctl' and c[1] not in ('is-active', 'is-enabled')
                             for c in json.loads(self.calls.read_text())))
        self.assertFalse(any(c[0] == 'nginx' and '-c' not in c for c in json.loads(self.calls.read_text())))

    def test_file_rollback_failure_keeps_recovery_copy_and_does_not_reload(self):
        self.success(self.publish())
        self.calls.write_text('[]')
        old = self.target.read_bytes(), self.target.stat().st_mtime_ns
        state = self.service.read_bytes()
        extra = '''mv() {
  case "$1" in */restore.conf) return 74;; esac
  command mv "$@"
}'''
        result = self.shell('preflight_host_nginx; write_host_nginx_fast', fault='global', extra=extra)
        self.assertEqual(result.returncode, 2)
        self.assertIn('file rollback incomplete', result.stderr)
        self.assertIn('actual service state=active', result.stderr)
        copies = list(self.nginx.glob('.zxy-nginx-check-*/previous.conf'))
        self.assertEqual(len(copies), 1)
        self.assertEqual((copies[0].read_bytes(), copies[0].stat().st_mtime_ns), old)
        self.assertEqual(self.service.read_bytes(), state)
        calls = json.loads(self.calls.read_text())
        self.assertEqual(sum(c[0] == 'nginx' and '-c' not in c for c in calls), 1)
        self.assertFalse(any(c[:2] == ['systemctl', 'reload'] for c in calls))

    def test_symbolic_link_never_follows_foreign_target(self):
        foreign = self.root / "foreign.conf"
        foreign.write_text("unowned-data")
        before = foreign.read_bytes(), foreign.stat().st_mtime_ns
        self.target.symlink_to(foreign)
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not be a symlink", result.stderr)
        self.assertEqual((foreign.read_bytes(), foreign.stat().st_mtime_ns), before)
        self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_file_changed_after_preflight_is_not_overwritten(self):
        self.success(self.publish())
        self.calls.write_text("[]")
        altered = self.target.read_text() + "\n# external administrator change\n"
        result = self.shell('preflight_host_nginx; printf "\\n# external administrator change\\n" >> "$HOST_NGINX_FILE"; write_host_nginx_fast')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("changed after preflight", result.stderr)
        self.assertEqual(self.target.read_text(), altered)
        self.assertEqual(json.loads(self.calls.read_text()), [])

    def test_main_ownership_check_precedes_backup_and_cleanup(self):
        main = (REPO / "deploy/install.sh").read_text().split("\nmain() {", 1)[1]
        self.assertLess(main.index("preflight_host_nginx"), main.index("installer_backup_existing"))
        self.assertLess(main.index("preflight_host_nginx"), main.index("cleanup_old_runtime"))

    def test_web_root_with_spaces_is_quoted_and_repeatable(self):
        previous = self.app
        self.app = self.root / "app with spaces"
        previous.rename(self.app)
        self.env["APP_DIR"] = str(self.app)
        self.info.write_text("PORT=49321\nWEB_BASE_PATH=reviewbase\nINSTALL_MODE=fast\nINSTALL_DIR=" + str(self.app) + "\n")
        self.success(self.publish())
        self.assertIn('root "' + str(self.app / "frontend/dist") + '";', self.target.read_text())
        self.success(self.publish())


if __name__ == "__main__":
    unittest.main(verbosity=2)
