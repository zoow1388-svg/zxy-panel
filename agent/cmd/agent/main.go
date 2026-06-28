// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "0.7.5.1-update-config-agent-xray"

type Heartbeat struct {
	ServerID      string  `json:"server_id"`
	Hostname      string  `json:"hostname"`
	AgentVersion  string  `json:"agent_version"`
	XrayVersion   string  `json:"xray_version"`
	ConfigHash    string  `json:"config_hash"`
	LastMessage   string  `json:"last_message"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	UploadTotal   int64   `json:"upload_total"`
	DownloadTotal int64   `json:"download_total"`
}

type SyncRequest struct {
	ServerID     string `json:"server_id"`
	ConfigHash   string `json:"config_hash"`
	AgentVersion string `json:"agent_version"`
}

type SyncResponse struct {
	OK                  bool           `json:"ok"`
	ServerID            string         `json:"server_id"`
	DesiredConfigHash   string         `json:"desired_config_hash"`
	RestartRequired     bool           `json:"restart_required"`
	XrayConfig          map[string]any `json:"xray_config"`
	NextIntervalSeconds int            `json:"next_interval_seconds"`
	Message             string         `json:"message"`
}

type Config struct {
	PanelBase     string
	ServerID      string
	AgentToken    string
	ConfigPath    string
	TestCommand   string
	ReloadCommand string
	Interval      time.Duration
	ApplyConfig   bool
}

func main() {
	cfg := Config{
		PanelBase:     strings.TrimRight(getenv("ZXY_PANEL_BASE", "http://127.0.0.1:8088"), "/"),
		ServerID:      getenv("ZXY_SERVER_ID", ""),
		AgentToken:    getenv("ZXY_AGENT_TOKEN", ""),
		ConfigPath:    getenv("ZXY_XRAY_CONFIG", "/etc/zxy-panel/xray/config.json"),
		TestCommand:   getenv("ZXY_XRAY_TEST_CMD", "xray run -test -config {config}"),
		ReloadCommand: getenv("ZXY_XRAY_RELOAD_CMD", "systemctl restart xray"),
		Interval:      time.Duration(getenvInt("ZXY_AGENT_INTERVAL_SECONDS", 30)) * time.Second,
		ApplyConfig:   getenvBool("ZXY_APPLY_CONFIG", true),
	}
	if cfg.ServerID == "" || cfg.AgentToken == "" {
		log.Fatal("ZXY_SERVER_ID and ZXY_AGENT_TOKEN are required")
	}
	hostname, _ := os.Hostname()
	lastMessage := "agent started"
	log.Printf("ZXY Agent %s started, panel=%s, server_id=%s, config=%s", version, cfg.PanelBase, cfg.ServerID, cfg.ConfigPath)
	for {
		hash := fileHash(cfg.ConfigPath)
		syncResp, err := syncConfig(cfg, hash)
		if err != nil {
			lastMessage = "sync failed: " + err.Error()
			log.Println(lastMessage)
		} else if syncResp.RestartRequired {
			if cfg.ApplyConfig {
				if err := applyXrayConfig(cfg, syncResp); err != nil {
					lastMessage = "apply failed: " + err.Error()
					log.Println(lastMessage)
				} else {
					lastMessage = "config applied: " + syncResp.DesiredConfigHash
					log.Println(lastMessage)
					hash = fileHash(cfg.ConfigPath)
				}
			} else {
				lastMessage = "config changed but apply disabled"
				log.Println(lastMessage)
			}
		} else {
			lastMessage = "config already up to date"
		}
		hb := collectHeartbeat(cfg, hostname, hash, lastMessage)
		if err := heartbeat(cfg, hb); err != nil {
			log.Printf("heartbeat failed: %v", err)
		}
		interval := cfg.Interval
		if syncResp.NextIntervalSeconds > 0 {
			interval = time.Duration(syncResp.NextIntervalSeconds) * time.Second
		}
		time.Sleep(interval)
	}
}

func syncConfig(cfg Config, currentHash string) (SyncResponse, error) {
	payload, _ := json.Marshal(SyncRequest{ServerID: cfg.ServerID, ConfigHash: currentHash, AgentVersion: version})
	var out SyncResponse
	if err := postJSON(cfg.PanelBase+"/api/agent/sync", cfg.AgentToken, payload, &out); err != nil {
		return out, err
	}
	return out, nil
}

func heartbeat(cfg Config, hb Heartbeat) error {
	payload, _ := json.Marshal(hb)
	return postJSON(cfg.PanelBase+"/api/agent/heartbeat", cfg.AgentToken, payload, nil)
}

func postJSON(url, token string, payload []byte, out any) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", token)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return err
		}
	}
	return nil
}

func applyXrayConfig(cfg Config, syncResp SyncResponse) error {
	if syncResp.XrayConfig == nil {
		return fmt.Errorf("empty xray config")
	}
	raw, err := json.MarshalIndent(syncResp.XrayConfig, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0755); err != nil {
		return err
	}
	tmp := cfg.ConfigPath + ".tmp.json"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if cfg.TestCommand != "" {
		cmd := strings.ReplaceAll(cfg.TestCommand, "{config}", tmp)
		if out, err := runShell(cmd); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("xray test failed: %v; output=%s", err, strings.TrimSpace(out))
		}
	}
	if err := os.Rename(tmp, cfg.ConfigPath); err != nil {
		return err
	}
	// 让 xray 服务无论以 root/nobody 运行都能读取配置；生产版后续会改成更细粒度权限。
	_ = os.Chmod(cfg.ConfigPath, 0644)
	if cfg.ReloadCommand != "" {
		if out, err := runShell(strings.ReplaceAll(cfg.ReloadCommand, "{config}", cfg.ConfigPath)); err != nil {
			return fmt.Errorf("xray reload failed: %v; output=%s", err, strings.TrimSpace(out))
		}
	}
	return nil
}

func collectHeartbeat(cfg Config, hostname, hash, msg string) Heartbeat {
	up, down := networkTotals()
	return Heartbeat{
		ServerID: cfg.ServerID, Hostname: hostname, AgentVersion: version, XrayVersion: xrayVersion(), ConfigHash: hash, LastMessage: msg,
		CPUUsage: loadAverage(), MemoryUsage: memoryUsage(), DiskUsage: diskUsage("/"), UploadTotal: up, DownloadTotal: down,
	}
}

func runShell(command string) (string, error) {
	cmd := exec.Command("/bin/sh", "-lc", command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func xrayVersion() string {
	out, err := exec.Command("/bin/sh", "-lc", "command -v xray >/dev/null 2>&1 && xray version | head -1 || true").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func fileHash(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func loadAverage() float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func memoryUsage() float64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	vals := map[string]float64{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			key := strings.TrimSuffix(fields[0], ":")
			vals[key], _ = strconv.ParseFloat(fields[1], 64)
		}
	}
	total := vals["MemTotal"]
	avail := vals["MemAvailable"]
	if total <= 0 {
		return 0
	}
	return round2((total - avail) * 100 / total)
}

func diskUsage(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	if total <= 0 {
		return 0
	}
	return round2((total - free) * 100 / total)
}

func networkTotals() (int64, int64) {
	raw, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	var rx, tx int64
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		r, _ := strconv.ParseInt(fields[0], 10, 64)
		t, _ := strconv.ParseInt(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return tx, rx
}

func round2(v float64) float64 { return float64(int(v*100)) / 100 }
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
func getenvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	return fallback
}
