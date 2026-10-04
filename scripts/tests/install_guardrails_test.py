"""Isolated installer guardrails. No full install or real system command is run."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]


class InstallGuardrails(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="zxy-ops-a-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "app"
        self.config = self.root / "config"
        self.source = self.root / "source"
        self.bin = self.root / "bin"
        for p in (self.app, self.config, self.source, self.bin):
            p.mkdir()
        for command in ("bash", "python3", "date", "dirname", "grep", "head", "cut", "mkdir", "rm", "cp", "chmod", "mktemp", "tr"):
            actual = shutil.which(command)
            self.assertIsNotNone(actual, "Required existing tool: " + command)
            (self.bin / command).symlink_to(actual)
        self.trace = self.root / "trace"
        self.trace.write_text("", encoding="utf-8")
        self.env = {"PATH": str(self.bin), "HOME": str(self.root), "TMPDIR": str(self.root),
                    "APP_DIR": str(self.app), "CONFIG_DIR": str(self.config),
                    "TRACE": str(self.trace), "LAB": str(self.root),
                    "PYTHONDONTWRITEBYTECODE": "1"}
        self.info("fast")
        self.env_file()
        self.libs = {}
        for name, rel in (("install", "deploy/install.sh"), ("agent", "deploy/agent-install.sh")):
            text = (REPO / rel).read_text(encoding="utf-8")
            self.assertEqual(text.count('\nmain "$@"'), 1)
            text = text.rsplit('\nmain "$@"', 1)[0]
            path = self.root / (name + "-library.sh")
            path.write_bytes(text.encode("utf-8"))
            self.libs[name] = path
        cli = (REPO / "scripts/zxy-panel").read_text(encoding="utf-8")
        prefix, boundary, _ = cli.partition('\ncase "${1:-}" in')
        self.assertTrue(boundary)
        prefix = prefix.replace('CONFIG_DIR="/etc/zxy-panel"', 'CONFIG_DIR="${CONFIG_DIR:?}"', 1)
        prefix = prefix.replace('APP_DIR="/opt/zxy-panel"', 'APP_DIR="${APP_DIR:?}"', 1)
        self.libs["cli"] = self.root / "cli-library.sh"
        self.libs["cli"].write_bytes(prefix.encode("utf-8"))

    def info(self, mode):
        (self.config / "panel.info").write_text(
            "INSTALL_DIR=" + str(self.app) + "\nINSTALL_MODE=" + mode +
            "\nPORT=42698\nWEB_BASE_PATH=existingbase\nUSERNAME=synthetic-user\n"
            "PASSWORD=synthetic-password\nAPI_TOKEN=synthetic-shared\n", encoding="utf-8")

    def env_file(self, extra=""):
        (self.app / ".env").write_text(
            "API_PORT=9088\nWEB_PORT=9173\nWEB_BASE_PATH=existingbase\n"
            "ZXY_JWT_SECRET=synthetic-jwt\nZXY_AGENT_SHARED_SECRET=synthetic-shared\n"
            "ZXY_ADMIN_USERNAME=synthetic-user\nZXY_ADMIN_PASSWORD=synthetic-password\n"
            "ZXY_DB_PATH=" + str(self.app / "data/zxy-panel.json") +
            "\nZXY_LOCAL_SERVER_IP=127.0.0.1\nZXY_LOCAL_SERVER_HOST=127.0.0.1\nEXTENSION=keep-me\n" + extra,
            encoding="utf-8")

    def agent_file(self, extra=""):
        database = self.app / "data/zxy-panel.json"
        database.parent.mkdir(exist_ok=True)
        if not database.exists():
            database.write_text(json.dumps({"servers": {"srv-synthetic": {
                "id": "srv-synthetic", "agent_token": "synthetic-agent-token"}}}))
        (self.config / "agent.env").write_text(
            "ZXY_PANEL_BASE=http://127.0.0.1:9088\nZXY_SERVER_ID=srv-synthetic\n"
            "ZXY_AGENT_TOKEN=synthetic-agent-token\nZXY_APPLY_CONFIG=false\n"
            "ZXY_XRAY_CONFIG=" + str(self.config / "xray/config.json") +
            "\nZXY_XRAY_TEST_CMD=xray run -test -config {config}\n"
            "ZXY_XRAY_RELOAD_CMD=systemctl restart xray\nZXY_AGENT_INTERVAL_SECONDS=67\n"
            "EXTRA_AGENT=retained\n" + extra, encoding="utf-8")

    def api_unit(self, directory=None):
        p = self.root / "api.service"
        p.write_text("[Service]\nWorkingDirectory=" + str(directory or self.app) +
                     "\nEnvironmentFile=" + str(self.app / ".env") +
                     "\nExecStart=" + str(self.app / "bin/zxy-panel-api-linux-amd64") + "\n",
                     encoding="utf-8")

    def agent_unit(self):
        (self.root / "agent.service").write_text(
            "[Service]\nEnvironmentFile=" + str(self.config / "agent.env") +
            "\nExecStart=/usr/local/bin/zxy-agent\n", encoding="utf-8")

    def fast_assets(self):
        for rel in ("bin/zxy-panel-api-linux-amd64", "bin/zxy-agent-linux-amd64", "frontend/dist/index.html"):
            p = self.source / rel
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(b"synthetic-asset")
            p.chmod(0o755)

    def docker_fixture(self):
        payloads = {}
        for name in ("zxy-panel-api", "zxy-panel-frontend"):
            payloads[name] = [{"Id": "owned-" + name, "Config": {"Labels": {
                "com.docker.compose.service": name,
                "com.docker.compose.project": "synthetic-project",
                "com.docker.compose.project.working_dir": str(self.app),
                "com.docker.compose.project.config_files": str(self.app / "docker-compose.yml")}},
                "Mounts": [{"Type": "bind", "Source": str(self.app / "data"), "Destination": "/app/data"}]}]
            (self.root / (name + ".json")).write_text(json.dumps(payloads[name]))
        setup = r'''
docker() {
  printf 'docker %s\n' "$*" >> "$TRACE"
  case "$1" in
    context) echo default ;;
    ps) printf 'zxy-panel-api\nzxy-panel-frontend\nforeign-zxy-panel-business\n' ;;
    inspect) python3 -c 'import pathlib,sys;print(pathlib.Path(sys.argv[1]).read_text())' "$LAB/$2.json" ;;
    compose)
      if [[ "$2" == version ]]; then echo synthetic-compose; return; fi
      python3 - "$LAB/compose-args.json" "$@" <<'PY_ARGS'
import json, os, sys
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps({
    "argv": sys.argv[2:],
    "remove_orphans": os.environ.get("COMPOSE_REMOVE_ORPHANS"),
    "profiles": os.environ.get("COMPOSE_PROFILES")}))
PY_ARGS
      ;;
    *) return 99 ;;
  esac
}
'''
        return payloads, setup

    def snapshot(self):
        return {str(p.relative_to(self.root)): (hashlib.sha256(p.read_bytes()).hexdigest(),
                p.stat().st_mtime_ns, p.stat().st_mode)
                for folder in (self.app, self.config) for p in folder.rglob("*") if p.is_file()}

    def shell(self, library, body, updates=None, setup=""):
        env = dict(self.env)
        env.update(updates or {})
        prefix = r'''
systemctl() {
  if [[ "$1" != show ]]; then printf 'systemctl %s\n' "$*" >> "$TRACE"; return; fi
  python3 - "$LAB" "$2" "$4" <<'PY_SHOW'
import json, shlex, sys
from pathlib import Path
root, unit, key = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
overrides = root / 'unit-overrides.json'
custom = json.loads(overrides.read_text()) if overrides.exists() else {}
if unit in custom and key in custom[unit]:
    print(custom[unit][key])
    raise SystemExit()
name = {'zxy-panel-api': 'api.service', 'zxy-agent': 'agent.service',
        'xray': 'xray-base.service'}.get(unit, 'absent.service')
path = root / name
if key == 'LoadState':
    print('loaded' if path.exists() else 'not-found')
elif key == 'FragmentPath':
    print(str(path) if path.exists() else '')
elif key == 'DropInPaths':
    dropin = root / 'xray-dropin.conf'
    print(str(dropin) if unit == 'xray' and dropin.exists() else '')
elif path.exists():
    source = root / 'xray-dropin.conf' if unit == 'xray' else path
    values = {}
    for line in source.read_text().splitlines():
        if '=' in line:
            field, value = line.split('=', 1)
            values[field] = value
    if key == 'ExecStart':
        parts = shlex.split(values.get('ExecStart', ''))
        print('{ path=' + parts[0] + ' ; argv[]=' + ' '.join(parts) + ' ; ignore_errors=no ; }' if parts else '')
    elif key == 'EnvironmentFiles':
        print(' '.join(shlex.split(values.get('EnvironmentFile', ''))) + ' (ignore_errors=no)')
    else:
        print(' '.join(shlex.split(values.get(key, ''))))
else:
    print('')
PY_SHOW
}
docker() { printf 'docker %s\n' "$*" >> "$TRACE"; return 99; }
curl() { printf 'forbidden-curl\n' >> "$TRACE"; return 99; }
nginx() { printf 'forbidden-nginx\n' >> "$TRACE"; return 99; }
apt-get() { printf 'forbidden-apt\n' >> "$TRACE"; return 99; }
uname() { if [[ "$1" == -m ]]; then echo x86_64; else echo Linux; fi; }
'''
        script = prefix + '\nsource "' + str(self.libs[library]) + '"\n' + r'''
SRC_DIR="$LAB/source"
ROOT_DIR="$LAB/source"
API_UNIT_FILE="$LAB/api.service"
AGENT_UNIT_FILE="$LAB/agent.service"
XRAY_DROPIN="$LAB/xray-dropin.conf"
random_unused_port() { echo 42698; }
public_ip() { echo 127.0.0.1; }
''' + setup + "\n" + body
        result = subprocess.run(["bash", "-euc", script], env=env, capture_output=True, text=True)
        self.assertNotIn("command not found", result.stderr, "Fixture is missing a required existing command")
        self.assertNotIn("forbidden-", self.trace.read_text(), "Unexpected system/network operation")
        return result

    def ok(self, *args, **kwargs):
        result = self.shell(*args, **kwargs)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def bad(self, *args, **kwargs):
        before = self.snapshot()
        result = self.shell(*args, **kwargs)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.snapshot(), before, "Rejected input changed protected files")
        return result

    def test_existing_env_beats_script_defaults(self):
        self.ok("install", 'INSTALL_MODE=fast; resolve_existing_config; [[ "$API_PORT" == 9088 && "$WEB_PORT" == 9173 && "$JWT_SECRET" == synthetic-jwt && "$WEB_BASE_PATH" == existingbase ]]')

    def test_real_explicit_input_beats_existing(self):
        self.ok("install", 'preflight_inputs; INSTALL_MODE=fast; resolve_existing_config; [[ "$API_PORT" == 10088 && "$ADMIN_USERNAME" == explicit-user ]]',
                updates={"API_PORT": "10088", "ZXY_ADMIN_USERNAME": "explicit-user"})

    def test_empty_and_invalid_explicit_values_rejected(self):
        for key, value in (("API_PORT", ""), ("WEB_PORT", "0"), ("API_PORT", "65536"),
                           ("PANEL_PORT", "bad"), ("WEB_BASE_PATH", "a/../b"), ("AUTO_AGENT", "yes"),
                           ("APP_DIR", ""), ("ZXY_ADMIN_PASSWORD", "")):
            with self.subTest(key=key, value=value):
                self.bad("install", "preflight_inputs", updates={key: value})

    def test_duplicate_env_rejected_without_mutation(self):
        self.env_file("API_PORT=10088\n")
        self.bad("install", "INSTALL_MODE=fast; resolve_existing_config")

    def test_invalid_existing_port_is_not_defaulted(self):
        self.env_file()
        p = self.app / ".env"
        p.write_text(p.read_text().replace("API_PORT=9088", "API_PORT=broken"))
        self.bad("install", "INSTALL_MODE=fast; resolve_existing_config")

    def test_fast_external_db_pointer_preserved(self):
        p = self.app / ".env"
        p.write_text(p.read_text().replace(str(self.app / "data/zxy-panel.json"), str(self.root / "external/panel.json")))
        self.ok("install", 'INSTALL_MODE=fast; resolve_existing_config; [[ "$DB_PATH" == "$LAB/external/panel.json" ]]')
        self.bad("install", "INSTALL_MODE=docker; resolve_existing_config")

    def test_cross_mode_rejected_even_when_fresh(self):
        self.info("docker")
        self.fast_assets()
        for fresh in ("false", "true"):
            with self.subTest(fresh=fresh):
                self.bad("install", "selected_install_mode", updates={"ZXY_INSTALL_MODE": "fast", "FRESH_INSTALL": fresh})

    def test_auto_preserves_existing_docker(self):
        self.info("docker")
        self.fast_assets()
        self.assertEqual(self.ok("install", "selected_install_mode").stdout.strip(), "docker")

    def test_auto_preserves_existing_fast(self):
        self.fast_assets()
        self.assertEqual(self.ok("install", "selected_install_mode").stdout.strip(), "fast")

    def test_invalid_and_missing_fast_assets_rejected(self):
        self.bad("install", "selected_install_mode", updates={"ZXY_INSTALL_MODE": "invalid"})
        self.bad("install", "selected_install_mode", updates={"ZXY_INSTALL_MODE": "fast"})

    def test_new_auto_asset_selection(self):
        (self.config / "panel.info").unlink()
        (self.app / ".env").unlink()
        self.assertEqual(self.ok("install", "selected_install_mode").stdout.strip(), "docker")
        self.fast_assets()
        self.assertEqual(self.ok("install", "selected_install_mode").stdout.strip(), "fast")

    def test_no_identity_inferred_from_unowned_api_unit(self):
        self.info("")
        self.api_unit(self.root / "foreign")
        self.bad("install", "selected_install_mode")

    def test_architecture_rejection_has_no_cleanup(self):
        self.bad("install", "preflight_inputs", setup="uname() { echo aarch64; }")
        self.assertEqual(self.trace.read_text(), "")

    def test_sync_keeps_config_logs_data_backups_and_custom_db(self):
        actual_rsync = shutil.which("rsync")
        self.assertIsNotNone(actual_rsync, "Required existing rsync for the real synchronization branch")
        for mode in ("fast", "docker"):
            for rsync in (True, False):
                with self.subTest(mode=mode, rsync=rsync):
                    link = self.bin / "rsync"
                    if link.exists(): link.unlink()
                    if rsync: link.symlink_to(actual_rsync)
                    for rel in ("logs/activity.log", "data/zxy-panel.json", "backups/old.tar.gz", "custom/db.json"):
                        p = self.app / rel
                        p.parent.mkdir(parents=True, exist_ok=True)
                        if not p.exists(): p.write_bytes(b"protected")
                    for rel in (".env", "logs/activity.log", "data/zxy-panel.json", "backups/old.tar.gz"):
                        p = self.source / rel
                        p.parent.mkdir(parents=True, exist_ok=True)
                        p.write_bytes(b"must-not-overwrite")
                    before = self.snapshot()
                    (self.source / "VERSION").write_text("synthetic-package")
                    self.ok("install", 'INSTALL_MODE=' + mode + '; DB_PATH="$APP_DIR/custom/db.json"; copy_package_files')
                    current = self.snapshot()
                    for rel, state in before.items(): self.assertEqual(current[rel], state)
                    self.assertEqual((self.app / "VERSION").read_text(), "synthetic-package")
                    (self.app / "VERSION").unlink()

    def test_atomic_env_write_keeps_extensions_and_round_trips_quotes(self):
        self.ok("install", 'INSTALL_MODE=fast; resolve_existing_config; ADMIN_PASSWORD="$SPECIAL"; write_env; [[ "$(env_file_value ZXY_ADMIN_PASSWORD)" == "$SPECIAL" ]]',
                updates={"SPECIAL": "synthetic's $literal \\ path"})
        self.assertIn("EXTENSION=keep-me", (self.app / ".env").read_text())
        self.assertEqual((self.app / ".env").stat().st_mode & 0o777, 0o600)

    def test_env_replace_failure_preserves_full_snapshot(self):
        hook = self.root / "fault-hook"
        hook.mkdir()
        (hook / "sitecustomize.py").write_text(
            "import os\n_original=os.replace\n"
            "def _replace(src,dst,*a,**kw):\n"
            "    if str(dst)==os.environ.get('FAIL_TARGET'): raise OSError('synthetic injected failure')\n"
            "    return _original(src,dst,*a,**kw)\nos.replace=_replace\n")
        self.bad("install", "INSTALL_MODE=fast; resolve_existing_config; write_env",
                 updates={"PYTHONPATH": str(hook), "FAIL_TARGET": str(self.app / ".env")})
        self.assertEqual(list(self.app.glob("tmp*")), [])

    def test_unowned_unit_blocks_before_stop(self):
        self.api_unit(self.root / "foreign")
        self.bad("install", "INSTALL_MODE=fast; AUTO_AGENT=false; preflight_runtime_ownership")
        self.assertEqual(self.trace.read_text(), "")

    def test_auto_agent_false_never_stops_existing_agent(self):
        self.api_unit()
        self.agent_file()
        self.agent_unit()
        self.ok("install", "INSTALL_MODE=fast; AUTO_AGENT=false; cleanup_old_runtime")
        self.assertIn("systemctl stop zxy-panel-api", self.trace.read_text())
        self.assertNotIn("zxy-agent", self.trace.read_text())
        self.assertNotIn("docker", self.trace.read_text())

    def test_owned_docker_only_no_name_substring_cleanup(self):
        self.info("docker")
        payloads = {}
        for name in ("zxy-panel-api", "zxy-panel-frontend"):
            payloads[name] = [{"Id": "owned-" + name, "Config": {"Labels": {
                "com.docker.compose.service": name,
                "com.docker.compose.project": "synthetic-project",
                "com.docker.compose.project.working_dir": str(self.app),
                "com.docker.compose.project.config_files": str(self.app / "docker-compose.yml")}},
                "Mounts": [{"Type": "bind", "Source": str(self.app / "data"), "Destination": "/app/data"}]}]
            (self.root / (name + ".json")).write_text(json.dumps(payloads[name]))
        setup = r'''
docker() {
  printf 'docker %s\n' "$*" >> "$TRACE"
  case "$1" in
    context) echo default ;;
    ps) printf 'zxy-panel-api\nzxy-panel-frontend\nforeign-zxy-panel-business\n' ;;
    inspect) python3 -c 'import pathlib,sys;print(pathlib.Path(sys.argv[1]).read_text())' "$LAB/$2.json" ;;
    stop) [[ "$2" == owned-zxy-panel-api && "$3" == owned-zxy-panel-frontend ]] ;;
    *) return 99 ;;
  esac
}
'''
        self.ok("install", "INSTALL_MODE=docker; AUTO_AGENT=false; inspect_owned_containers; cleanup_old_runtime", setup=setup)
        trace = self.trace.read_text()
        self.assertNotIn(" rm ", trace)
        self.assertNotIn("foreign-zxy-panel-business", trace)
        labels = payloads["zxy-panel-api"][0]["Config"]["Labels"]
        for key in list(labels):
            with self.subTest(missing_label=key):
                altered = json.loads(json.dumps(payloads["zxy-panel-api"]))
                del altered[0]["Config"]["Labels"][key]
                (self.root / "zxy-panel-api.json").write_text(json.dumps(altered))
                self.trace.write_text("")
                self.bad("install", "inspect_owned_containers", setup=setup)
                self.assertNotIn("docker stop", self.trace.read_text())

    def test_agent_preserves_identity_interval_apply_and_extension(self):
        self.agent_file()
        self.ok("agent", 'INSTALL_XRAY=false; SETUP_XRAY_SERVICE=false; resolve_agent_config; [[ "$SERVER_ID" == srv-synthetic && "$AGENT_TOKEN" == synthetic-agent-token && "$AGENT_INTERVAL" == 67 && "$APPLY_CONFIG" == false ]]; write_agent_env')
        content = (self.config / "agent.env").read_text()
        self.assertIn("EXTRA_AGENT=retained", content)
        self.assertIn("ZXY_AGENT_INTERVAL_SECONDS=67", content)
        self.assertEqual((self.config / "agent.env").stat().st_mode & 0o777, 0o600)

    def test_agent_explicit_interval_beats_valid_old_config(self):
        self.agent_file()
        self.ok("agent", 'resolve_agent_config; [[ "$AGENT_INTERVAL" == 81 && "$APPLY_CONFIG" == false ]]',
                updates={"ZXY_AGENT_INTERVAL_SECONDS": "81", "APPLY_CONFIG": "false"})

    def test_agent_invalid_interval_and_identity_change_rejected(self):
        self.agent_file()
        self.bad("agent", "resolve_agent_config", updates={"ZXY_AGENT_INTERVAL_SECONDS": "0"})
        self.bad("agent", "resolve_agent_config", updates={"SERVER_ID": "srv-other"})

    def test_agent_identity_consistency_uses_exact_id_and_token(self):
        self.agent_file()
        database = self.app / "data/zxy-panel.json"
        database.parent.mkdir(exist_ok=True)
        database.write_text(json.dumps({"servers": {"srv-synthetic": {"id": "srv-synthetic", "agent_token": "synthetic-agent-token"}}}))
        self.ok("agent", "resolve_agent_config")
        database.write_text(json.dumps({"servers": {"srv-synthetic": {"id": "srv-other", "agent_token": "synthetic-agent-token"}}}))
        self.bad("agent", "resolve_agent_config")

    def test_foreign_xray_refused_and_status_only_is_allowed(self):
        self.agent_file()
        xray = self.bin / "xray"
        xray.write_text("#!/usr/bin/env bash\nexit 99\n")
        xray.chmod(0o755)
        self.bad("agent", "preflight_agent")
        self.ok("agent", "preflight_agent", updates={"SETUP_XRAY_SERVICE": "false", "APPLY_CONFIG": "false"})
        self.bad("agent", "preflight_agent", updates={"SETUP_XRAY_SERVICE": "false", "APPLY_CONFIG": "false", "ZXY_FORCE_INSTALL_XRAY": "true"})

    def test_legacy_managed_xray_requires_agent_linked_configuration(self):
        self.agent_file()
        self.agent_unit()
        (self.root / "xray-base.service").write_text("[Service]\n")
        xray = self.bin / "xray"
        xray.write_text("#!/usr/bin/env bash\nexit 99\n")
        xray.chmod(0o755)
        dropin = self.root / "xray-dropin.conf"
        dropin.write_text("[Service]\nUser=root\nGroup=root\nExecStart=\nExecStart=" + str(xray) +
                         " run -config " + str(self.config / "xray/config.json") + "\n")
        self.ok("agent", "preflight_agent")
        dropin.write_text(dropin.read_text().replace("xray/config.json", "foreign/config.json"))
        self.bad("agent", "preflight_agent")

    def test_cli_declared_docker_is_not_overridden_by_stale_unit(self):
        self.info("docker")
        self.api_unit()
        self.assertEqual(self.ok("cli", "install_mode").stdout.strip(), "docker")
        self.assertEqual(self.trace.read_text(), "")

    def test_cli_unknown_mode_is_not_guessed(self):
        self.info("")
        self.bad("cli", "install_mode")

    def test_no_default_nginx_cleanup_remains(self):
        text = (REPO / "deploy/install.sh").read_text()
        self.assertNotIn("disable_default_nginx_sites", text)
        self.assertNotIn('name=zxy-panel', text)
        self.assertNotIn("docker rm -f", text)

    def test_empty_existing_values_are_not_missing_defaults(self):
        for key in ("API_PORT", "WEB_PORT", "ZXY_DB_PATH", "ZXY_JWT_SECRET",
                    "ZXY_ADMIN_PASSWORD"):
            with self.subTest(key=key):
                self.env_file()
                p = self.app / ".env"
                lines = [key + "=" if line.startswith(key + "=") else line
                         for line in p.read_text().splitlines()]
                p.write_text("\n".join(lines) + "\n")
                self.bad("install", "INSTALL_MODE=fast; resolve_existing_config")
        self.env_file()
        for key in ("ZXY_APPLY_CONFIG", "ZXY_AGENT_INTERVAL_SECONDS", "ZXY_SERVER_ID"):
            with self.subTest(key=key):
                self.agent_file()
                p = self.config / "agent.env"
                p.write_text("\n".join(key + "=" if line.startswith(key + "=") else line
                                       for line in p.read_text().splitlines()) + "\n")
                self.bad("agent", "resolve_agent_config")

    def test_systemd_unquoted_paths_decode_before_rewrite(self):
        path = str(self.root / "external panel/store.json")
        self.env_file()
        p = self.app / ".env"
        p.write_text(p.read_text().replace(str(self.app / "data/zxy-panel.json"),
                                          path.replace(" ", "\\ ")))
        self.ok("install", 'INSTALL_MODE=fast; resolve_existing_config; [[ "$DB_PATH" == "$EXPECTED" ]]; write_env; [[ "$(env_file_value ZXY_DB_PATH)" == "$EXPECTED" ]]',
                updates={"EXPECTED": path})
        self.agent_file()
        p = self.config / "agent.env"
        p.write_text(p.read_text().replace(str(self.config / "xray/config.json"),
                                          str(self.config / "xray folder/config.json").replace(" ", "\\ ")))
        self.ok("agent", 'resolve_agent_config; [[ "$XRAY_CONFIG" == "$CONFIG_DIR/xray folder/config.json" ]]; write_agent_env; [[ "$(agent_env_value ZXY_XRAY_CONFIG)" == "$XRAY_CONFIG" ]]',
                updates={"ZXY_DB_PATH": str(self.app / "data/zxy-panel.json")})

    def test_docker_unquoted_backslash_is_literal_not_systemd_escape(self):
        self.env_file("EXTENSION_PATH=/srv/panel\\ data/store.json\n")
        self.ok("install", 'INSTALL_MODE=docker; [[ "$(env_file_value EXTENSION_PATH)" == "$EXPECTED" ]]',
                updates={"EXPECTED": "/srv/panel\\ data/store.json"})
        self.env_file("EXTENSION_VALUE=${UNDEFINED_ENVIRONMENT}\n")
        self.ok("install", 'INSTALL_MODE=docker; [[ "$(env_file_value EXTENSION_VALUE)" == "" ]]')

    def test_docker_supported_interpolation_uses_prior_values_and_real_input(self):
        self.env_file("PANEL_HINT=19088\n")
        p = self.app / ".env"
        p.write_text(p.read_text().replace("API_PORT=9088", "API_PORT=${PORT_INPUT:-10088}"))
        self.ok("install", 'INSTALL_MODE=docker; resolve_existing_config; [[ "$API_PORT" == 10088 ]]')
        self.ok("install", 'INSTALL_MODE=docker; resolve_existing_config; [[ "$API_PORT" == 19088 ]]',
                updates={"PORT_INPUT": "19088"})

    def test_explicit_agent_identity_requires_both_fields_before_stop(self):
        for inputs in ({"ZXY_SERVER_ID": "srv-only"}, {"ZXY_AGENT_TOKEN": "synthetic-only"},
                       {"SERVER_ID": "", "AGENT_TOKEN": "synthetic-token"}):
            with self.subTest(inputs=list(inputs)):
                self.bad("install", "AUTO_AGENT=true; INSTALL_MODE=fast; preflight_runtime_ownership", updates=inputs)
                self.assertEqual(self.trace.read_text(), "")

    def test_environment_symlink_rejected_before_any_stop(self):
        real = self.root / "actual.env"
        p = self.app / ".env"
        p.rename(real)
        p.symlink_to(real)
        before = (real.read_bytes(), real.stat().st_mtime_ns)
        self.bad("install", "INSTALL_MODE=fast; resolve_existing_config; cleanup_old_runtime")
        self.assertEqual((real.read_bytes(), real.stat().st_mtime_ns), before)
        self.assertNotIn("systemctl", self.trace.read_text())

    def test_old_agent_missing_database_is_not_silently_accepted(self):
        self.agent_file()
        (self.app / "data/zxy-panel.json").unlink()
        self.bad("agent", "resolve_agent_config")

    def test_parent_forwards_resolved_database_to_preflight(self):
        script = self.source / "deploy/agent-install.sh"
        script.parent.mkdir()
        script.write_text('#!/usr/bin/env bash\n[[ "$1" == --preflight && "$ZXY_DB_PATH" == "$LAB/external/panel.json" ]]\n')
        self.ok("install", 'INSTALL_MODE=fast; AUTO_AGENT=true; DB_PATH="$LAB/external/panel.json"; preflight_runtime_ownership')
        self.assertNotIn(" stop ", self.trace.read_text())

    def test_first_agent_aliases_and_generic_priority(self):
        script = self.app / "deploy/agent-install.sh"
        script.parent.mkdir()
        script.write_text('#!/usr/bin/env bash\n[[ "$SERVER_ID" == "$EXPECT_ID" && "$AGENT_TOKEN" == "$EXPECT_TOKEN" && "${PANEL_BASE-${ZXY_PANEL_BASE-}}" == "$EXPECT_BASE" ]]\n')
        aliases = {"ZXY_SERVER_ID": "srv-alias", "ZXY_AGENT_TOKEN": "synthetic-alias",
                   "ZXY_PANEL_BASE": "http://127.0.0.1:19388",
                   "EXPECT_ID": "srv-alias", "EXPECT_TOKEN": "synthetic-alias",
                   "EXPECT_BASE": "http://127.0.0.1:19388"}
        self.ok("install", 'AUTO_AGENT=true; API_PORT=9088; DB_PATH="$APP_DIR/data/zxy-panel.json"; install_local_agent',
                updates=aliases)
        self.ok("install", 'AUTO_AGENT=true; API_PORT=9088; DB_PATH="$APP_DIR/data/zxy-panel.json"; install_local_agent',
                updates=dict(aliases, SERVER_ID="srv-generic", AGENT_TOKEN="synthetic-generic",
                             PANEL_BASE="http://127.0.0.1:19488", EXPECT_ID="srv-generic",
                             EXPECT_TOKEN="synthetic-generic", EXPECT_BASE="http://127.0.0.1:19488"))

    def test_bundled_xray_install_failure_is_not_success_or_download(self):
        path = self.source / "bin/xray-linux-amd64"
        path.parent.mkdir()
        path.write_bytes(b"synthetic-binary")
        before = self.snapshot()
        result = self.shell("agent", "if install_local_xray_if_available; then exit 0; else exit $?; fi",
                            setup='install() { printf "install-failed\\n" >> "$TRACE"; return 73; }')
        self.assertEqual(result.returncode, 2)
        self.assertEqual(self.trace.read_text(), "install-failed\n")
        self.assertEqual(self.snapshot(), before)

    def test_vendor_loaded_unit_without_our_file_is_refused(self):
        (self.root / "unit-overrides.json").write_text(json.dumps({
            "zxy-panel-api": {"LoadState": "loaded",
                              "FragmentPath": "/usr/lib/systemd/system/zxy-panel-api.service"}}))
        self.bad("install", "INSTALL_MODE=fast; AUTO_AGENT=false; preflight_runtime_ownership")
        self.assertEqual(self.trace.read_text(), "")

    def test_effective_unit_overrides_are_not_owned(self):
        self.api_unit()
        for field, value in (("FragmentPath", "/run/systemd/system/zxy-panel-api.service"),
                             ("DropInPaths", "/etc/systemd/system/zxy-panel-api.service.d/foreign.conf"),
                             ("EnvironmentFiles", "/foreign.env (ignore_errors=no)"),
                             ("WorkingDirectory", "/foreign-app"),
                             ("ExecStart", "{ path=/foreign-api ; argv[]=/foreign-api ; }")):
            with self.subTest(field=field):
                (self.root / "unit-overrides.json").write_text(json.dumps({"zxy-panel-api": {field: value}}))
                self.bad("install", "INSTALL_MODE=fast; AUTO_AGENT=false; preflight_runtime_ownership")
                self.assertEqual(self.trace.read_text(), "")

    def test_matching_unit_with_environment_override_is_refused(self):
        self.api_unit()
        p = self.root / "api.service"
        p.write_text(p.read_text() + "Environment=ZXY_DB_PATH=/foreign-data.json\n")
        self.bad("install", "INSTALL_MODE=fast; AUTO_AGENT=false; preflight_runtime_ownership")
        self.assertEqual(self.trace.read_text(), "")

    def test_systemd_inspection_failure_is_not_absence(self):
        self.bad("install", "INSTALL_MODE=fast; AUTO_AGENT=false; preflight_runtime_ownership",
                 setup="systemctl() { return 88; }")
        self.agent_file()
        self.bad("agent", "preflight_agent", setup="systemctl() { return 88; }")

    def test_agent_vendor_unit_is_not_overwritten(self):
        self.agent_file()
        (self.root / "unit-overrides.json").write_text(json.dumps({
            "zxy-agent": {"LoadState": "loaded", "FragmentPath": "/usr/lib/systemd/system/zxy-agent.service"}}))
        self.bad("agent", "preflight_agent")
        self.assertEqual(self.trace.read_text(), "")

    def test_agent_unit_additional_dropin_is_refused(self):
        self.agent_file()
        self.agent_unit()
        (self.root / "unit-overrides.json").write_text(json.dumps({
            "zxy-agent": {"DropInPaths": "/run/systemd/system/zxy-agent.service.d/override.conf"}}))
        self.bad("agent", "preflight_agent")
        self.assertEqual(self.trace.read_text(), "")

    def test_compose_execution_is_bound_to_verified_project_file_and_environment(self):
        self.info("docker")
        _, setup = self.docker_fixture()
        self.ok("install", "INSTALL_MODE=docker; inspect_owned_containers; COMPOSE='docker compose'; run_panel_compose up -d",
                setup=setup, updates={"COMPOSE_REMOVE_ORPHANS": "true", "COMPOSE_PROFILES": "foreign-profile"})
        args = json.loads((self.root / "compose-args.json").read_text())
        self.assertEqual(args["argv"], ["compose", "-p", "synthetic-project", "-f",
                         str(self.app / "docker-compose.yml"), "--project-directory", str(self.app),
                         "--env-file", str(self.app / ".env"), "up", "-d"])
        self.assertEqual(args["remove_orphans"], "false")
        self.assertEqual(args["profiles"], "")
        self.ok("cli", "run_panel_compose restart zxy-panel-api", setup=setup)
        args = json.loads((self.root / "compose-args.json").read_text())
        self.assertEqual(args["argv"][-2:], ["restart", "zxy-panel-api"])
        self.assertEqual(args["argv"][2:4], ["synthetic-project", "-f"])

    def test_conflicting_compose_file_or_project_never_executes_action(self):
        self.info("docker")
        _, setup = self.docker_fixture()
        for key, value in (("COMPOSE_FILE", "/foreign/docker-compose.yml"),
                           ("COMPOSE_PROJECT_NAME", "foreign-project"),
                           ("DOCKER_HOST", "tcp://127.0.0.1:12345"),
                           ("DOCKER_CONTEXT", "foreign-context")):
            with self.subTest(key=key, source="process"):
                for library, body in (("install", "INSTALL_MODE=docker; inspect_owned_containers"),
                                      ("cli", "run_panel_compose restart")):
                    self.trace.write_text("")
                    self.bad(library, body, setup=setup, updates={key: value})
                    self.assertNotIn("compose -p", self.trace.read_text())
                    self.assertNotIn("docker stop", self.trace.read_text())
            with self.subTest(key=key, source="old-env"):
                self.env_file(key + "=" + value + "\n")
                for library, body in (("install", "INSTALL_MODE=docker; inspect_owned_containers"),
                                      ("cli", "run_panel_compose restart")):
                    self.trace.write_text("")
                    self.bad(library, body, setup=setup)
                    self.assertNotIn("compose -p", self.trace.read_text())
                    self.assertNotIn("docker stop", self.trace.read_text())
                self.env_file()

    def test_two_container_projects_must_agree(self):
        self.info("docker")
        payloads, setup = self.docker_fixture()
        payloads["zxy-panel-frontend"][0]["Config"]["Labels"]["com.docker.compose.project"] = "foreign-project"
        (self.root / "zxy-panel-frontend.json").write_text(json.dumps(payloads["zxy-panel-frontend"]))
        self.bad("install", "INSTALL_MODE=docker; inspect_owned_containers", setup=setup)
        self.bad("cli", "run_panel_compose restart", setup=setup)
        self.assertNotIn("compose -p", self.trace.read_text())

    def test_remote_current_context_is_refused(self):
        self.info("docker")
        _, setup = self.docker_fixture()
        setup = 'docker() { if [[ "$1" == context ]]; then echo foreign-context; elif [[ "$1 $2" == "compose version" ]]; then printf "docker compose version\\n" >> "$TRACE"; echo synthetic-compose; else return 99; fi; }'
        self.bad("install", "INSTALL_MODE=docker; inspect_owned_containers", setup=setup)
        self.bad("cli", "run_panel_compose restart", setup=setup)
        self.assertEqual(self.trace.read_text(), "docker compose version\n")


    def test_xray_nonloaded_artifacts_are_preflighted(self):
        for state, artifact in (("masked", False), ("error", False), ("not-found", True)):
            with self.subTest(state=state):
                (self.root / "unit-overrides.json").write_text(json.dumps({"xray": {"LoadState": state}}))
                dropin = self.root / "xray-dropin.conf"
                if artifact:
                    dropin.write_text("[Service]\nExecStart=/foreign/xray\n")
                self.bad("agent", "preflight_agent")
                self.assertEqual(self.trace.read_text(), "")
                if artifact:
                    self.assertEqual(dropin.read_text(), "[Service]\nExecStart=/foreign/xray\n")
                    dropin.unlink()

    def test_agent_port_change_requires_matching_explicit_endpoint(self):
        self.agent_file()
        self.api_unit()
        self.agent_unit()
        script = self.source / "deploy/agent-install.sh"
        script.parent.mkdir()
        script.write_text('[[ "$1" == --preflight ]] || exit 77\nprintf "child-preflight\\n" >> "$TRACE"\n')
        for auto in ("true", "false"):
            for endpoint in (None, "http://127.0.0.1:9088", "http://foreign.invalid:10088"):
                with self.subTest(auto=auto, endpoint=endpoint):
                    inputs = {"API_PORT": "10088", "AUTO_AGENT": auto}
                    if endpoint is not None:
                        inputs["PANEL_BASE"] = endpoint
                    result = self.bad("install", "INSTALL_MODE=fast; resolve_existing_config; preflight_runtime_ownership", updates=inputs)
                    self.assertIn("API port change conflicts with the retained local Agent endpoint", result.stderr)
                    self.assertEqual(self.trace.read_text(), "")

    def test_matching_explicit_endpoint_preserves_identity(self):
        self.agent_file()
        self.api_unit()
        self.agent_unit()
        stub = self.source / "deploy/agent-install.sh"
        stub.parent.mkdir()
        stub.write_text('[[ "$1" == --preflight && "$PANEL_BASE" == http://127.0.0.1:10088 ]]\n')
        result = self.ok("install", "INSTALL_MODE=fast; resolve_existing_config; preflight_runtime_ownership",
                         updates={"API_PORT": "10088", "PANEL_BASE": "http://127.0.0.1:10088"})
        self.assertEqual(result.stderr, "")
        self.ok("agent", 'resolve_agent_config; [[ "$SERVER_ID" == srv-synthetic && "$AGENT_TOKEN" == synthetic-agent-token && "$PANEL_BASE" == http://127.0.0.1:10088 ]]',
                updates={"PANEL_BASE": "http://127.0.0.1:10088"})
        self.assertEqual(self.trace.read_text(), "")

    def test_fresh_retained_agent_identity_is_refused_before_cleanup(self):
        self.agent_file()
        self.api_unit()
        self.agent_unit()
        script = self.source / "deploy/agent-install.sh"
        script.parent.mkdir()
        script.write_text('[[ "$1" == --preflight ]] || exit 77\nprintf "child-preflight\\n" >> "$TRACE"\n')
        for auto in ("true", "false"):
            with self.subTest(auto=auto):
                result = self.bad("install", "INSTALL_MODE=fast; resolve_existing_config; preflight_runtime_ownership",
                                  updates={"FRESH_INSTALL": "true", "AUTO_AGENT": auto})
                self.assertIn("Fresh install cannot retain an existing Agent identity", result.stderr)
                self.assertEqual(self.trace.read_text(), "")

    def test_first_explicit_identity_checks_existing_database(self):
        data = self.app / "data/zxy-panel.json"
        data.parent.mkdir()
        data.write_text(json.dumps({"servers": {"srv-synthetic": {
            "id": "srv-synthetic", "agent_token": "synthetic-agent-token"}}}))
        for identity, token in (("wrong-id", "synthetic-agent-token"),
                                ("srv-synthetic", "wrong-token")):
            with self.subTest(identity=identity):
                self.bad("agent", "resolve_agent_config", updates={"SERVER_ID": identity, "AGENT_TOKEN": token})
                self.assertEqual(self.trace.read_text(), "")
        self.ok("agent", 'resolve_agent_config; [[ "$SERVER_ID" == srv-synthetic ]]',
                updates={"SERVER_ID": "srv-synthetic", "AGENT_TOKEN": "synthetic-agent-token"})
        self.bad("install", "INSTALL_MODE=fast; resolve_existing_config; preflight_runtime_ownership",
                 updates={"FRESH_INSTALL": "true", "SERVER_ID": "srv-synthetic", "AGENT_TOKEN": "synthetic-agent-token"})
        self.assertEqual(self.trace.read_text(), "")

    def test_explicit_empty_flags_and_local_values_are_not_defaulted(self):
        for key in ("INSTALL_XRAY", "SETUP_XRAY_SERVICE", "ZXY_FORCE_INSTALL_XRAY", "ZXY_SKIP_XRAY_INSTALL"):
            with self.subTest(key=key):
                self.bad("agent", "preflight_agent", updates={key: ""})
                self.assertEqual(self.trace.read_text(), "")
        for key in ("ZXY_LOCAL_SERVER_IP", "ZXY_LOCAL_SERVER_HOST", "ZXY_LOCAL_SERVER_NAME",
                    "ZXY_LOCAL_SERVER_REGION", "ZXY_LOCAL_SERVER_PROVIDER"):
            with self.subTest(key=key):
                self.bad("install", "preflight_inputs", updates={key: ""})
                self.assertEqual(self.trace.read_text(), "")

    def uninstall_body(self):
        text = (REPO / "scripts/zxy-panel").read_text()
        branch = text.split("\n  uninstall)\n", 1)[1].split("\n    ;;", 1)[0]
        return "uninstall_fixture() {\n" + branch + "\n}\nrm() { printf 'remove %s\\n' \"$*\" >> \"$TRACE\"; }\nuninstall_fixture"

    def test_fast_uninstall_never_selects_installed_compose(self):
        before = self.snapshot()
        self.ok("cli", self.uninstall_body(), setup='compose_cmd() { printf "unexpected-compose\\n" >> "$TRACE"; echo "docker compose"; }')
        self.assertNotIn("unexpected-compose", self.trace.read_text())
        self.assertNotIn("docker ", self.trace.read_text())
        self.assertIn("systemctl stop zxy-panel-api", self.trace.read_text())
        self.assertEqual(self.snapshot(), before)

    def test_docker_uninstall_checks_ownership_before_cleanup(self):
        self.info("docker")
        _, setup = self.docker_fixture()
        self.bad("cli", self.uninstall_body(), setup=setup, updates={"COMPOSE_FILE": "/foreign/compose.yml"})
        self.assertNotIn("systemctl stop", self.trace.read_text())
        self.assertNotIn("remove ", self.trace.read_text())
        self.trace.write_text("")
        self.ok("cli", self.uninstall_body(), setup=setup)
        trace = self.trace.read_text()
        self.assertNotIn("systemctl stop zxy-panel-api", trace)
        self.assertLess(trace.index("config --quiet"), trace.index(" down"))
        self.assertLess(trace.index(" down"), trace.index("systemctl stop zxy-agent"))

    def test_unsupported_systemd_paths_reject_before_writes(self):
        for key in ("APP_DIR", "CONFIG_DIR"):
            for value in (str(self.root / "panel-%n"), str(self.root / r"panel\x2ddata"), ""):
                with self.subTest(key=key, value=value):
                    main = self.bad("install", "preflight_inputs", updates={key: value})
                    self.assertIn("Invalid explicit " + key, main.stderr)
                    agent = self.bad("agent", "preflight_agent", updates={key: value})
                    self.assertIn("unsupported " + key + " path", agent.stderr)
                    self.assertEqual(self.trace.read_text(), "")

    def test_status_only_agent_allows_unmanaged_masked_xray(self):
        self.agent_file()
        self.agent_unit()
        artifact = self.root / "xray-dropin.conf"
        artifact.write_text("[Service]\nExecStart=/foreign/xray\n")
        before = self.snapshot()
        original = (artifact.read_bytes(), artifact.stat().st_mtime_ns)
        for state in ("masked", "error", "not-found"):
            with self.subTest(state=state):
                (self.root / "unit-overrides.json").write_text(json.dumps({"xray": {
                    "LoadState": state, "FragmentPath": "/foreign/xray.service",
                    "DropInPaths": str(artifact)}}))
                self.ok("agent", "preflight_agent", updates={
                    "INSTALL_XRAY": "false", "SETUP_XRAY_SERVICE": "false",
                    "APPLY_CONFIG": "false", "ZXY_FORCE_INSTALL_XRAY": "0"})
                self.assertEqual(self.snapshot(), before)
                self.assertEqual((artifact.read_bytes(), artifact.stat().st_mtime_ns), original)
                self.assertEqual(self.trace.read_text(), "")

    def test_parent_empty_force_skip_is_not_transmitted_as_default(self):
        for key in ("ZXY_FORCE_INSTALL_XRAY", "ZXY_SKIP_XRAY_INSTALL"):
            for value in ("", "invalid"):
                with self.subTest(key=key, value=value):
                    result = self.bad("install", "preflight_inputs", updates={key: value})
                    self.assertIn("Invalid explicit " + key, result.stderr)
                    self.assertEqual(self.trace.read_text(), "")

if __name__ == "__main__":
    unittest.main(verbosity=2)
