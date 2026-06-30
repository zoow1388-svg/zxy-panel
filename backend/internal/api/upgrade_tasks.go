// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type upgradeTaskState struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	Stage          string         `json:"stage"`
	Message        string         `json:"message"`
	CurrentVersion string         `json:"current_version"`
	TargetVersion  string         `json:"target_version"`
	Package        string         `json:"package"`
	DownloadURL    string         `json:"download_url"`
	SHA256         string         `json:"sha256"`
	LogFile        string         `json:"log_file"`
	WorkDir        string         `json:"work_dir"`
	StartedAt      string         `json:"started_at"`
	UpdatedAt      string         `json:"updated_at"`
	CompletedAt    string         `json:"completed_at,omitempty"`
	ExitCode       int            `json:"exit_code,omitempty"`
	Error          string         `json:"error,omitempty"`
	Manifest       updateManifest `json:"manifest,omitempty"`
	Stale          bool           `json:"stale,omitempty"`
	StaleReason    string         `json:"stale_reason,omitempty"`
}

func (r *Router) updateTaskLatest(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	state, err := readUpgradeTaskState()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "has_task": false, "message": "暂无升级任务"})
		return
	}
	state = normalizeUpgradeTaskState(state)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "has_task": true, "task": state})
}

func (r *Router) updateTaskLogs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	state, _ := readUpgradeTaskState()
	logFile := state.LogFile
	if logFile == "" {
		logFile = filepath.Join(installDir, "logs", "upgrade-task.log")
	}
	limit := int64(128 * 1024)
	text := tailFile(logFile, limit)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "log_file": logFile, "logs": text})
}

func (r *Router) updateTaskPrecheck(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	checks := upgradePrechecks()
	ok := true
	for _, c := range checks {
		if v, _ := c["ok"].(bool); !v {
			ok = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "checks": checks, "message": precheckMessage(ok)})
}

func (r *Router) updateTaskClearStale(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	state, err := readUpgradeTaskState()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "暂无需要清理的升级任务"})
		return
	}
	state = normalizeUpgradeTaskState(state)
	if !state.Stale && isActiveUpgradeStatus(state.Status) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "当前任务仍在执行中，尚未达到卡死清理条件。", "task": state})
		return
	}
	if state.Status == "success" || state.Status == "failed" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "任务已结束，无需清理。", "task": state})
		return
	}
	state.Status = "failed"
	state.Stage = "stale_cleaned"
	state.Message = "已由管理员清理卡死升级任务。日志和备份已保留，可重新检查更新或使用 SSH 命令兜底。"
	state.Error = firstNonEmpty(state.StaleReason, "upgrade task stale and manually cleaned")
	state.ExitCode = 98
	state.CompletedAt = time.Now().Format(time.RFC3339)
	state.Stale = true
	if state.StaleReason == "" {
		state.StaleReason = "任务超时未更新，已清理为 failed。"
	}
	_ = writeUpgradeTaskState(state)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": state.Message, "task": state})
}

func (r *Router) updateTaskCreate(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !managedUpgradeSupported() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "当前运行模式暂不支持后台托管升级。请继续使用复制升级命令作为兜底方案。fast/systemd 模式支持托管升级。"})
		return
	}
	if runningUpgradeTask() {
		state, _ := readUpgradeTaskState()
		state = normalizeUpgradeTaskState(state)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "已有升级任务正在执行，请等待完成。若任务超过 10 分钟未更新，可先清理卡死任务。", "task": state})
		return
	}
	manifestURL := updateManifestURL()
	if manifestURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "远程版本清单未配置，无法托管升级。"})
		return
	}
	manifest, err := fetchUpdateManifest(req.Context(), manifestURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "远程版本清单不可用：" + err.Error()})
		return
	}
	pkg := strings.TrimSpace(manifest.Package)
	if pkg == "" && manifest.DownloadURL != "" {
		parts := strings.Split(manifest.DownloadURL, "/")
		pkg = parts[len(parts)-1]
	}
	target := targetManifestVersion(manifest)
	if ok, message := upgradeAllowedMessage(target); !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               false,
			"current_version":  panelVersion,
			"target_version":   target,
			"update_available": false,
			"message":          message,
		})
		return
	}
	if pkg == "" || strings.TrimSpace(manifest.DownloadURL) == "" || strings.TrimSpace(manifest.SHA256) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "version.json 缺少 package、download_url 或 sha256，禁止托管升级。"})
		return
	}
	if !safePackageName(pkg) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "升级包文件名不安全，已拒绝。"})
		return
	}
	id := "upgrade_" + time.Now().Format("20060102_150405")
	task := upgradeTaskState{
		ID:             id,
		Status:         "pending",
		Stage:          "created",
		Message:        "升级任务已创建，等待独立 systemd runner 接管。",
		CurrentVersion: panelVersion,
		TargetVersion:  target,
		Package:        pkg,
		DownloadURL:    manifest.DownloadURL,
		SHA256:         manifest.SHA256,
		LogFile:        filepath.Join(installDir, "logs", id+".log"),
		WorkDir:        filepath.Join("/root/zxy-panel-upgrades", id),
		StartedAt:      time.Now().Format(time.RFC3339),
		UpdatedAt:      time.Now().Format(time.RFC3339),
		Manifest:       manifest,
	}
	if err := os.MkdirAll(filepath.Join(installDir, "logs"), 0755); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "无法创建日志目录：" + err.Error()})
		return
	}
	if err := writeUpgradeTaskState(task); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "无法写入任务状态：" + err.Error()})
		return
	}
	script, err := writeUpgradeRunner(task, manifestURL)
	if err != nil {
		task.Status = "failed"
		task.Stage = "prepare_failed"
		task.Error = err.Error()
		_ = writeUpgradeTaskState(task)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "无法生成升级脚本：" + err.Error(), "task": task})
		return
	}
	if err := startUpgradeRunner(task.ID, script); err != nil {
		task.Status = "failed"
		task.Stage = "start_failed"
		task.Error = err.Error()
		_ = writeUpgradeTaskState(task)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "升级任务启动失败：" + err.Error(), "task": task})
		return
	}
	task.Status = "running"
	task.Stage = "queued"
	task.Message = "升级任务已交给独立 systemd runner 执行。API 重启不会中断升级；页面可继续刷新查看日志。"
	_ = writeUpgradeTaskState(task)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": task.Message, "task": task})
}

func upgradeStatePath() string { return filepath.Join(installDir, "data", "upgrade-task-latest.json") }

func readUpgradeTaskState() (upgradeTaskState, error) {
	var s upgradeTaskState
	raw, err := os.ReadFile(upgradeStatePath())
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	return s, nil
}

func writeUpgradeTaskState(s upgradeTaskState) error {
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	p := upgradeStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0600)
}

func runningUpgradeTask() bool {
	s, err := readUpgradeTaskState()
	if err != nil {
		return false
	}
	s = normalizeUpgradeTaskState(s)
	return isActiveUpgradeStatus(s.Status) && !s.Stale
}

func isActiveUpgradeStatus(status string) bool {
	switch status {
	case "pending", "running", "queued", "downloading", "verifying", "backing_up", "installing", "restarting":
		return true
	default:
		return false
	}
}

func normalizeUpgradeTaskState(s upgradeTaskState) upgradeTaskState {
	if !isActiveUpgradeStatus(s.Status) {
		return s
	}
	updated, err := time.Parse(time.RFC3339, s.UpdatedAt)
	if err != nil {
		updated, _ = time.Parse("2006-01-02T15:04:05Z", s.UpdatedAt)
	}
	if updated.IsZero() || time.Since(updated) <= 10*time.Minute {
		return s
	}
	if versionMatchesTarget(panelVersion, s.TargetVersion) {
		s.Status = "success"
		s.Stage = "recovered_success"
		s.Message = "检测到当前 API 版本已是目标版本，已自动恢复升级任务为成功。"
		s.Error = ""
		s.CompletedAt = time.Now().Format(time.RFC3339)
		_ = writeUpgradeTaskState(s)
		return s
	}
	s.Stale = true
	s.StaleReason = "任务超过 10 分钟未更新，且当前 API 版本仍未变成目标版本。可能在 install.sh 重启 API 前后被中断。"
	return s
}

func versionMatchesTarget(current, target string) bool {
	current = strings.TrimSpace(current)
	target = strings.TrimSpace(target)
	if current == "" || target == "" {
		return false
	}
	return current == target || strings.HasPrefix(current, target) || strings.HasPrefix(target, current)
}

func managedUpgradeSupported() bool {
	if _, err := os.Stat("/etc/zxy-panel/panel.info"); err != nil {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return true
}

func safePackageName(s string) bool {
	if strings.Contains(s, "/") || strings.Contains(s, "..") || strings.TrimSpace(s) == "" {
		return false
	}
	return regexp.MustCompile(`^[A-Za-z0-9._+-]+\.zip$`).MatchString(s)
}

func startUpgradeRunner(taskID, script string) error {
	unit := "zxy-panel-upgrade-" + strings.ReplaceAll(taskID, "_", "-")
	if commandExists("systemd-run") {
		cmd := exec.Command("systemd-run", "--unit", unit, "--collect", "--property=Type=simple", "--property=KillMode=process", "/bin/bash", script)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		return fmt.Errorf("systemd-run failed: %s: %s", err.Error(), strings.TrimSpace(string(out)))
	}
	cmd := exec.Command("setsid", "bash", script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func writeUpgradeRunner(task upgradeTaskState, manifestURL string) (string, error) {
	scriptPath := filepath.Join(installDir, "logs", task.ID+".sh")
	statusPath := upgradeStatePath()
	backupDir := filepath.Join(installDir, "backups", "upgrade", task.ID)
	content := fmt.Sprintf(`#!/usr/bin/env bash
set -u
TASK_ID=%q
STATUS_FILE=%q
LOG_FILE=%q
WORK_DIR=%q
BACKUP_DIR=%q
PKG=%q
DOWNLOAD_URL=%q
SHA256=%q
MANIFEST_URL=%q
INSTALL_DIR=%q
CURRENT_VERSION=%q
TARGET_VERSION=%q
API_URL="http://127.0.0.1:8088/api/health"

mkdir -p "$(dirname "$STATUS_FILE")" "$(dirname "$LOG_FILE")" "$WORK_DIR" "$BACKUP_DIR"
exec >>"$LOG_FILE" 2>&1

write_status() {
  local status="$1" stage="$2" message="$3" exit_code="${4:-0}" error_msg="${5:-}"
  python3 - "$STATUS_FILE" "$TASK_ID" "$status" "$stage" "$message" "$exit_code" "$error_msg" "$LOG_FILE" "$WORK_DIR" "$PKG" "$DOWNLOAD_URL" "$SHA256" "$CURRENT_VERSION" "$TARGET_VERSION" <<'PY_STATUS'
import json, sys, time
path, task_id, status, stage, message, exit_code, error_msg, log_file, work_dir, pkg, dl, sha, cur, target = sys.argv[1:]
try:
    with open(path, encoding='utf-8') as f:
        data=json.load(f)
except Exception:
    data={}
data.update({
    'id': task_id,
    'status': status,
    'stage': stage,
    'message': message,
    'exit_code': int(exit_code or 0),
    'error': error_msg,
    'log_file': log_file,
    'work_dir': work_dir,
    'package': pkg,
    'download_url': dl,
    'sha256': sha,
    'current_version': cur,
    'target_version': target,
    'updated_at': time.strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', time.gmtime()),
})
if not data.get('started_at'):
    data['started_at']=data['updated_at']
if status in ('success','failed'):
    data['completed_at']=data['updated_at']
data.pop('stale', None)
data.pop('stale_reason', None)
with open(path, 'w', encoding='utf-8') as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
PY_STATUS
}

fail() {
  local code="$1" stage="$2" msg="$3"
  echo "[FAILED] $stage: $msg"
  write_status failed "$stage" "$msg" "$code" "$msg"
  exit "$code"
}

trap 'fail $? unexpected "upgrade script interrupted"' ERR

echo "========== ZXY Panel Upgrade Task $TASK_ID =========="
echo "Start: $(date)"
echo "Current: $CURRENT_VERSION"
echo "Target: $TARGET_VERSION"
echo "Package: $PKG"
echo "Runner PID: $$"
write_status running precheck "升级前检查中"

command -v curl >/dev/null 2>&1 || fail 11 precheck "curl not found"
command -v unzip >/dev/null 2>&1 || fail 12 precheck "unzip not found"
command -v sha256sum >/dev/null 2>&1 || fail 13 precheck "sha256sum not found"
command -v python3 >/dev/null 2>&1 || fail 14 precheck "python3 not found"

free_kb=$(df -Pk /root | awk 'NR==2{print $4}')
if [ "${free_kb:-0}" -lt 1048576 ]; then
  fail 15 precheck "磁盘可用空间不足 1GB"
fi

cd "$WORK_DIR" || fail 16 precheck "无法进入升级工作目录"
rm -f "$PKG"
write_status downloading downloading "正在下载升级包"
echo "Downloading: $DOWNLOAD_URL"
curl -fL --retry 3 --connect-timeout 10 -o "$PKG" "$DOWNLOAD_URL" || fail 21 downloading "升级包下载失败"

write_status verifying verifying "正在校验 SHA256"
echo "$SHA256  $PKG" | sha256sum -c - || fail 22 verifying "SHA256 校验失败"

write_status backing_up backing_up "正在备份当前版本配置和数据"
mkdir -p "$BACKUP_DIR"
[ -d /opt/zxy-panel/data ] && tar -czf "$BACKUP_DIR/data.tar.gz" -C /opt/zxy-panel data || true
[ -d /etc/zxy-panel ] && tar -czf "$BACKUP_DIR/etc-zxy-panel.tar.gz" -C /etc zxy-panel || true
[ -f /etc/nginx/conf.d/zxy-panel.conf ] && cp -a /etc/nginx/conf.d/zxy-panel.conf "$BACKUP_DIR/zxy-panel.nginx.conf" || true
[ -f /opt/zxy-panel/.env ] && cp -a /opt/zxy-panel/.env "$BACKUP_DIR/dot-env" || true

echo "Unzipping package"
rm -rf "${PKG%%.zip}"
unzip -o "$PKG" || fail 31 installing "解压升级包失败"
cd "${PKG%%.zip}" || fail 32 installing "解压目录不存在"

write_status installing installing "正在执行 deploy/install.sh，面板可能短暂断开"
echo "Running deploy/install.sh"
ZXY_UPDATE_MANIFEST_URL="$MANIFEST_URL" ZXY_INSTALL_MODE=fast bash deploy/install.sh || fail 33 installing "安装脚本执行失败"

write_status restarting restarting "安装脚本完成，正在等待 API 恢复并校验版本"
systemctl restart zxy-panel-api nginx zxy-agent >/dev/null 2>&1 || true

for i in $(seq 1 90); do
  health=$(curl -fsS --max-time 2 "$API_URL" 2>/dev/null || true)
  if echo "$health" | grep -q "$TARGET_VERSION"; then
    echo "API health target version verified: $TARGET_VERSION"
    write_status success success "升级完成，当前 API 版本已切换到 $TARGET_VERSION"
    echo "Upgrade finished: $(date)"
    exit 0
  fi
  if [ $((i %% 10)) -eq 0 ]; then
    echo "Waiting API target version... round=$i health=$health"
  fi
  sleep 2
done

health=$(curl -fsS --max-time 3 "$API_URL" 2>/dev/null || true)
echo "Last API health: $health"
fail 34 verify "安装脚本已结束，但 API health 未返回目标版本 $TARGET_VERSION"
`, task.ID, statusPath, task.LogFile, task.WorkDir, backupDir, task.Package, task.DownloadURL, task.SHA256, manifestURL, installDir, panelVersion, task.TargetVersion)
	if err := os.WriteFile(scriptPath, []byte(content), 0700); err != nil {
		return "", err
	}
	return scriptPath, nil
}

func upgradePrechecks() []map[string]any {
	checks := []map[string]any{}
	add := func(name string, ok bool, message string) {
		checks = append(checks, map[string]any{"name": name, "ok": ok, "message": message})
	}
	add("运行模式", managedUpgradeSupported(), "fast/systemd 模式支持托管升级；Docker 模式请使用复制命令兜底")
	add("远程版本清单", updateManifestURL() != "", firstNonEmpty(updateManifestURL(), "未配置 ZXY_UPDATE_MANIFEST_URL"))
	add("curl", commandExists("curl"), "下载升级包需要 curl")
	add("unzip", commandExists("unzip"), "解压升级包需要 unzip")
	add("sha256sum", commandExists("sha256sum"), "校验升级包需要 sha256sum")
	add("systemctl", commandExists("systemctl"), "fast/systemd 托管升级需要 systemctl")
	add("systemd-run", commandExists("systemd-run"), "托管升级将使用 systemd-run 独立运行，避免 API 重启中断任务")
	freeOK, freeMsg := diskFreeCheck("/root", 1024*1024)
	add("磁盘空间", freeOK, freeMsg)
	add("任务锁", !runningUpgradeTask(), "同一时间只允许一个升级任务；卡死任务可清理后重试")
	return checks
}

func commandExists(cmd string) bool { _, err := exec.LookPath(cmd); return err == nil }

func diskFreeCheck(path string, minKB uint64) (bool, string) {
	out, err := exec.Command("df", "-Pk", path).CombinedOutput()
	if err != nil {
		return false, err.Error()
	}
	fields := strings.Fields(string(out))
	if len(fields) < 11 {
		return false, strings.TrimSpace(string(out))
	}
	avail := fields[len(fields)-3]
	var kb uint64
	_, _ = fmt.Sscanf(avail, "%d", &kb)
	if kb < minKB {
		return false, fmt.Sprintf("可用空间 %d MB，小于要求 %d MB", kb/1024, minKB/1024)
	}
	return true, fmt.Sprintf("可用空间约 %d MB", kb/1024)
}

func precheckMessage(ok bool) string {
	if ok {
		return "升级前检查通过，可以创建托管升级任务。"
	}
	return "升级前检查存在风险，请处理失败项后再执行托管升级。"
}

func tailFile(path string, limit int64) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if st.Size() > limit {
		start = st.Size() - limit
	}
	_, _ = f.Seek(start, io.SeekStart)
	raw, _ := io.ReadAll(f)
	if start > 0 {
		return "...\n" + string(raw)
	}
	return string(raw)
}
