// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type updateManifest struct {
	Latest              string   `json:"latest"`
	Version             string   `json:"version"`
	Package             string   `json:"package"`
	DownloadURL         string   `json:"download_url"`
	SHA256              string   `json:"sha256"`
	Changelog           []string `json:"changelog"`
	MinSupportedVersion string   `json:"min_supported_version"`
}

func updateManifestURL() string {
	return strings.TrimSpace(getenv("ZXY_UPDATE_MANIFEST_URL", ""))
}

func (r *Router) updateStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	manifestURL := updateManifestURL()
	configured := manifestURL != ""
	note := "未配置远程版本清单。请先把代码仓库和 version.json 发布好，再通过环境变量 ZXY_UPDATE_MANIFEST_URL 配置正式地址。"
	if configured {
		note = "已配置远程版本清单，可检查更新并生成升级命令。"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":     panelVersion,
		"manifest_url":        manifestURL,
		"manifest_configured": configured,
		"manifest_display":    firstNonEmpty(manifestURL, "未配置"),
		"xray_version":        r.currentXrayVersionText(),
		"install_dir":         installDir,
		"backup_dir":          filepath.Join(installDir, "backups"),
		"note":                note,
	})
}

func (r *Router) updateCheck(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost && req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	manifestURL := updateManifestURL()
	if manifestURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              false,
			"current_version": panelVersion,
			"manifest_url":    "",
			"message":         "暂未配置远程版本清单。请先上传代码仓库并发布 version.json，再设置 ZXY_UPDATE_MANIFEST_URL。",
		})
		return
	}
	manifest, err := fetchUpdateManifest(req.Context(), manifestURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              false,
			"current_version": panelVersion,
			"manifest_url":    manifestURL,
			"error":           err.Error(),
			"message":         "暂时无法获取远程版本清单：" + err.Error(),
		})
		return
	}
	latest := manifest.Latest
	if latest == "" {
		latest = manifest.Version
	}
	available := isRemoteVersionNewer(latest, panelVersion)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"current_version":  panelVersion,
		"latest_version":   latest,
		"update_available": available,
		"manifest":         manifest,
		"message":          updateMessage(latest),
	})
}

func (r *Router) updatePanelCommand(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost && req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	manifestURL := updateManifestURL()
	if manifestURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "无法生成升级命令：远程版本清单未配置。请先发布 version.json 并设置 ZXY_UPDATE_MANIFEST_URL。"})
		return
	}
	manifest, err := fetchUpdateManifest(req.Context(), manifestURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "message": "无法生成升级命令：远程版本清单不可用：" + err.Error()})
		return
	}
	target := firstNonEmpty(manifest.Latest, manifest.Version)
	if !isRemoteVersionNewer(target, panelVersion) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "current_version": panelVersion, "target_version": target, "message": "当前已是最新版本，远程版本不高于当前版本，已禁止生成降级命令。"})
		return
	}
	pkg := strings.TrimSpace(manifest.Package)
	if pkg == "" && manifest.DownloadURL != "" {
		parts := strings.Split(manifest.DownloadURL, "/")
		pkg = parts[len(parts)-1]
	}
	if pkg == "" || manifest.DownloadURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "version.json 缺少 package 或 download_url。"})
		return
	}
	dir := strings.TrimSuffix(pkg, ".zip")
	shaLine := ""
	if strings.TrimSpace(manifest.SHA256) != "" {
		shaLine = fmt.Sprintf("echo '%s  %s' | sha256sum -c - && \\\n", shellQuoteSafe(manifest.SHA256), shellQuoteSafe(pkg))
	}
	command := fmt.Sprintf("cd /root && \\\nmkdir -p /root/zxy-panel-upgrades && \\\ncd /root/zxy-panel-upgrades && \\\ncurl -L --fail -o %s %s && \\\n%sunzip -o %s && \\\ncd %s && \\\nbash deploy/install.sh 2>&1 | tee /root/zxy-panel-upgrade.log", shellEscape(pkg), shellEscape(manifest.DownloadURL), shaLine, shellEscape(pkg), shellEscape(dir))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"current_version": panelVersion,
		"target_version":  firstNonEmpty(manifest.Latest, manifest.Version),
		"package":         pkg,
		"command":         command,
		"message":         "为安全起见，当前版本先生成可审查升级命令。确认无误后可复制到服务器执行。后续版本会加入后台直接执行与回滚。",
	})
}

func (r *Router) updateXrayStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current_xray":   r.currentXrayVersionText(),
		"message":        "网络核心升级入口已预留。下一步会加入检查最新版、备份旧核心、测试配置、替换并重启。",
		"planned_checks": []string{"备份当前 xray", "下载新版网络核心", "执行配置测试", "测试通过后重启", "失败保留旧核心"},
	})
}

func fetchUpdateManifest(ctx context.Context, url string) (updateManifest, error) {
	var manifest updateManifest
	if url == "" {
		return manifest, fmt.Errorf("manifest url is empty")
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return manifest, err
	}
	req.Header.Set("User-Agent", "ZXY-Panel/"+panelVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return manifest, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return manifest, fmt.Errorf("version manifest http status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024))
	if err := dec.Decode(&manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (r *Router) currentXrayVersionText() string {
	for _, srv := range r.store.Data.Servers {
		if strings.TrimSpace(srv.XrayVersion) != "" {
			return strings.TrimSpace(srv.XrayVersion)
		}
	}
	return currentXrayVersionText()
}

func currentXrayVersionText() string {
	candidates := []string{"xray", "/usr/local/bin/xray", "/usr/bin/xray"}
	for _, bin := range candidates {
		if strings.Contains(bin, "/") {
			if _, err := os.Stat(bin); err != nil {
				continue
			}
		}
		out, err := exec.Command(bin, "version").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			return strings.TrimSpace(lines[0])
		}
	}
	return "未检测到 Xray 或当前容器不可访问宿主机 Xray"
}

func updateMessage(latest string) string {
	if latest == "" {
		return "远程版本清单未提供 latest 字段。"
	}
	if isRemoteVersionNewer(latest, panelVersion) {
		return "发现新版本，可查看更新日志并生成升级命令。"
	}
	return "当前已是最新版本。"
}

func isRemoteVersionNewer(remote, current string) bool {
	return comparePanelVersions(remote, current) > 0
}

func comparePanelVersions(a, b string) int {
	ap := numericVersionParts(a)
	bp := numericVersionParts(b)
	max := len(ap)
	if len(bp) > max {
		max = len(bp)
	}
	for i := 0; i < max; i++ {
		ai, bi := 0, 0
		if i < len(ap) {
			ai = ap[i]
		}
		if i < len(bp) {
			bi = bp[i]
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

func numericVersionParts(s string) []int {
	s = strings.TrimSpace(s)
	start := -1
	for i, r := range s {
		if r >= '0' && r <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := start
	for end < len(s) {
		c := s[end]
		if (c < '0' || c > '9') && c != '.' {
			break
		}
		end++
	}
	parts := strings.Split(strings.Trim(s[start:end], "."), ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			out = append(out, 0)
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

func shellEscape(s string) string    { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func shellQuoteSafe(s string) string { return strings.ReplaceAll(s, "'", "") }
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
