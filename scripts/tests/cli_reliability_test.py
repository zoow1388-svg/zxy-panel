"""CLI/config/data reliability fixtures. Never invokes the real installer/services."""
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import subprocess
import tarfile
import tempfile
import unittest
import zipfile

REPO = Path(__file__).resolve().parents[2]


class CliReliability(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="zxy-ops-cli-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app, self.config, self.bin = (self.root / p for p in ("app", "config", "bin"))
        self.system = self.root / "system"
        for p in (self.app, self.config, self.bin, self.system):
            p.mkdir()
        for name in ("bash", "python3", "date", "dirname", "grep", "head", "cut", "mkdir",
                     "rm", "cp", "mv", "chmod", "mktemp", "cat", "tr", "sha256sum", "tee", "wc", "sleep", "seq"):
            actual = shutil.which(name)
            self.assertIsNotNone(actual, "Existing tool required: " + name)
            (self.bin / name).symlink_to(actual)
        source = (REPO / "scripts/zxy-panel").read_text(encoding="utf-8")
        self.assertEqual(source.count('\ncase "${1:-}" in'), 1)
        # Only the existing CLI's installed configuration binding is changed.
        source = source.replace('CONFIG_DIR="/etc/zxy-panel"',
                                "CONFIG_DIR=" + shlex.quote(str(self.config)), 1)
        system_paths = dict(HOST_NGINX_FILE="/etc/nginx/conf.d/zxy-panel.conf",
            API_UNIT_FILE="/etc/systemd/system/zxy-panel-api.service",
            AGENT_UNIT_FILE="/etc/systemd/system/zxy-agent.service",
            XRAY_DROPIN_FILE="/etc/systemd/system/xray.service.d/99-zxy-panel.conf",
            BBR_CONF_FILE="/etc/sysctl.d/99-zxy-bbr.conf",
            BBR_DISABLED_FILE="/etc/zxy-panel/bbr.disabled", PROC_DIRECTORY="/proc")
        # Rebind only declared managed system paths to synthetic input locations.
        # No production function body, decision or assertion is substituted.
        for key, value in system_paths.items():
            assignment = key + "=" + value
            self.assertEqual(source.count(assignment + "\n"), 1)
            source = source.replace(assignment + "\n", key + "=" +
                                    shlex.quote(str(self.system / key)) + "\n", 1)
        self.cli = self.root / "cli.sh"
        self.cli.write_bytes(source.encode())
        (self.config / "panel.info").write_text("INSTALL_DIR=" + str(self.app) +
            "\nINSTALL_MODE=fast\nVERSION=0.7.8-stable-engineering\nPORT=49321\nWEB_BASE_PATH=fixturebase\n")
        (self.app / ".env").write_text("API_PORT=18088\nWEB_PORT=15173\nZXY_DB_PATH=" +
            str(self.app / "data/zxy-panel.json") + "\nZXY_AUTO_AGENT=false\n")
        (self.app / "VERSION").write_text("0.7.8-stable-engineering\n")
        self.trace = self.root / "trace"
        self.trace.write_text("")
        self.data = self.app / "data/zxy-panel.json"
        self.data.parent.mkdir()
        self.data.write_text(json.dumps(dict(servers={}, clients={}, logs=[])))
        self.env = dict(PATH=str(self.bin), HOME=str(self.root), TMPDIR=str(self.root),
                        LAB=str(self.root), TRACE=str(self.trace), CONFIG_DIR=str(self.config),
                        APP_DIR=str(self.app), PYTHONDONTWRITEBYTECODE="1")
        self.source = self.root / "source"
        (self.source / "scripts").mkdir(parents=True)
        (self.source / "scripts/zxy-panel").write_bytes((REPO / "scripts/zxy-panel").read_bytes())
        install = (REPO / "deploy/install.sh").read_text(encoding="utf-8")
        self.assertEqual(install.count('\nmain "$@"'), 1)
        self.install_lib = self.root / "install-library.sh"
        library = install.rsplit('\nmain "$@"', 1)[0]
        destination = "/usr/local/bin/zxy-panel <<'PY_INSTALL_CLI'"
        self.assertEqual(library.count(destination), 1)
        library = library.replace(destination, shlex.quote(str(self.bin/"published-cli"))+
                                  " <<'PY_INSTALL_CLI'", 1)
        self.install_lib.write_bytes(library.encode())

    def shell(self, body, setup="", updates=None):
        env = dict(self.env)
        env.update(updates or {})
        result = subprocess.run(["bash", "-euc", 'source "' + str(self.cli) + '"\n' +
                                 setup + "\n" + body], env=env, capture_output=True, text=True)
        self.assertNotIn("command not found", result.stderr)
        return result

    def snapshot(self, backups=False):
        return {str(p.relative_to(self.root)): (hashlib.sha256(p.read_bytes()).hexdigest(),
                    p.stat().st_mtime_ns, p.stat().st_mode)
                for folder in (self.app, self.config, self.system) for p in folder.rglob("*")
                if p.is_file() and (backups or "backups" not in p.relative_to(self.root).parts)}

    def installer_shell(self, body, extra=""):
        setup = r'''
systemctl() { printf 'systemctl %s\n' "$*" >> "$TRACE"; if [[ "$1" == is-active ]]; then echo active; fi; }
SRC_DIR="$LAB/source"
INSTALL_MODE=fast
DB_PATH="$APP_DIR/data/zxy-panel.json"
HOST_NGINX_FILE="$LAB/system/HOST_NGINX_FILE"
API_UNIT_FILE="$LAB/system/API_UNIT_FILE"
AGENT_UNIT_FILE="$LAB/system/AGENT_UNIT_FILE"
XRAY_DROPIN_FILE="$LAB/system/XRAY_DROPIN_FILE"
BBR_CONF_FILE="$LAB/system/BBR_CONF_FILE"
BBR_DISABLED_FILE="$LAB/system/BBR_DISABLED_FILE"
'''
        script = 'source ' + shlex.quote(str(self.install_lib)) + "\n" + setup + "\n" + extra + "\n" + body
        result = subprocess.run(["bash", "-euc", script], env=self.env, capture_output=True, text=True)
        self.assertNotIn("command not found", result.stderr)
        return result

    def python_fault(self, injection):
        return '''python3() {
  if [[ "$1" != - ]]; then command python3 "$@"; return; fi
  local script
  script=$(cat)
  if [[ "$script" == *"meta = dict(format='zxy-panel-config-data-v1'"* ]]; then
    script=''' + shlex.quote(injection) + '''$'\\n'"$script"
  fi
  command python3 "$@" <<< "$script"
}'''

    def invoke(self, action, fault="", updates=None):
        # All service, container, HTTP and journal observations are substitutes.
        setup = r'''
systemctl() {
  printf 'systemctl %s\n' "$*" >> "$TRACE"
  if [[ "$1" == show ]]; then
    if [[ "$2" == zxy-agent || "$2" == xray ]]; then
      if [[ "$OPTIONAL" == absent ]]; then echo not-found; else echo loaded; fi
    else echo loaded
    fi
    return
  fi
  if [[ "$1" == is-active ]]; then echo active; return; fi
  if [[ "$1" == restart && "$2" == zxy-panel-api && "$FAULT" == restart ]]; then return 17; fi
  if [[ "$1" == restart ]]; then return; fi
  return 90
}
nginx() { printf 'nginx %s\n' "$*" >> "$TRACE"; [[ "$FAULT" != nginx ]]; }
curl() {
  printf 'curl %s\n' "${@: -1}" >> "$TRACE"
  if [[ "$FAULT" == health ]]; then printf '<html>SPA</html>'; return; fi
  printf '{"status":"ok","service":"zxy-panel-api","version":"0.7.8-stable-engineering"}'
}
journalctl() {
  printf 'journalctl\n' >> "$TRACE"
  [[ "$FAULT" != journal ]]
}
sleep() { printf 'poll interval\n' >> "$TRACE"; }
docker() {
  printf 'docker %s\n' "$*" >> "$TRACE"
  case "$1" in
    context) printf 'default\n';;
    compose) [[ "$FAULT" != compose ]];;
    inspect)
      if [[ "$2" == --format ]]; then
        if [[ "$FAULT" == container ]]; then printf 'false\n'; else printf 'true\n'; fi
      else
        python3 - "$LAB/app" "$2" <<'PY_CONTAINER'
import json,sys
app, service=sys.argv[1:]
print(json.dumps([dict(Config=dict(Labels={
    'com.docker.compose.project':'synthetic-project',
    'com.docker.compose.service':service,
    'com.docker.compose.project.working_dir':app,
    'com.docker.compose.project.config_files':app+'/docker-compose.yml'}),
    Mounts=[dict(Type='bind',Source=app+'/data',Destination='/app/data')])]))
PY_CONTAINER
      fi;;
    *) return 91;;
  esac
}
export -f systemctl nginx curl journalctl sleep docker
'''
        env = dict(self.env, FAULT=fault, OPTIONAL="absent")
        env.update(updates or {})
        result = subprocess.run(["bash", "-euc", setup + "\nexec bash " + shlex.quote(str(self.cli)) +
                                 " " + shlex.quote(action)], env=env, capture_output=True, text=True)
        self.assertNotIn("command not found", result.stderr)
        return result

    def manifest(self, **overrides):
        result = dict(version="0.7.9", latest="0.7.9-next-agent-xray", codename="next",
                      package="zxy-panel-v0.7.9-next.zip",
                      download_url="https://releases.invalid/zxy-panel-v0.7.9-next.zip?download=1&value=a%26b%3Dc",
                      sha256="a" * 64)
        result.update(overrides)
        path = self.root / "manifest.json"
        path.write_text(json.dumps(result))
        return path

    def command(self, path):
        result = self.shell('generate_upgrade_command "$LAB/manifest.json" 0.7.8-stable-engineering')
        self.assertEqual(result.returncode, 0, result.stderr)
        printed = result.stdout[result.stdout.index("bash -c "):].strip()
        argv = shlex.split(printed)
        self.assertEqual(argv[:2], ["bash", "-c"])
        self.assertEqual(len(argv), 3)
        return argv[2]

    def synthetic_package(self, member=None, status=0):
        package = self.root / "download.zip"
        with zipfile.ZipFile(package, "w") as z:
            z.writestr("zxy-panel-v0.7.9-next/deploy/install.sh",
                       '#!/bin/bash\nprintf "synthetic installer\\n" >> "$TRACE"\nexit ' + str(status) + "\n")
            if member:
                z.writestr(member, "synthetic")
        return package, hashlib.sha256(package.read_bytes()).hexdigest()

    def run_generated(self, script):
        work = self.root / "upgrade"
        work.mkdir()
        setup = r'''
mkdir() {
  [[ "$*" == '-p /root/zxy-panel-upgrades' ]] || return 91
}
mktemp() {
  [[ "$*" == '-d /root/zxy-panel-upgrades/job-XXXXXX' ]] || return 92
  printf '%s\n' "$LAB/upgrade"
}
curl() {
  printf 'curl\n' >> "$TRACE"
  [[ "$1" == --fail && "$2" == --location && "$3" == --silent && "$4" == --show-error && "$5" == -o ]] || return 93
  command cp "$LAB/download.zip" "$6"
  printf '%s' "$7" > "$LAB/download-url"
}
unzip() {
  printf 'unzip\n' >> "$TRACE"
  [[ "$PWD" == "$LAB/upgrade" ]] || return 94
  [[ "$1" == -q && "$2" == zxy-panel-v0.7.9-next.zip ]] || return 96
  python3 - "$2" <<'PY_EXTRACT'
import os, sys, zipfile
from pathlib import Path
assert Path.cwd() == Path(os.environ['LAB']) / 'upgrade'
with zipfile.ZipFile(sys.argv[1]) as z:
    z.extractall()
PY_EXTRACT
}
tee() {
  [[ "$1" == /root/zxy-panel-upgrade.log ]] || return 95
  command tee "$LAB/upgrade.log"
}
'''
        return self.shell(script, setup=setup)

    def test_upgrade_command_quotes_url_and_checks_sha_before_extract(self):
        _, sha = self.synthetic_package()
        manifest = self.manifest(sha256=sha)
        script = self.command(manifest)
        self.assertLess(script.index("sha256sum -c -"), script.index('unzip -q'))
        self.assertIn("set -euo pipefail", script)
        result = self.run_generated(script)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.trace.read_text().splitlines(), ["curl", "unzip", "synthetic installer"])
        self.assertEqual((self.root / "download-url").read_text(), json.loads(manifest.read_text())["download_url"])

    def test_hash_mismatch_never_extracts_or_installs(self):
        self.synthetic_package()
        script = self.command(self.manifest(sha256="b" * 64))
        result = self.run_generated(script)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.trace.read_text().splitlines(), ["curl"])
        self.assertFalse((self.root / "upgrade/zxy-panel-v0.7.9-next").exists())

    def test_unsafe_zip_is_rejected_after_hash_before_extract(self):
        _, sha = self.synthetic_package("../outside")
        result = self.run_generated(self.command(self.manifest(sha256=sha)))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsafe ZIP path", result.stderr)
        self.assertEqual(self.trace.read_text().splitlines(), ["curl"])
        self.assertFalse((self.root / "outside").exists())

    def test_installer_failure_is_not_hidden_by_tee(self):
        _, sha = self.synthetic_package(status=19)
        result = self.run_generated(self.command(self.manifest(sha256=sha)))
        self.assertEqual(result.returncode, 19)
        self.assertEqual(self.trace.read_text().splitlines(), ["curl", "unzip", "synthetic installer"])

    def test_manifest_missing_or_invalid_security_fields_fails(self):
        for overrides in ({"sha256": ""}, {"sha256": "x" * 64}, {"package": "../unsafe.zip"},
                          {"download_url": "http://releases.invalid/zxy-panel-v0.7.9-next.zip"},
                          {"download_url": "https://user:password@releases.invalid/zxy-panel-v0.7.9-next.zip"},
                          {"download_url": "https://releases.invalid/other.zip"},
                          {"latest": "0.7.9-next;unsafe"}, {"version": None}):
            with self.subTest(fields=tuple(overrides)):
                self.manifest(**overrides)
                result = self.shell('generate_upgrade_command "$LAB/manifest.json" 0.7.8-stable-engineering')
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("invalid update manifest", result.stderr)
                self.assertNotIn("bash -c", result.stdout)
                self.assertEqual(self.trace.read_bytes(), b"")

    def test_current_and_older_versions_never_generate_downgrade(self):
        for version in ("0.7.8", "0.7.7.5", "0.7.8.0"):
            with self.subTest(version=version):
                self.manifest(version=version, latest=version + "-next-agent-xray",
                    package="zxy-panel-v" + version + "-next.zip",
                    download_url="https://releases.invalid/zxy-panel-v" + version + "-next.zip")
                result = self.shell('generate_upgrade_command "$LAB/manifest.json" 0.7.8-stable-engineering')
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("no downgrade command", result.stdout)
                self.assertNotIn("bash -c", result.stdout)

    def test_health_requires_exact_service_version_and_json(self):
        setup = '''curl() { printf '%s' "$HEALTH"; }'''
        for payload, valid in ((json.dumps(dict(status="ok", service="zxy-panel-api", version="0.7.8-stable-engineering")), True),
                               ("<html>SPA</html>", False), ("{}", False),
                               (json.dumps(dict(status="ok", service="zxy-panel-api", version="0.7.7.5")), False),
                               (json.dumps(dict(status="ok", service="foreign", version="0.7.8-stable-engineering")), False)):
            with self.subTest(valid=valid, payload_type=payload[:1]):
                result = self.shell('health_check http://127.0.0.1:18088/api/health 0.7.8-stable-engineering',
                                    setup=setup, updates={"HEALTH": payload})
                self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_configuration_reader_respects_consumer_and_missing_vs_empty(self):
        (self.app / ".env").write_text("API_PORT='18088'\nSPECIAL='literal $value'\nEMPTY=\n")
        result = self.shell('env_file_value SPECIAL; printf "\\\\n"; env_file_value EMPTY "$APP_DIR/.env" present')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "literal $value\npresent")
        self.assertEqual(self.shell('env_file_value ABSENT "$APP_DIR/.env" present').stdout, "")

    def test_doctor_fast_custom_port_and_optional_absence(self):
        result = self.invoke("doctor")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Doctor result: PASS", result.stdout)
        self.assertIn("[INFO] zxy-agent", result.stdout)
        self.assertIn("[INFO] xray", result.stdout)
        self.assertIn("[INFO] Backup status", result.stdout)
        trace = self.trace.read_text()
        self.assertIn("curl http://127.0.0.1:18088/api/health", trace)
        self.assertIn("curl http://127.0.0.1:49321/fixturebase/api/health", trace)
        self.assertNotIn(":8088/", trace)
        self.assertNotIn("docker ", trace)

    def test_doctor_docker_uses_containers_not_api_systemd(self):
        info = self.config / "panel.info"
        info.write_text(info.read_text().replace("INSTALL_MODE=fast", "INSTALL_MODE=docker"))
        result = self.invoke("doctor")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("running container", result.stdout)
        self.assertNotIn("systemctl is-active --quiet zxy-panel-api", self.trace.read_text())
        self.assertIn("docker inspect --format {{.State.Running}} zxy-panel-api", self.trace.read_text())
        failed = self.invoke("doctor", fault="container")
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("Doctor result: FAIL", failed.stdout)

    def test_doctor_rejects_html_health_and_version_disagreement(self):
        result = self.invoke("doctor", fault="health")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Doctor result: FAIL", result.stdout)
        (self.app / "VERSION").write_text("0.7.7.5-old\n")
        self.trace.write_text("")
        result = self.invoke("doctor")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("versions disagree", result.stderr)
        self.assertEqual(self.trace.read_bytes(), b"")

    def test_doctor_requested_missing_agent_fails(self):
        env = self.app / ".env"
        env.write_text(env.read_text().replace("ZXY_AUTO_AGENT=false", "ZXY_AUTO_AGENT=true"))
        result = self.invoke("doctor")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requested but missing", result.stdout)
        self.assertNotIn("Doctor result: PASS", result.stdout)

    def test_doctor_external_database_and_unknown_journal_are_not_defaulted(self):
        external = self.root / "external-db.json"
        self.data.rename(external)
        env = self.app / ".env"
        env.write_text(env.read_text().replace(str(self.data), str(external)))
        result = self.invoke("doctor", fault="journal")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("configured database JSON is readable", result.stdout)
        self.assertIn("journal observation unavailable", result.stdout)
        self.assertNotIn("no abnormal restart loop", result.stdout)

    def test_restart_failure_reports_actual_state_without_success(self):
        result = self.invoke("restart", fault="restart")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Actual runtime mode: fast", result.stderr)
        self.assertNotIn("ZXY Panel restarted", result.stdout)
        self.assertNotIn("systemctl restart nginx", self.trace.read_text())

    def test_restart_success_requires_health_not_just_restart_exit(self):
        result = self.invoke("restart")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("health versions confirmed", result.stdout)
        self.assertNotIn("systemctl restart zxy-agent", self.trace.read_text())
        self.trace.write_text("")
        failed = self.invoke("restart", fault="health")
        self.assertNotEqual(failed.returncode, 0)
        self.assertNotIn("ZXY Panel restarted", failed.stdout)
        self.assertEqual(self.trace.read_text().count("poll interval"), 30)

    def test_backup_contains_actual_db_agent_and_bbr_but_no_program(self):
        external = self.root / "external-db.json"
        self.data.rename(external)
        env = self.app / ".env"
        env.write_text(env.read_text().replace(str(self.data), str(external)))
        (self.config / "agent.env").write_text("ZXY_SERVER_ID=synthetic-server\nZXY_AGENT_TOKEN=synthetic-agent\n")
        (self.system / "BBR_CONF_FILE").write_text("net.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n")
        (self.system / "BBR_DISABLED_FILE").write_text("disabled")
        (self.app / "bin").mkdir()
        (self.app / "bin/program").write_text("not configuration")
        before = self.snapshot()
        result = self.shell("create_backup manual")
        self.assertEqual(result.returncode, 0, result.stderr)
        path = Path(result.stdout.strip())
        self.assertTrue(path.is_relative_to(self.app / "backups"))
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.snapshot(), before)
        with tarfile.open(path) as archive:
            meta = json.load(archive.extractfile("manifest.json"))
            self.assertEqual(meta["database_path"], str(external))
            self.assertEqual(meta["format"], "zxy-panel-config-data-v1")
            self.assertEqual(archive.extractfile("files/database").read(), external.read_bytes())
            self.assertEqual(archive.extractfile("files/agent_environment").read(), (self.config / "agent.env").read_bytes())
            self.assertIn("files/bbr", archive.getnames())
            self.assertIn("files/bbr_disabled", archive.getnames())
            self.assertTrue(all(m.isfile() for m in archive.getmembers()))
            self.assertFalse(any("program" in n or "VERSION" in n for n in archive.getnames()))
            for key, record in meta["files"].items():
                value = archive.extractfile("files/" + key).read()
                self.assertEqual(hashlib.sha256(value).hexdigest(), record["sha256"])

    def test_same_second_backups_are_unique_and_chosen_input_unchanged(self):
        frozen = """import datetime
class Fixed(datetime.datetime):
    @classmethod
    def utcnow(cls): return cls(2026,1,2,3,4,5)
datetime.datetime=Fixed
"""
        first = self.shell("create_backup first", setup=self.python_fault(frozen))
        self.assertEqual(first.returncode, 0, first.stderr)
        chosen = Path(first.stdout.strip())
        before = self.snapshot(backups=True)
        second = self.shell("create_backup pre-restore", setup=self.python_fault(frozen))
        self.assertEqual(second.returncode, 0, second.stderr)
        other = Path(second.stdout.strip())
        self.assertNotEqual(chosen, other)
        self.assertIn("20260102-030405", chosen.name)
        self.assertIn("20260102-030405", other.name)
        self.assertEqual(self.snapshot(backups=True)[str(chosen.relative_to(self.root))],
                         before[str(chosen.relative_to(self.root))])
        self.assertEqual(len(list((self.app / "backups").glob("*.tar.gz"))), 2)
        self.assertFalse(list((self.app / "backups").glob("*.part")))

    def test_forced_name_collision_cannot_overwrite_prior_backup(self):
        injection = """import datetime,uuid
class Fixed(datetime.datetime):
    @classmethod
    def utcnow(cls): return cls(2026,1,2,3,4,5)
datetime.datetime=Fixed
uuid.uuid4=lambda: uuid.UUID(int=1)
"""
        first = self.shell("create_backup first", setup=self.python_fault(injection))
        self.assertEqual(first.returncode, 0, first.stderr)
        before = self.snapshot(backups=True)
        failed = self.shell("create_backup pre-restore", setup=self.python_fault(injection))
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("backup failed", failed.stderr)
        self.assertEqual(self.snapshot(backups=True), before)

    def test_backup_archive_and_publish_failures_preserve_sources_and_old_archives(self):
        self.assertEqual(self.shell("create_backup manual").returncode, 0)
        before = self.snapshot(backups=True)
        for injection in (
            "import tarfile\n"
            "def fail_add(*a,**k): raise tarfile.TarError('synthetic archive write failure')\n"
            "tarfile.TarFile.addfile=fail_add\n",
            "import os\n"
            "def fail_link(*a,**k): raise OSError('synthetic publish failure')\n"
            "os.link=fail_link\n"):
            with self.subTest(fault=injection.splitlines()[0]):
                result = self.shell("create_backup pre-install", setup=self.python_fault(injection))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("backup failed", result.stderr)
                self.assertEqual(self.snapshot(backups=True), before)
                self.assertFalse(list((self.app / "backups").glob("*.part")))

    def test_backup_non_regular_source_and_symlink_are_rejected(self):
        self.data.unlink()
        self.data.mkdir()
        before = self.snapshot()
        result = self.shell("create_backup manual")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("backup failed", result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertFalse(list((self.app / "backups").glob("*.tar.gz")))
        self.data.rmdir()
        outside = self.root / "foreign-db.json"
        outside.write_text("{}")
        self.data.symlink_to(outside)
        original = outside.read_bytes(), outside.stat().st_mtime_ns
        result = self.shell("create_backup manual")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((outside.read_bytes(), outside.stat().st_mtime_ns), original)
        self.assertFalse(list((self.app / "backups").glob("*.tar.gz")))

    def test_backup_command_failure_reports_actual_state_not_success(self):
        folder = self.app / "backups"
        folder.write_text("not a directory")
        before = self.snapshot(backups=True)
        result = self.invoke("backup")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("backup failed", result.stderr)
        self.assertIn("Actual runtime mode: fast", result.stderr)
        self.assertNotIn("Backup created", result.stdout)
        self.assertEqual(self.snapshot(backups=True), before)


    def test_installer_uses_same_backup_and_preserves_custom_roots_and_old_data(self):
        (self.config / "agent.env").write_text("ZXY_SERVER_ID=synthetic-server\nZXY_AGENT_TOKEN=synthetic-agent\n")
        before = self.snapshot()
        result = self.installer_shell('installer_backup_existing; printf "BACKUP=%s\\\\n" "$PRE_INSTALL_BACKUP"')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.snapshot(), before)
        marker = next(line for line in result.stdout.splitlines() if line.startswith("BACKUP="))
        with tarfile.open(marker.split("=", 1)[1]) as archive:
            meta = json.load(archive.extractfile("manifest.json"))
            self.assertEqual(meta["install_dir"], str(self.app))
            self.assertEqual(meta["config_dir"], str(self.config))
            self.assertEqual(meta["reason"], "pre-install")
            self.assertIn("files/agent_environment", archive.getnames())

    def test_preinstall_backup_failure_never_reaches_cleanup_or_fresh_removal(self):
        (self.app / "backups").write_text("synthetic write failure")
        before = self.snapshot(backups=True)
        result = self.installer_shell(
            'FRESH_INSTALL=true; installer_backup_existing; printf "UNSAFE cleanup\\\\n" >> "$TRACE"; clear_fresh_database')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no runtime cleanup or data removal", result.stderr)
        self.assertNotIn("UNSAFE", self.trace.read_text())
        self.assertEqual(self.snapshot(backups=True), before)

    def test_fresh_without_matching_backup_does_not_remove_data(self):
        before = self.snapshot()
        result = self.installer_shell('FRESH_INSTALL=true; clear_fresh_database')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no matching unchanged backup", result.stderr)
        self.assertEqual(self.snapshot(), before)

    def test_fresh_modified_after_backup_is_retained(self):
        extra = self.app / "data/ancillary"
        extra.write_text("not an owned DB record")
        result = self.installer_shell(
            'FRESH_INSTALL=true; installer_backup_existing; printf \'{"new_data":true}\' > "$DB_PATH"; clear_fresh_database')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(json.loads(self.data.read_text()), {"new_data": True})
        self.assertEqual(extra.read_text(), "not an owned DB record")

    def test_fresh_with_verified_backup_clears_only_owned_json_database(self):
        extra = self.app / "data/ancillary"
        extra.write_text("not an owned DB record")
        result = self.installer_shell('FRESH_INSTALL=true; installer_backup_existing; clear_fresh_database')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.data.exists())
        self.assertEqual(extra.read_text(), "not an owned DB record")
        files = list((self.app / "backups").glob("*.tar.gz"))
        self.assertEqual(len(files), 1)
        with tarfile.open(files[0]) as archive:
            self.assertEqual(json.load(archive.extractfile("files/database")),
                             dict(servers={}, clients={}, logs=[]))

    def test_post_install_doctor_failure_is_not_ignored(self):
        result = self.installer_shell('post_install_self_check; printf "FALSE SUCCESS\\\\n"',
            extra="zxy-panel() { printf 'Doctor result: FAIL\\\\n'; return 7; }")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Doctor result: FAIL", result.stdout)
        self.assertNotIn("FALSE SUCCESS", result.stdout)

    def backup(self):
        result = self.shell("create_backup manual")
        self.assertEqual(result.returncode, 0, result.stderr)
        return Path(result.stdout.strip())

    def rewritten_archive(self, source, change):
        with tarfile.open(source) as t:
            items = [(m, t.extractfile(m).read()) for m in t]
        items = change(items)
        path = self.root / "synthetic-restore.tar.gz"
        with tarfile.open(path, "w:gz") as t:
            for member, payload in items:
                member.size = len(payload)
                t.addfile(member, io.BytesIO(payload))
        path.chmod(0o600)
        return path

    def restore_setup(self, fault=""):
        (self.app / "bin").mkdir(exist_ok=True)
        (self.app / "bin/zxy-panel-api-linux-amd64").write_text("synthetic executable")
        (self.system / "API_UNIT_FILE").write_text(
            "[Unit]\nDescription=Synthetic API\n[Service]\nWorkingDirectory="+shlex.quote(str(self.app))+
            "\nEnvironmentFile="+shlex.quote(str(self.app/".env"))+
            "\nExecStart="+shlex.quote(str(self.app/"bin/zxy-panel-api-linux-amd64"))+"\n")
        proc = self.system/"PROC_DIRECTORY/777"
        proc.mkdir(parents=True,exist_ok=True)
        (proc/"environ").write_bytes(("ZXY_DB_PATH="+str(self.data)+"\0").encode())
        if not (proc/"cwd").is_symlink(): (proc/"cwd").symlink_to(self.app)
        return r'''
RESTORE_FAULT=''' + shlex.quote(fault) + r'''
systemctl() {
  printf 'systemctl %s\n' "$*" >> "$TRACE"
  if [[ "$1" == show ]]; then
    case "$2:$4" in
      zxy-panel-api:FragmentPath) printf '%s\n' "$LAB/system/API_UNIT_FILE";;
      zxy-panel-api:EnvironmentFiles) printf '%s/.env (ignore_errors=no)\n' "$APP_DIR";;
      zxy-panel-api:WorkingDirectory) printf '%s\n' "$APP_DIR";;
      zxy-panel-api:ExecStart) printf '{ path=%s/bin/zxy-panel-api-linux-amd64 ; argv[]=%s/bin/zxy-panel-api-linux-amd64 ; }\n' "$APP_DIR" "$APP_DIR";;
      zxy-panel-api:MainPID) printf '777\n';;
      *:UnsetEnvironment) [[ "$RESTORE_FAULT" != unset ]] || printf 'ZXY_DB_PATH\n';;
      *:StartLimitAction|*:FailureAction|*:SuccessAction)
       if [[ "$RESTORE_FAULT" == host-action ]]; then printf 'poweroff\n'; else printf 'none\n'; fi;;
      *:OnFailure|*:OnSuccess) ;;
      zxy-panel-api:DropInPaths) [[ "$RESTORE_FAULT" != foreign ]] || printf '/foreign/override.conf\n';;
      zxy-agent:LoadState|xray:LoadState) printf 'not-found\n';;
      *:ExecStartPre|*:ExecStartPost|*:ExecStop|*:ExecStopPost|*:ExecReload) ;;
      *) return 91;;
    esac
    return
  fi
  if [[ "$1" == is-active ]]; then printf 'active\n'; return; fi
  if [[ "$1" == stop && "$RESTORE_FAULT" == stop ]]; then return 17; fi
  if [[ "$1" == start && "$RESTORE_FAULT" == start ]]; then return 18; fi
  [[ "$1" == start || "$1" == stop ]]
}
nginx() { printf 'nginx %s\n' "$*" >> "$TRACE"; }
curl() {
  printf 'curl %s\n' "${@: -1}" >> "$TRACE"
  if [[ "$RESTORE_FAULT" == health ]]; then printf '<html>not health</html>'; return; fi
  printf '{"status":"ok","service":"zxy-panel-api","version":"0.7.8-stable-engineering"}'
}
sleep() { printf 'bounded health poll\n' >> "$TRACE"; }
'''

    def restore_fault(self):
        return r'''
python3() {
  if [[ "$1" != - ]]; then command python3 "$@"; return; fi
  local script
  script=$(cat)
  if [[ "$script" == *"action, work, archive, app"* && "$2" == apply ]]; then
    script=$'import os\nreal_replace=os.replace\ncalls=0\ndef fail_once(a,b):\n global calls\n calls+=1\n if calls==2: raise OSError("synthetic apply failure")\n return real_replace(a,b)\nos.replace=fail_once\n'"$script"
  fi
  command python3 "$@" <<< "$script"
}
'''

    def test_restore_success_recovers_only_config_data_and_keeps_current_program(self):
        setup = self.restore_setup()
        old = self.data.read_bytes()
        archive = self.backup()
        chosen = archive.read_bytes(), archive.stat().st_mtime_ns
        self.data.write_text('{"clients":{"new":"synthetic"},"logs":["new"]}')
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Configuration/data restore completed", result.stdout)
        self.assertEqual(self.data.read_bytes(), old)
        after = self.snapshot()
        for key in before:
            if key not in ("app/data/zxy-panel.json", "app/.env", "config/panel.info"):
                self.assertEqual(after[key], before[key], key)
        self.assertEqual((archive.read_bytes(), archive.stat().st_mtime_ns), chosen)
        self.assertEqual(len(list((self.app / "backups").glob("*.tar.gz"))), 2)
        trace = self.trace.read_text()
        self.assertLess(trace.index("systemctl stop"), trace.index("systemctl start"))
        self.assertIn("/fixturebase/api/health", trace)
        self.assertNotIn("daemon-reload", trace)
        self.assertNotIn("reload nginx", trace)

    def test_restore_apply_failure_recovers_full_bytes_mode_and_mtime(self):
        setup = self.restore_setup() + self.restore_fault()
        archive = self.backup()
        self.data.write_text('{"clients":{"retained":"synthetic"},"logs":["retained"]}')
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("restore completed", result.stdout)
        self.assertEqual(self.snapshot(), before)
        self.assertIn("Actual runtime mode", result.stderr)

    def test_restore_start_and_health_failure_are_nonzero_with_recovery(self):
        for fault in ("start", "health"):
            with self.subTest(fault=fault):
                setup = self.restore_setup(fault)
                archive = self.backup()
                self.data.write_text('{"retained":"synthetic"}')
                before = self.snapshot()
                result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("restore completed", result.stdout)
                self.assertEqual(self.snapshot(), before)
                self.assertIn("Actual runtime mode", result.stderr)

    def test_restore_stop_failure_never_applies_files(self):
        setup = self.restore_setup("stop")
        archive = self.backup()
        self.data.write_text('{"retained":"synthetic"}')
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no restored files applied", result.stderr)
        self.assertEqual(self.snapshot(), before)

    def test_restore_foreign_unit_refused_before_backup_or_stop(self):
        setup = self.restore_setup("foreign")
        archive = self.backup()
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("systemctl stop", self.trace.read_text())
        self.assertEqual(len(list((self.app / "backups").glob("*.tar.gz"))), 1)
        self.assertEqual(self.snapshot(), before)

    def test_restore_bad_members_duplicates_links_mapping_and_hash_never_stop_writers(self):
        setup = self.restore_setup()
        archive = self.backup()
        before = self.snapshot()
        def extra(items, name, link=False):
            entry = tarfile.TarInfo(name)
            if link:
                entry.type, entry.linkname = tarfile.SYMTYPE, "/outside"
            return items + [(entry, b"" if link else b"synthetic")]
        def metadata(items):
            out = []
            for member, value in items:
                if member.name == "manifest.json":
                    meta = json.loads(value)
                    meta["database_path"] = "/outside/database.json"
                    value = json.dumps(meta).encode()
                out.append((member, value))
            return out
        for change in (
            lambda items: extra(items, "../outside"),
            lambda items: extra(items, "files/unknown"),
            lambda items: extra(items, "files/database", True),
            lambda items: items + [items[0]],
            metadata,
            lambda items: [(m, b'{"bad_hash":true}' if m.name == "files/database" else v) for m,v in items],
        ):
            with self.subTest(case=repr(change)):
                bad = self.rewritten_archive(archive, change)
                self.trace.write_text("")
                result = self.shell("restore_backup " + shlex.quote(str(bad)), setup)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.snapshot(), before)
                self.assertNotIn("systemctl stop", self.trace.read_text())
                self.assertNotIn("restore completed", result.stdout)

    def test_restore_legacy_absolute_tar_is_refused(self):
        setup = self.restore_setup()
        archive = self.backup()
        legacy = self.rewritten_archive(archive, lambda items: [
            (tarfile.TarInfo("opt/zxy-panel/data/zxy-panel.json"), b"{}")])
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(legacy)), setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.snapshot(), before)
        self.assertNotIn("systemctl stop", self.trace.read_text())

    def test_restore_incompatible_port_rejected_before_stop(self):
        setup = self.restore_setup()
        archive = self.backup()
        env = self.app / ".env"
        env.write_text(env.read_text().replace("API_PORT=18088","API_PORT=18089"))
        before = self.snapshot()
        result = self.shell("restore_backup " + shlex.quote(str(archive)), setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("deployment mapping", result.stderr)
        self.assertNotIn("systemctl stop", self.trace.read_text())
        self.assertEqual(self.snapshot(), before)

    def test_backup_fifo_is_rejected_without_blocking(self):
        self.data.unlink()
        os.mkfifo(self.data)
        with subprocess.Popen(["bash","-euc","source "+shlex.quote(str(self.cli))+
                               "; create_backup manual"], env=self.env, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, start_new_session=True) as process:
            try:
                _, stderr = process.communicate(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate()
                raise
            self.assertNotEqual(process.returncode,0)
            self.assertIn("backup failed",stderr)
        self.assertTrue(self.data.is_fifo())

    def test_fresh_rejects_missing_or_corrupt_database_member_before_removal(self):
        archive = self.backup()
        before = self.snapshot()
        for change in (
            lambda items: [(m,v) for m,v in items if m.name != "files/database"],
            lambda items: [(m,b'{"corrupt":true}' if m.name == "files/database" else v) for m,v in items],
        ):
            bad = self.rewritten_archive(archive,change)
            result = self.installer_shell("FRESH_INSTALL=true; PRE_INSTALL_BACKUP="+
                shlex.quote(str(bad))+"; clear_fresh_database")
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(self.snapshot(),before)

    def test_fresh_bbr_marker_requires_verified_backup_even_without_database(self):
        self.data.unlink()
        marker = self.system / "BBR_DISABLED_FILE"
        marker.write_text("synthetic disabled")
        before = self.snapshot()
        failure = self.installer_shell("FRESH_INSTALL=true; clear_fresh_database")
        self.assertNotEqual(failure.returncode,0)
        self.assertEqual(self.snapshot(),before)
        result = self.installer_shell("FRESH_INSTALL=true; installer_backup_existing; clear_fresh_database")
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertFalse(marker.exists())
        self.assertEqual(len(list((self.app/"backups").glob("*.tar.gz"))),1)

    def test_upgrade_command_preserves_custom_installation_roots(self):
        command = self.command(self.manifest())
        self.assertIn("APP_DIR="+shlex.quote(str(self.app)),command)
        self.assertIn("CONFIG_DIR="+shlex.quote(str(self.config)),command)

    def test_restart_docker_restarts_both_owned_services(self):
        info = self.config / "panel.info"
        info.write_text(info.read_text().replace("INSTALL_MODE=fast","INSTALL_MODE=docker"))
        result = self.invoke("restart")
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn("restart zxy-panel-api zxy-panel-frontend",self.trace.read_text())
        self.assertNotIn("systemctl restart zxy-panel-api",self.trace.read_text())

    def test_restart_prefix_health_failure_cannot_report_success(self):
        setup = r'''
systemctl() { [[ "$1" != show ]] || printf 'not-found\n'; }
nginx() { return 0; }
curl() {
  if [[ "${@: -1}" == *"/fixturebase/api/health" ]]; then printf '<html>SPA</html>'
  else printf '{"status":"ok","service":"zxy-panel-api","version":"0.7.8-stable-engineering"}'; fi
}
sleep() { printf 'bounded poll\n' >> "$TRACE"; }
'''
        result = self.shell("restart_panel",setup)
        self.assertNotEqual(result.returncode,0)
        self.assertNotIn("Panel restarted",result.stdout)
        self.assertEqual(self.trace.read_text().count("bounded poll"),30)

    def test_panel_info_publication_and_write_failure_preserve_previous(self):
        variables = r'''
ADMIN_USERNAME=synthetic-admin
ADMIN_PASSWORD_DISPLAY=synthetic-only
PANEL_PORT=49321
WEB_BASE_PATH=fixturebase
PUBLIC_IP=127.0.0.1
AGENT_SECRET=synthetic-only
VERSION=0.7.8-stable-engineering
COMPOSE_PROJECT=''
AUTO_AGENT=false
INSTALL_XRAY=false
SETUP_XRAY_SERVICE=false
'''
        result = self.installer_shell("write_panel_info", extra=variables)
        self.assertEqual(result.returncode,0,result.stderr)
        p = self.config/"panel.info"
        before = self.snapshot()
        self.assertEqual(p.stat().st_mode & 0o777,0o600)
        self.assertIn("CONFIG_DIR="+str(self.config),p.read_text())
        for injected in ('cat() { return 17; }', 'mv() { return 18; }',
                         'chmod() { return 19; }'):
            with self.subTest(fault=injected):
                failed = self.installer_shell("write_panel_info", extra=variables+"\n"+injected)
                self.assertNotEqual(failed.returncode,0)
                self.assertEqual(self.snapshot(),before)
                self.assertFalse(list(self.config.glob(".panel-info-*")))

    def test_cli_publication_changes_only_configuration_binding_and_failure_retains_previous(self):
        path = self.app/"scripts/zxy-panel"
        path.parent.mkdir()
        path.write_bytes((REPO/"scripts/zxy-panel").read_bytes())
        result = self.installer_shell("install_cli")
        self.assertEqual(result.returncode,0,result.stderr)
        published = self.bin/"published-cli"
        expected = path.read_text().replace('CONFIG_DIR="/etc/zxy-panel"',
                         "CONFIG_DIR="+shlex.quote(str(self.config)),1)
        self.assertEqual(published.read_text(),expected)
        self.assertEqual(published.stat().st_mode & 0o777,0o755)
        old = published.read_bytes(), published.stat().st_mtime_ns
        injected = r'''
python3() {
 local body; body=$(cat)
 if [[ "$body" == *"marker = 'CONFIG_DIR="* ]]; then
   body=$'import os\ndef fail(*a,**k): raise OSError("synthetic publication failure")\nos.replace=fail\n'"$body"
 fi
 command python3 "$@" <<< "$body"
}
'''
        failed = self.installer_shell("install_cli",extra=injected)
        self.assertNotEqual(failed.returncode,0)
        self.assertEqual((published.read_bytes(),published.stat().st_mtime_ns),old)
        self.assertFalse([p for p in self.bin.iterdir() if p.name.startswith("tmp")])

    def test_environment_duplicate_invalid_and_explicit_empty_fail_without_fallback(self):
        env = self.app/".env"
        for content in ("API_PORT=18088\nAPI_PORT=18089\n", "invalid line\n",
                        "API_PORT='unterminated\n", "API_PORT=\n"):
            with self.subTest(content=content):
                env.write_text(content)
                result = self.shell("operational_value API_PORT 8088")
                self.assertNotEqual(result.returncode,0)
                self.assertNotIn("8088",result.stdout)

    def test_docker_environment_interpolation_and_staged_agent_literal_parsing(self):
        info = self.config/"panel.info"
        info.write_text(info.read_text().replace("INSTALL_MODE=fast","INSTALL_MODE=docker"))
        (self.app/".env").write_text("BASE=fixture\nVALUE=\"${BASE}-$$-suffix\" #wrong\n")
        # Supported Compose double quotes must end at the quote; malformed
        # comments are rejected rather than silently changing configuration.
        self.assertNotEqual(self.shell("env_file_value VALUE").returncode,0)
        (self.app/".env").write_text('BASE=fixture\nVALUE="${BASE}-$$-suffix"\n')
        result = self.shell("env_file_value VALUE")
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(result.stdout,"fixture-$-suffix")
        staged = self.root/"staged-agent.env"
        staged.write_text('ZXY_AGENT_TOKEN="synthetic$BASE"\n')
        result = self.shell('env_file_value ZXY_AGENT_TOKEN "$LAB/staged-agent.env" value fast')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(result.stdout,"synthetic$BASE")

    def test_health_transport_failure_and_empty_version_never_succeed(self):
        for body in ('health_check http://127.0.0.1:18088/api/health 0.7.8-stable-engineering',
                     "health_check http://127.0.0.1:18088/api/health ''"):
            result = self.shell(body, "curl() { return 17; }")
            self.assertNotEqual(result.returncode,0)

    def test_restore_symlink_destination_is_rejected_without_touching_target(self):
        setup = self.restore_setup()
        archive = self.backup()
        outside = self.root/"outside.json"
        self.data.rename(outside)
        self.data.symlink_to(outside)
        original = outside.read_bytes(), outside.stat().st_mtime_ns
        result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((outside.read_bytes(),outside.stat().st_mtime_ns),original)
        self.assertNotIn("systemctl stop",self.trace.read_text())

    def test_restore_retained_agent_identity_must_exist_in_restored_database(self):
        setup = self.restore_setup()
        (self.config/"agent.env").write_text(
            "ZXY_SERVER_ID=synthetic-server\nZXY_AGENT_TOKEN=synthetic-agent\n")
        archive = self.backup()
        before = self.snapshot()
        result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
        self.assertNotEqual(result.returncode,0)
        self.assertIn("retained Agent identity",result.stderr)
        self.assertEqual(self.snapshot(),before)
        self.assertNotIn("systemctl stop",self.trace.read_text())

    def docker_restore_setup(self,fault=""):
        return self.restore_setup() + "DOCKER_RESTORE_FAULT="+shlex.quote(fault)+"\n"+r'''
docker() {
 printf 'docker %s\n' "$*" >> "$TRACE"
 case "$1" in
  context) printf 'default\n';;
  compose)
   if [[ "$*" == *"up --help" ]]; then
    if [[ "$DOCKER_RESTORE_FAULT" != no-pull-option ]]; then printf '  --pull string  Pull policy\n'; fi
   fi
   if [[ "$*" == *"config --format json" ]]; then
    python3 - "$APP_DIR" "$DOCKER_RESTORE_FAULT" <<'PY_MODEL'
import json,sys
app,fault=sys.argv[1:]
services={}
for name,target,published,folder in [('zxy-panel-api',8088,'18088','backend'),
 ('zxy-panel-frontend',5173,'15173','frontend')]:
 services[name]=dict(container_name=name,build=dict(context=app+'/'+folder),
  ports=[dict(target=target,published=published,host_ip='127.0.0.1',protocol='tcp')])
services['zxy-panel-api'].update(volumes=[dict(type='bind',source=app+'/data',target='/app/data')],
 environment={'ZXY_DB_PATH':'/app/data/zxy-panel.json'})
if fault=='foreign-volume': services['zxy-panel-api']['volumes'][0]['source']=app+'/foreign-data'
if fault=='foreign-port': services['zxy-panel-api']['ports'][0]['host_ip']='0.0.0.0'
if fault=='swapped-image': services['zxy-panel-api']['image']='synthetic-project-zxy-panel-frontend'
if fault=='always-pull': services['zxy-panel-api']['pull_policy']='always'
print(json.dumps(dict(services=services)))
PY_MODEL
   fi
   if [[ "$*" == *"up -d --no-build"* ]]; then
    [[ "$*" == *"--pull never"* ]] || return 93
    printf 'recreated\n' > "$LAB/recreated"
   fi
   return 0;;
  image) printf 'sha256:%s\n' "${@: -1}";;
  inspect)
   if [[ "$2" == --format ]]; then
    case "$3" in
     '{{.State.Running}}') printf 'true\n';;
     '{{.Config.Image}}') printf 'synthetic-project-%s\n' "$4";;
     '{{.Image}}') printf 'sha256:synthetic-project-%s\n' "$4";;
     *) return 91;;
    esac
   else
    python3 - "$APP_DIR" "$2" "$DOCKER_RESTORE_FAULT" <<'PY_CONTAINER'
import json,sys
app,service,fault=sys.argv[1:]
target,port=(8088,'18088') if service=='zxy-panel-api' else (5173,'15173')
tag='synthetic-project-'+service
environment=['ZXY_DB_PATH='+('/unexpected/database.json' if fault=='actual-env' else '/app/data/zxy-panel.json')] if service=='zxy-panel-api' else []
print(json.dumps([dict(Name='/'+service,Image='sha256:'+tag,Config=dict(Image=tag,Env=environment,Labels={
 'com.docker.compose.project':'synthetic-project',
 'com.docker.compose.service':service,
 'com.docker.compose.project.working_dir':app,
 'com.docker.compose.project.config_files':app+'/docker-compose.yml'}),
 HostConfig=dict(PortBindings={str(target)+'/tcp':[dict(HostIp='127.0.0.1',HostPort=port)]}),
 Mounts=[dict(Type='bind',Source=app+'/data',Destination='/app/data',RW=True)] if service=='zxy-panel-api' else [])]))
PY_CONTAINER
   fi;;
  *) return 92;;
 esac
}
'''
    def test_restore_docker_recreates_only_current_images_without_build(self):
        info = self.config/"panel.info"
        info.write_text(info.read_text().replace("INSTALL_MODE=fast","INSTALL_MODE=docker"))
        setup = self.docker_restore_setup()
        archive = self.backup()
        original = self.data.read_bytes()
        self.data.write_text('{"new":"synthetic"}')
        result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(self.data.read_bytes(),original)
        trace = self.trace.read_text()
        self.assertIn("up -d --no-build --pull never --force-recreate --no-deps zxy-panel-api zxy-panel-frontend",trace)
        self.assertNotIn("systemctl stop zxy-panel-api",trace)
        self.assertNotIn("--build ",trace)

    def test_restore_docker_model_mapping_conflicts_are_rejected_before_stop(self):
        info = self.config/"panel.info"
        info.write_text(info.read_text().replace("INSTALL_MODE=fast","INSTALL_MODE=docker"))
        archive = self.backup()
        for fault in ("foreign-volume","foreign-port","swapped-image","actual-env","always-pull","no-pull-option"):
            with self.subTest(fault=fault):
                setup = self.docker_restore_setup(fault)
                before = self.snapshot()
                self.trace.write_text("")
                result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
                self.assertNotEqual(result.returncode,0)
                self.assertEqual(self.snapshot(),before)
                self.assertNotIn("stop zxy-panel",self.trace.read_text())
                self.assertNotIn("up -d",self.trace.read_text())
                self.assertNotIn("restore completed",result.stdout)

    def test_restore_failed_health_after_normal_runtime_write_recovers_pre_restore_database(self):
        setup = self.restore_setup("health")+r'''
definition=$(declare -f systemctl)
eval "${definition/systemctl/fixture_systemctl}"
systemctl() {
 if [[ "$1" == start && "$2" == zxy-panel-api && ! -f "$LAB/runtime-write" ]]; then
  printf '{"normal_runtime_write":true}' > "$APP_DIR/data/zxy-panel.json"
  printf 'written\n' > "$LAB/runtime-write"
 fi
 fixture_systemctl "$@"
}
'''
        archive = self.backup()
        self.data.write_text('{"before_restore":"retained","logs":["retained"]}')
        before = self.snapshot()
        result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.snapshot(),before)
        self.assertNotIn("restore completed",result.stdout)
        evidence = list((self.app/"backups").glob(".restore-*/failed-runtime-database"))
        self.assertEqual(len(evidence),1)
        self.assertEqual(json.loads(evidence[0].read_text()),dict(normal_runtime_write=True))

    def test_bbr_only_first_install_uses_validated_parent_mode_not_identity_guess(self):
        self.data.unlink()
        (self.app/".env").unlink()
        (self.config/"panel.info").unlink()
        marker = self.system/"BBR_DISABLED_FILE"
        marker.write_text("synthetic disabled")
        result = self.installer_shell("FRESH_INSTALL=true; installer_backup_existing; clear_fresh_database")
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertFalse(marker.exists())
        files = list((self.app/"backups").glob("*.tar.gz"))
        self.assertEqual(len(files),1)
        with tarfile.open(files[0]) as t:
            meta = json.load(t.extractfile("manifest.json"))
            self.assertEqual(meta["install_mode"],"fast")
            self.assertIn("files/bbr_disabled",t.getnames())

    def test_latest_backup_uses_mtime_not_uuid_and_refuses_timestamp_tie(self):
        first,second = self.backup(),self.backup()
        low,high = sorted((first,second))
        os.utime(high,ns=(1000000000,1000000000))
        os.utime(low,ns=(2000000000,2000000000))
        result = self.shell("latest_backup_file")
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(Path(result.stdout.strip()),low)
        os.utime(high,ns=(2000000000,2000000000))
        before = self.snapshot(backups=True)
        result = self.shell("latest_backup_file")
        self.assertNotEqual(result.returncode,0)
        self.assertIn("ambiguous",result.stderr)
        self.assertEqual(self.snapshot(backups=True),before)

    def test_restore_fast_actual_environment_unset_and_host_actions_rejected_before_stop(self):
        for fault in ("unset","host-action","process-environment"):
            with self.subTest(fault=fault):
                setup = self.restore_setup(fault)
                archive = self.backup()
                if fault=="process-environment":
                    (self.system/"PROC_DIRECTORY/777/environ").write_bytes(
                        b"ZXY_DB_PATH=/unexpected/database.json\0")
                before = self.snapshot()
                self.trace.write_text("")
                result = self.shell("restore_backup "+shlex.quote(str(archive)),setup)
                self.assertNotEqual(result.returncode,0)
                self.assertEqual(self.snapshot(),before)
                self.assertNotIn("systemctl stop",self.trace.read_text())
                self.assertNotIn("restore completed",result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
