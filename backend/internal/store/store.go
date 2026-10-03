// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
		return errors.New("existing panel data file is empty; restore a backup before starting")
	}
	var data *model.PanelData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data == nil {
		return errors.New("existing panel data must be a JSON object; restore a backup before starting")
	}
	s.Data = *data
	normalize(&s.Data)
	return nil
}

func newData() model.PanelData {
	return model.PanelData{
		Version:       "0.7.7.6-bbr-optimization-agent-xray",
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
	d.Version = "0.7.7.6-bbr-optimization-agent-xray"
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
	return s.commitLocked(clonePanelData(s.Data))
}

// Caller holds Mu.Lock through validation and this commit.
func (s *Store) SaveServerLocked(id string, server model.Server, actor, action, ip, detail string) error {
	if id == "" || server.ID != id {
		return errors.New("server identity mismatch")
	}
	if current, exists := s.Data.Servers[id]; exists && current.ID != id {
		return errors.New("stored server identity mismatch")
	}
	next := clonePanelData(s.Data)
	if next.Servers == nil {
		next.Servers = make(map[string]model.Server)
	}
	next.Servers[id] = cloneServer(server)
	if action != "" {
		addLog(&next, actor, action, ip, detail)
	}
	return s.commitLocked(next)
}

func (s *Store) commitLocked(next model.PanelData) error {
	normalize(&next)
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := writePanelData(s.Path, raw, osPanelFiles{}); err != nil {
		return err
	}
	s.Data = next
	return nil
}

func cloneServer(server model.Server) model.Server {
	server.BBRStatus.AvailableCongestionControl = slices.Clone(server.BBRStatus.AvailableCongestionControl)
	if server.BBRPendingAction != nil {
		action := *server.BBRPendingAction
		server.BBRPendingAction = &action
	}
	return server
}

func clonePanelData(data model.PanelData) model.PanelData {
	data.Admins = maps.Clone(data.Admins)
	data.Servers = maps.Clone(data.Servers)
	for id, server := range data.Servers {
		data.Servers[id] = cloneServer(server)
	}
	data.Nodes = maps.Clone(data.Nodes)
	data.Clients = maps.Clone(data.Clients)
	for id, client := range data.Clients {
		client.NodeIDs = slices.Clone(client.NodeIDs)
		client.RelayRouteIDs = slices.Clone(client.RelayRouteIDs)
		data.Clients[id] = client
	}
	data.RelayRoutes = maps.Clone(data.RelayRoutes)
	data.LandingExits = maps.Clone(data.LandingExits)
	data.OperationLogs = maps.Clone(data.OperationLogs)
	data.NetworkPolicy.DNSServers = slices.Clone(data.NetworkPolicy.DNSServers)
	data.NetworkPolicyBackup.DNSServers = slices.Clone(data.NetworkPolicyBackup.DNSServers)
	return data
}

type stagedPanelFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

type panelFiles interface {
	CreateTemp(string, string) (stagedPanelFile, error)
	Rename(string, string) error
}

type osPanelFiles struct{}

func (osPanelFiles) CreateTemp(dir, pattern string) (stagedPanelFile, error) {
	return os.CreateTemp(dir, pattern)
}

func (osPanelFiles) Rename(from, to string) error { return os.Rename(from, to) }

func closePanelFile(file stagedPanelFile, cause error) error {
	if err := file.Close(); err != nil {
		return errors.Join(cause, fmt.Errorf("close temporary panel data: %w", err))
	}
	return cause
}

func writePanelData(path string, raw []byte, files panelFiles) error {
	file, err := files.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary panel data: %w", err)
	}
	// Failed temporary files are retained with restricted permissions as evidence.
	written, err := file.Write(raw)
	if err == nil && written != len(raw) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return closePanelFile(file, fmt.Errorf("write temporary panel data: %w", err))
	}
	if err := file.Sync(); err != nil {
		return closePanelFile(file, fmt.Errorf("sync temporary panel data: %w", err))
	}
	if err := closePanelFile(file, nil); err != nil {
		return err
	}
	if err := files.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace panel data: %w", err)
	}
	return nil
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
	addLog(&s.Data, actor, action, ip, detail)
}

func addLog(data *model.PanelData, actor, action, ip, detail string) {
	if data.OperationLogs == nil {
		data.OperationLogs = make(map[string]model.OperationLog)
	}
	id := NewID("log")
	data.OperationLogs[id] = model.OperationLog{ID: id, Actor: actor, Action: action, IP: ip, Detail: detail, CreatedAt: time.Now()}
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
