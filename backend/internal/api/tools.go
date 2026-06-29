// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type socks5TestRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type socks5TestResponse struct {
	OK        bool   `json:"ok"`
	ExitIP    string `json:"exit_ip"`
	Target    string `json:"target"`
	LatencyMS int64  `json:"latency_ms"`
	Message   string `json:"message"`
}

func (r *Router) testSocks5(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body socks5TestRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	body.Host = strings.TrimSpace(body.Host)
	body.Username = strings.TrimSpace(body.Username)
	if body.Host == "" || body.Port < 1 || body.Port > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请填写正确的 SOCKS5 地址和端口"})
		return
	}
	started := time.Now()
	target := "api.ipify.org:80"
	ip, err := socks5HTTPGet(body.Host, body.Port, body.Username, body.Password, "api.ipify.org", 80, "api.ipify.org", "/")
	if err != nil || net.ParseIP(strings.TrimSpace(ip)) == nil {
		target = "ifconfig.me:80"
		ip, err = socks5HTTPGet(body.Host, body.Port, body.Username, body.Password, "ifconfig.me", 80, "ifconfig.me", "/ip")
	}
	cost := time.Since(started).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, socks5TestResponse{OK: false, Target: target, LatencyMS: cost, Message: err.Error()})
		return
	}
	ip = strings.TrimSpace(ip)
	if parsed := net.ParseIP(ip); parsed == nil {
		writeJSON(w, http.StatusOK, socks5TestResponse{OK: false, Target: target, LatencyMS: cost, Message: "SOCKS5 已连接，但出口 IP 返回内容异常：" + trimForMessage(ip, 120)})
		return
	}
	writeJSON(w, http.StatusOK, socks5TestResponse{OK: true, ExitIP: ip, Target: target, LatencyMS: cost, Message: "SOCKS5 连通，出口 IP 检测成功"})
}

func socks5HTTPGet(socksHost string, socksPort int, username, password, targetHost string, targetPort int, httpHost, path string) (string, error) {
	dialer := net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(socksHost, strconv.Itoa(socksPort)))
	if err != nil {
		return "", fmt.Errorf("连接 SOCKS5 失败：%w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	if err := socks5Handshake(conn, username, password, targetHost, targetPort); err != nil {
		return "", err
	}
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: ZXY-Panel/0.7.5.5\r\nAccept: text/plain\r\nConnection: close\r\n\r\n", path, httpHost)
	if _, err := conn.Write([]byte(req)); err != nil {
		return "", fmt.Errorf("通过 SOCKS5 发送出口检测请求失败：%w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(conn, 64*1024))
	if err != nil {
		return "", fmt.Errorf("读取出口检测结果失败：%w", err)
	}
	return parseHTTPBody(raw)
}

func socks5Handshake(conn net.Conn, username, password, targetHost string, targetPort int) error {
	methods := []byte{0x00}
	if username != "" || password != "" {
		methods = []byte{0x00, 0x02}
	}
	hello := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(hello); err != nil {
		return fmt.Errorf("SOCKS5 握手发送失败：%w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("SOCKS5 握手读取失败：%w", err)
	}
	if resp[0] != 0x05 {
		return fmt.Errorf("SOCKS5 服务端响应版本异常")
	}
	switch resp[1] {
	case 0x00:
		// no auth
	case 0x02:
		if username == "" && password == "" {
			return fmt.Errorf("SOCKS5 需要账号密码认证")
		}
		if len(username) > 255 || len(password) > 255 {
			return fmt.Errorf("SOCKS5 账号或密码过长")
		}
		auth := []byte{0x01, byte(len(username))}
		auth = append(auth, []byte(username)...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, []byte(password)...)
		if _, err := conn.Write(auth); err != nil {
			return fmt.Errorf("SOCKS5 认证发送失败：%w", err)
		}
		ar := make([]byte, 2)
		if _, err := io.ReadFull(conn, ar); err != nil {
			return fmt.Errorf("SOCKS5 认证读取失败：%w", err)
		}
		if ar[1] != 0x00 {
			return fmt.Errorf("SOCKS5 账号或密码认证失败")
		}
	case 0xff:
		return fmt.Errorf("SOCKS5 不接受当前认证方式")
	default:
		return fmt.Errorf("SOCKS5 返回了不支持的认证方式：0x%02x", resp[1])
	}

	if len(targetHost) > 255 {
		return fmt.Errorf("目标域名过长")
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(targetHost))}
	req = append(req, []byte(targetHost)...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(targetPort))
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT 发送失败：%w", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT 读取失败：%w", err)
	}
	if head[0] != 0x05 {
		return fmt.Errorf("SOCKS5 CONNECT 响应版本异常")
	}
	if head[1] != 0x00 {
		return fmt.Errorf("SOCKS5 CONNECT 失败：%s", socks5ReplyMessage(head[1]))
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x03:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return fmt.Errorf("SOCKS5 CONNECT 地址读取失败：%w", err)
		}
		skip = int(lenBuf[0])
	case 0x04:
		skip = 16
	default:
		return fmt.Errorf("SOCKS5 CONNECT 地址类型异常")
	}
	if skip > 0 {
		if _, err := io.CopyN(io.Discard, conn, int64(skip)); err != nil {
			return fmt.Errorf("SOCKS5 CONNECT 地址跳过失败：%w", err)
		}
	}
	if _, err := io.CopyN(io.Discard, conn, 2); err != nil {
		return fmt.Errorf("SOCKS5 CONNECT 端口读取失败：%w", err)
	}
	return nil
}

func socks5ReplyMessage(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return fmt.Sprintf("unknown reply 0x%02x", code)
	}
}

func parseHTTPBody(raw []byte) (string, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("HTTP 响应读取失败：%w", err)
	}
	parts := strings.Fields(statusLine)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "2") {
		return "", fmt.Errorf("出口检测 HTTP 状态异常：%s", strings.TrimSpace(statusLine))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("HTTP 响应头读取失败：%w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("HTTP 响应内容读取失败：%w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func trimForMessage(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
