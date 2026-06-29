// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/security"
)

type Store struct {
	Mu   sync.RWMutex
	Path string
	Data model.PanelData
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "./data/zxy-panel.json"
	}
	s := &Store{Path: path}
	if err := s.loadOrInit(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadOrInit() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(s.Path); errors.Is(err, os.ErrNotExist) {
		s.Data = newData()
		if err := s.seedDefaultAdmin(); err != nil {
			return err
		}
		if err := s.seedLocalServer(); err != nil {
			return err
		}
		return s.SaveLocked()
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		s.Data = newData()
		if err := s.seedDefaultAdmin(); err != nil {
			return err
		}
		if err := s.seedLocalServer(); err != nil {
			return err
		}
		return s.SaveLocked()
	}
	if err := json.Unmarshal(raw, &s.Data); err != nil {
		return err
	}
	normalize(&s.Data)
	changed, err := s.ensureSingleModeLocalServer()
	if err != nil {
		return err
	}
	if changed {
		return s.SaveLocked()
	}
	return nil
}

func newData() model.PanelData {
	return model.PanelData{
		Version:       "0.7.5.9-qr-import-compatibility-agent-xray",
		Admins:        map[string]model.AdminUser{},
		Servers:       map[string]model.Server{},
		Nodes:         map[string]model.Node{},
		Clients:       map[string]model.Client{},
		RelayRoutes:   map[string]model.RelayRoute{},
		LandingExits:  map[string]model.LandingExit{},
		OperationLogs: map[string]model.OperationLog{},
	}
}

func normalize(d *model.PanelData) {
	if d.Admins == nil {
		d.Admins = map[string]model.AdminUser{}
	}
	if d.Servers == nil {
		d.Servers = map[string]model.Server{}
	}
	if d.Nodes == nil {
		d.Nodes = map[string]model.Node{}
	}
	if d.Clients == nil {
		d.Clients = map[string]model.Client{}
	}
	if d.RelayRoutes == nil {
		d.RelayRoutes = map[string]model.RelayRoute{}
	}
	if d.LandingExits == nil {
		d.LandingExits = map[string]model.LandingExit{}
	}
	if d.OperationLogs == nil {
		d.OperationLogs = map[string]model.OperationLog{}
	}
	normalizeNetworkPolicy(&d.NetworkPolicy)
	normalizeNetworkPolicy(&d.NetworkPolicyBackup)
	// V0.4.1 修复：旧版本 AdminUser.PasswordHash 被 json:"-" 忽略，重启后会丢失密码哈希，导致 admin/admin123 无法登录。
	// 如果发现管理员哈希为空，自动重置为默认密码 admin123，方便测试版升级恢复登录。
	for id, a := range d.Admins {
		if a.PasswordHash == "" {
			hash, err := security.HashPassword("admin123")
			if err == nil {
				a.PasswordHash = hash
				a.Enabled = true
				d.Admins[id] = a
			}
		}
	}
	d.Version = "0.7.5.9-qr-import-compatibility-agent-xray"
}

func defaultNetworkPolicy() model.NetworkPolicy {
	return model.NetworkPolicy{
		Mode:                   "compat",
		PublicDNS:              false,
		DNSServers:             []string{},
		QueryStrategy:          "AsIs",
		DisableFallback:        false,
		DisableFallbackIfMatch: false,
		BlockDNS53:             false,
		BlockChinaDNS:          false,
		BlockQUIC:              false,
		IPv6Strategy:           "keep",
		ClashIncludeQuad9:      false,
		SingBoxIncludeQuad9:    false,
	}
}

func normalizeNetworkPolicy(p *model.NetworkPolicy) {
	if p.Mode == "" {
		*p = defaultNetworkPolicy()
		return
	}
	allowedModes := map[string]bool{"compat": true, "public_dns": true, "dns_leak_guard": true, "strict": true, "custom": true}
	if !allowedModes[p.Mode] {
		p.Mode = "compat"
	}
	if p.QueryStrategy == "" {
		p.QueryStrategy = "AsIs"
	}
	allowedQuery := map[string]bool{"AsIs": true, "UseIPv4": true, "UseIPv6": true, "UseIP": true}
	if !allowedQuery[p.QueryStrategy] {
		p.QueryStrategy = "AsIs"
	}
	if p.IPv6Strategy == "" {
		p.IPv6Strategy = "keep"
	}
	allowedIPv6 := map[string]bool{"keep": true, "warn": true, "disable_hint": true}
	if !allowedIPv6[p.IPv6Strategy] {
		p.IPv6Strategy = "keep"
	}
	if p.PublicDNS && len(p.DNSServers) == 0 {
		p.DNSServers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	}
}

func (s *Store) SaveLocked() error {
	normalize(&s.Data)
	raw, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *Store) seedDefaultAdmin() error {
	now := time.Now()
	username := getenv("ZXY_ADMIN_USERNAME", "admin")
	password := getenv("ZXY_ADMIN_PASSWORD", "admin123")
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	id := NewID("admin")
	s.Data.Admins[id] = model.AdminUser{
		ID: id, Username: username, PasswordHash: hash, Role: "super_admin", Enabled: true, CreatedAt: now,
	}
	return nil
}

func (s *Store) seedLocalServer() error {
	now := time.Now()
	serverID := NewID("srv")
	ip := getenv("ZXY_LOCAL_SERVER_IP", "127.0.0.1")
	host := getenv("ZXY_LOCAL_SERVER_HOST", ip)
	name := getenv("ZXY_LOCAL_SERVER_NAME", "本机服务器")
	region := getenv("ZXY_LOCAL_SERVER_REGION", "Local")
	provider := getenv("ZXY_LOCAL_SERVER_PROVIDER", "Self-hosted")
	s.Data.Servers[serverID] = model.Server{
		ID: serverID, Name: name, IP: ip, Host: host, Region: region, Provider: provider,
		Status: "offline", AgentToken: NewToken(), CreatedAt: now, UpdatedAt: now,
	}
	return nil
}

func (s *Store) ensureSingleModeLocalServer() (bool, error) {
	if len(s.Data.Servers) == 0 {
		return true, s.seedLocalServer()
	}
	localIP := getenv("ZXY_LOCAL_SERVER_IP", "127.0.0.1")
	localHost := getenv("ZXY_LOCAL_SERVER_HOST", localIP)
	localName := getenv("ZXY_LOCAL_SERVER_NAME", "本机服务器")
	localRegion := getenv("ZXY_LOCAL_SERVER_REGION", "Local")
	localProvider := getenv("ZXY_LOCAL_SERVER_PROVIDER", "Self-hosted")

	// V0.5.8：从 V0.4.x / V0.5.0 升级到单机模式时，旧数据里可能已经存在同 IP 服务器。
	// 这里自动选择一台作为“本机服务器”，把同 IP/Host 的重复服务器合并，避免后台出现多个本机、Agent 版本报警、入站绑定旧 server_id。
	candidates := []model.Server{}
	for _, srv := range s.Data.Servers {
		if sameServerEndpoint(srv, localIP, localHost) || len(s.Data.Servers) == 1 {
			candidates = append(candidates, srv)
		}
	}
	if len(candidates) == 0 {
		return true, s.seedLocalServer()
	}
	keep := pickLocalServer(candidates)
	changed := false
	if keep.Name == "" || keep.Name == keep.IP || keep.Name == keep.Host {
		keep.Name = localName
		changed = true
	}
	// V0.5.8：如果升级后本机服务器还显示 127.0.0.1/localhost，而安装脚本已经识别到公网 IP，
	// 自动把展示 IP/Host 修正为公网入口，避免后台看起来像只能本机访问。
	if keep.IP == "" || shouldReplaceLocalEndpoint(keep.IP, localIP) {
		keep.IP = localIP
		changed = true
	}
	if keep.Host == "" || shouldReplaceLocalEndpoint(keep.Host, localHost) {
		keep.Host = localHost
		changed = true
	}
	if keep.Region == "" {
		keep.Region = localRegion
		changed = true
	}
	if keep.Provider == "" {
		keep.Provider = localProvider
		changed = true
	}
	if keep.AgentToken == "" {
		keep.AgentToken = NewToken()
		changed = true
	}
	if keep.UpdatedAt.IsZero() {
		keep.UpdatedAt = time.Now()
		changed = true
	}
	s.Data.Servers[keep.ID] = keep

	for _, srv := range candidates {
		if srv.ID == keep.ID {
			continue
		}
		for id, n := range s.Data.Nodes {
			if n.ServerID == srv.ID {
				n.ServerID = keep.ID
				n.UpdatedAt = time.Now()
				s.Data.Nodes[id] = n
			}
		}
		delete(s.Data.Servers, srv.ID)
		changed = true
	}
	return changed, nil
}

func sameServerEndpoint(srv model.Server, localIP, localHost string) bool {
	return (localIP != "" && (srv.IP == localIP || srv.Host == localIP)) || (localHost != "" && (srv.IP == localHost || srv.Host == localHost)) ||
		(isLoopbackEndpoint(srv.IP) && !isLoopbackEndpoint(localIP)) || (isLoopbackEndpoint(srv.Host) && !isLoopbackEndpoint(localHost))
}

func isLoopbackEndpoint(v string) bool {
	switch v {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

func shouldReplaceLocalEndpoint(current, target string) bool {
	return target != "" && !isLoopbackEndpoint(target) && isLoopbackEndpoint(current)
}

func pickLocalServer(list []model.Server) model.Server {
	keep := list[0]
	for _, srv := range list[1:] {
		if srv.AgentVersion == "0.7.5.9-qr-import-compatibility-agent-xray" && keep.AgentVersion != "0.7.5.9-qr-import-compatibility-agent-xray" {
			keep = srv
			continue
		}
		if srv.Status == "online" && keep.Status != "online" {
			keep = srv
			continue
		}
		if srv.LastSyncAt.After(keep.LastSyncAt) {
			keep = srv
			continue
		}
		if keep.LastSyncAt.IsZero() && srv.CreatedAt.After(keep.CreatedAt) {
			keep = srv
		}
	}
	return keep
}

// EnsureSingleModeLocalServerLocked makes sure the single-node local server exists.
// Caller must hold s.Mu.Lock().
func (s *Store) EnsureSingleModeLocalServerLocked() error {
	changed, err := s.ensureSingleModeLocalServer()
	if err != nil {
		return err
	}
	if changed {
		return s.SaveLocked()
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (s *Store) FindAdminByUsername(username string) (model.AdminUser, bool) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	for _, a := range s.Data.Admins {
		if a.Username == username {
			return a, true
		}
	}
	return model.AdminUser{}, false
}

func (s *Store) UpdateAdminLogin(id, ip string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	a, ok := s.Data.Admins[id]
	if !ok {
		return
	}
	a.LastLoginIP = ip
	a.LastLoginAt = time.Now()
	s.Data.Admins[id] = a
	_ = s.SaveLocked()
}

func (s *Store) AddLog(actor, action, ip, detail string) {
	id := NewID("log")
	s.Data.OperationLogs[id] = model.OperationLog{ID: id, Actor: actor, Action: action, IP: ip, Detail: detail, CreatedAt: time.Now()}
}

func NewID(prefix string) string {
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), randHex(4))
}
func NewToken() string { return randHex(24) }
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s *Store) ChangeAdminPassword(adminID, oldPassword, newPassword string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	a, ok := s.Data.Admins[adminID]
	if !ok || !a.Enabled {
		return errors.New("admin not found")
	}
	if !security.VerifyPassword(oldPassword, a.PasswordHash) {
		return errors.New("old password incorrect")
	}
	hash, err := security.HashPassword(newPassword)
	if err != nil {
		return err
	}
	a.PasswordHash = hash
	s.Data.Admins[adminID] = a
	return s.SaveLocked()
}
