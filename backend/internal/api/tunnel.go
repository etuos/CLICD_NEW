package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Host-side WireGuard tunnel management.
// Reads/writes /etc/wireguard/wg0.conf and drives wg-quick@wg0 through systemd.

const (
	wgConfPath        = "/etc/wireguard/wg0.conf"
	wgSystemdUnit     = "wg-quick@wg0.service"
	wgIfName          = "wg0"
	wgDefaultPort     = 51820
	wgDefaultAddress  = "10.250.250.1/24"
	wgDefaultMTU      = 1420
	wgDefaultDNS      = "1.1.1.1"
	tunnelScopeRead   = "tunnel:read"
	tunnelScopeManage = "tunnel:manage"
)

// TunnelPeer mirrors one [Peer] section of the WireGuard config.
type TunnelPeer struct {
	PublicKey       string `json:"public_key"`
	AllowedIPs      string `json:"allowed_ips"`
	Keepalive       string `json:"persistent_keepalive,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	Comment         string `json:"comment,omitempty"`
	LastHandshake   int64  `json:"last_handshake"`
	RXBytes         int64  `json:"rx_bytes"`
	TXBytes         int64  `json:"tx_bytes"`
}

// TunnelStatus describes the current host-side tunnel state.
type TunnelStatus struct {
	Name               string       `json:"name"`
	Interface          string       `json:"interface"`
	Enabled            bool         `json:"enabled"`
	Active             bool         `json:"active"`
	Address            string       `json:"address"`
	ListenPort         int          `json:"listen_port"`
	MTU                int          `json:"mtu"`
	DNS                string       `json:"dns"`
	PrivateKey         string       `json:"private_key"`
	PublicKey          string       `json:"public_key"`
	Peers              []TunnelPeer `json:"peers"`
	ConfigPath         string       `json:"config_path"`
	WireGuardInstalled bool         `json:"wireguard_installed"`
	TunAvailable       bool         `json:"tun_available"`
	WireGuardModule    bool         `json:"wireguard_module"`
	KernelReady        bool         `json:"kernel_ready"`
	NATSubnet          string       `json:"nat_subnet,omitempty"`
}

// tunnelActionRequest is the POST body for the tunnel API.
type tunnelActionRequest struct {
	Action       string `json:"action"` // status|create|enable|disable|reload|sync|add_peer|remove_peer|reset_key
	ListenPort   int    `json:"listen_port"`
	Address      string `json:"address"`
	MTU          int    `json:"mtu"`
	DNS          string `json:"dns"`
	PeerPubKey   string `json:"peer_pub_key"`
	PeerAllowed  string `json:"peer_allowed_ips"`
	PeerName     string `json:"peer_name"`
	PeerEndpoint string `json:"peer_endpoint"`
}

// HandleTunnel serves GET (status) and POST (lifecycle actions).
func HandleTunnel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, tunnelScopeRead) {
			return
		}
		auditRequest(r, "tunnel.read", wgConfPath, "", true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: getTunnelStatus()})

	case http.MethodPost:
		if !requireScope(w, r, tunnelScopeManage) {
			return
		}
		var req tunnelActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		msg, err := runTunnelAction(req)
		if err != nil {
			auditRequest(r, "tunnel."+req.Action, wgConfPath, "", false, err.Error())
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
			return
		}
		auditRequest(r, "tunnel."+req.Action, wgConfPath, "", true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: msg, Data: getTunnelStatus()})

	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func runTunnelAction(req tunnelActionRequest) (string, error) {
	switch req.Action {
	case "status":
		return "隧道状态已获取", nil
	case "load_modules":
		if err := ensureWireguardKernel(); err != nil {
			return "", err
		}
		return "内核模块已就绪（wireguard 或 tun 可用）", nil
	case "create":
		return "WireGuard 隧道配置已生成", createTunnel(req)
	case "enable":
		return "隧道已启用并设置开机自启", enableTunnel()
	case "disable":
		return "隧道已停用", disableTunnel()
	case "reload":
		return "隧道配置已重新加载", systemctlAction(wgSystemdUnit, "restart")
	case "sync":
		return "隧道配置已同步到内核", syncTunnel()
	case "add_peer":
		return "Peer 已添加", addTunnelPeer(req)
	case "remove_peer":
		return "Peer 已移除", removeTunnelPeer(req.PeerPubKey)
	case "reset_key":
		return "密钥已重新生成，请同步更新对端配置", resetTunnelKey()
	case "delete":
		return "隧道已删除", deleteTunnel()
	default:
		return "", fmt.Errorf("Invalid action: %s", req.Action)
	}
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func getTunnelStatus() TunnelStatus {
	wgMod := kernelModuleAvailable("wireguard")
	tunMod := kernelModuleLoaded("tun")
	status := TunnelStatus{
		Name:               wgIfName,
		Interface:          wgIfName,
		ConfigPath:         wgConfPath,
		Peers:              []TunnelPeer{},
		WireGuardInstalled: hasCmd("wg") && hasCmd("wg-quick"),
		TunAvailable:       tunMod || hasFile("/dev/net/tun"),
		WireGuardModule:    wgMod,
		// 内核 WireGuard 不依赖 tun；若仅装了 userspace wg-go 则需 tun。
		KernelReady: wgMod || tunMod || hasFile("/dev/net/tun"),
	}

	if data, err := os.ReadFile(wgConfPath); err == nil {
		status.Enabled = true
		parseWireGuardConf(string(data), &status)
	}

	if hasCmd("systemctl") {
		if out, err := exec.Command("systemctl", "is-active", wgSystemdUnit).CombinedOutput(); err == nil {
			status.Active = strings.TrimSpace(string(out)) == "active"
		}
		if out, err := exec.Command("systemctl", "is-enabled", wgSystemdUnit).CombinedOutput(); err == nil {
			if strings.TrimSpace(string(out)) == "enabled" {
				status.Enabled = true
			}
		}
	}

	if status.Active {
		mergeWgShow(&status)
	}
	return status
}

// parseWireGuardConf parses the INI-style config without external deps.
func parseWireGuardConf(content string, status *TunnelStatus) {
	peers := []TunnelPeer{}
	curIdx := -1 // index into peers for the current [Peer] block

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.Trim(line, "[]")
			if section == "Peer" {
				peers = append(peers, TunnelPeer{})
				curIdx = len(peers) - 1
			} else {
				curIdx = -1
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		if curIdx >= 0 {
			switch key {
			case "PublicKey":
				peers[curIdx].PublicKey = val
			case "AllowedIPs":
				peers[curIdx].AllowedIPs = val
			case "PersistentKeepalive":
				peers[curIdx].Keepalive = val
			case "Endpoint":
				peers[curIdx].Endpoint = val
			}
			continue
		}

		switch key {
		case "Address":
			status.Address = val
		case "ListenPort":
			if n, err := strconv.Atoi(val); err == nil {
				status.ListenPort = n
			}
		case "MTU":
			if n, err := strconv.Atoi(val); err == nil {
				status.MTU = n
			}
		case "DNS":
			status.DNS = val
		case "PrivateKey":
			status.PrivateKey = maskKey(val)
			if pub, err := wgPubKey(val); err == nil {
				status.PublicKey = pub
			}
		}
	}
	status.Peers = peers
}

// mergeWgShow overlays live kernel state (handshake counters, endpoints) on top
// of the file-parsed config.
func mergeWgShow(status *TunnelStatus) {
	out, err := exec.Command("wg", "show", wgIfName, "dump").CombinedOutput()
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	first := true
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if first {
			// interface line: private-key public-key listen-port fwmark
			first = false
			if len(fields) >= 3 {
				if n, err := strconv.Atoi(fields[2]); err == nil {
					status.ListenPort = n
				}
			}
			continue
		}
		if len(fields) < 4 {
			continue
		}
		pubKey := fields[0]
		endpoint := fields[2]
		allowed := fields[3]
		// Look up the matching live peer to fill handshake / traffic counters.
		for j := range status.Peers {
			if status.Peers[j].PublicKey == pubKey {
				status.Peers[j].Endpoint = endpoint
				status.Peers[j].AllowedIPs = allowed
				if len(fields) >= 6 {
					status.Peers[j].LastHandshake, _ = strconv.ParseInt(fields[4], 10, 64)
				}
				if len(fields) >= 7 {
					status.Peers[j].RXBytes, _ = strconv.ParseInt(fields[5], 10, 64)
					status.Peers[j].TXBytes, _ = strconv.ParseInt(fields[6], 10, 64)
				}
				break
			}
		}
	}
}

// ---------------------------------------------------------------------------
// lifecycle
// ---------------------------------------------------------------------------

func createTunnel(req tunnelActionRequest) error {
	if err := ensureWireguardKernel(); err != nil {
		return err
	}
	if !hasCmd("wg") || !hasCmd("wg-quick") {
		return fmt.Errorf("未检测到 wg / wg-quick，请先安装 wireguard-tools（Debian/Ubuntu: apt install wireguard-tools；Alpine: apk add wireguard-tools）")
	}
	if hasFile(wgConfPath) {
		return fmt.Errorf("隧道配置已存在，请先删除或使用 enable")
	}
	if err := os.MkdirAll("/etc/wireguard", 0700); err != nil {
		return fmt.Errorf("创建 /etc/wireguard 失败: %v", err)
	}

	port := req.ListenPort
	if port <= 0 || port > 65535 {
		port = wgDefaultPort
	}
	addr := strings.TrimSpace(req.Address)
	if addr == "" {
		addr = wgDefaultAddress
	}
	mtu := req.MTU
	if mtu <= 0 {
		mtu = wgDefaultMTU
	}
	dns := strings.TrimSpace(req.DNS)
	if dns == "" {
		dns = wgDefaultDNS
	}

	privateKey, err := wgGenKey()
	if err != nil {
		return fmt.Errorf("生成 WireGuard 私钥失败: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("# Managed by CLICD - host WireGuard exit tunnel\n")
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("Address = %s\n", addr))
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", port))
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", privateKey))
	sb.WriteString("SaveConfig = false\n")
	sb.WriteString(fmt.Sprintf("MTU = %d\n", mtu))
	sb.WriteString(fmt.Sprintf("DNS = %s\n", dns))
	sb.WriteString("\n")

	if err := os.WriteFile(wgConfPath, []byte(sb.String()), 0600); err != nil {
		return fmt.Errorf("写入 %s 失败: %v", wgConfPath, err)
	}

	// NAT + forwarding so containers can egress through this tunnel.
	if subnet, ok := normalizeSubnet(addr); ok {
		_ = tunnelEnsureNATRule(subnet, wgIfName)
		_ = ensureForwardRule(wgIfName)
	}
	return nil
}

func enableTunnel() error {
	if err := ensureWireguardKernel(); err != nil {
		return err
	}
	if !hasFile(wgConfPath) {
		return fmt.Errorf("隧道配置不存在，请先生成配置")
	}
	return systemctlAction(wgSystemdUnit, "enable", "--now")
}

func disableTunnel() error {
	if err := systemctlAction(wgSystemdUnit, "stop"); err != nil {
		return err
	}
	// Best effort: also drop the unit from boot.
	if err := systemctlAction(wgSystemdUnit, "disable"); err != nil {
		return nil
	}
	return nil
}

func deleteTunnel() error {
	if err := systemctlAction(wgSystemdUnit, "stop"); err != nil {
		return nil
	}
	if err := systemctlAction(wgSystemdUnit, "disable"); err != nil {
		return nil
	}
	if err := os.Remove(wgConfPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %v", wgConfPath, err)
	}
	return nil
}

func syncTunnel() error {
	if !hasFile(wgConfPath) {
		return fmt.Errorf("隧道配置不存在，请先生成配置")
	}
	return systemctlAction(wgSystemdUnit, "syncconf")
}

func addTunnelPeer(req tunnelActionRequest) error {
	pubKey := strings.TrimSpace(req.PeerPubKey)
	if pubKey == "" {
		return fmt.Errorf("peer_pub_key 不能为空")
	}
	allowed := strings.TrimSpace(req.PeerAllowed)
	if allowed == "" {
		allowed = "0.0.0.0/0, ::/0"
	}
	endpoint := strings.TrimSpace(req.PeerEndpoint)

	content, err := os.ReadFile(wgConfPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %v", wgConfPath, err)
	}
	if strings.Contains(string(content), pubKey) {
		return fmt.Errorf("该 peer 已存在")
	}

	var sb strings.Builder
	sb.WriteString("\n[Peer]\n")
	if name := strings.TrimSpace(req.PeerName); name != "" {
		sb.WriteString(fmt.Sprintf("# %s\n", sanitizeComment(name)))
	}
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", pubKey))
	sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", allowed))
	if endpoint != "" {
		sb.WriteString(fmt.Sprintf("Endpoint = %s\n", endpoint))
	}
	sb.WriteString("PersistentKeepalive = 25\n")

	f, err := os.OpenFile(wgConfPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %v", wgConfPath, err)
	}
	defer f.Close()
	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("写入 %s 失败: %v", wgConfPath, err)
	}

	return syncTunnel()
}

func removeTunnelPeer(pubKey string) error {
	pubKey = strings.TrimSpace(pubKey)
	if pubKey == "" {
		return fmt.Errorf("peer public key 不能为空")
	}
	content, err := os.ReadFile(wgConfPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %v", wgConfPath, err)
	}

	kept := removePeerSection(string(content), pubKey)
	if kept == string(content) {
		return fmt.Errorf("未找到该 peer")
	}
	if err := os.WriteFile(wgConfPath, []byte(kept), 0600); err != nil {
		return fmt.Errorf("写入 %s 失败: %v", wgConfPath, err)
	}
	return syncTunnel()
}

// removePeerSection drops the [Peer] block that carries the given public key.
func removePeerSection(content, pubKey string) string {
	lines := strings.Split(content, "\n")
	var out []string
	skipping := false
	removed := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[Peer]" {
			// Look ahead for the public key of this block.
			blockKey := ""
			for j := i + 1; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if t == "[Peer]" || t == "[Interface]" {
					break
				}
				if strings.HasPrefix(t, "PublicKey = ") {
					blockKey = strings.TrimSpace(strings.TrimPrefix(t, "PublicKey = "))
					break
				}
			}
			skipping = blockKey == pubKey
			if skipping {
				removed = true
				// Drop a trailing blank line that belonged to the removed block.
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				continue
			}
		}
		if skipping {
			// Inside the removed block: skip until the next section header.
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				skipping = false
			} else {
				continue
			}
		}
		out = append(out, line)
	}
	if !removed {
		return content
	}
	return strings.Join(out, "\n")
}

func resetTunnelKey() error {
	content, err := os.ReadFile(wgConfPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %v", wgConfPath, err)
	}
	privateKey, err := wgGenKey()
	if err != nil {
		return fmt.Errorf("生成 WireGuard 私钥失败: %v", err)
	}
	lines := strings.Split(string(content), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "PrivateKey = ") {
			lines[i] = fmt.Sprintf("PrivateKey = %s", privateKey)
			replaced = true
		}
	}
	if !replaced {
		return fmt.Errorf("配置中未找到 PrivateKey")
	}
	if err := os.WriteFile(wgConfPath, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		return fmt.Errorf("写入 %s 失败: %v", wgConfPath, err)
	}
	return syncTunnel()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func hasFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// kernelModuleLoaded reports whether the named kernel module is currently
// loaded (present under /sys/module, or listed in /proc/modules).
func kernelModuleLoaded(name string) bool {
	if _, err := os.Stat("/sys/module/" + name); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/modules"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if f := strings.Fields(line); len(f) >= 1 && f[0] == name {
				return true
			}
		}
	}
	return false
}

// kernelModuleAvailable reports whether the module is loaded or resolvable
// (i.e. modprobe can find it on disk / in kernel config). Built-in modules
// show up via /sys/module; loadable ones are resolved with a dry run.
func kernelModuleAvailable(name string) bool {
	if kernelModuleLoaded(name) {
		return true
	}
	if !hasCmd("modprobe") {
		return false
	}
	out, err := exec.Command("modprobe", "-n", "-v", name).CombinedOutput()
	return err == nil && strings.Contains(string(out), "insmod")
}

// ensureWireguardKernel best-effort loads the kernel modules WireGuard needs
// and reports whether the host can actually run a tunnel. Kernel WireGuard
// needs the `wireguard` module; only userspace implementations (wg-go /
// boringtun) fall back to the `tun` module, so a missing `tun` is not fatal
// when the kernel module is present.
func ensureWireguardKernel() error {
	if hasCmd("modprobe") {
		_ = exec.Command("modprobe", "wireguard").Run()
		_ = exec.Command("modprobe", "tun").Run()
	}
	if kernelModuleAvailable("wireguard") {
		return nil
	}
	if kernelModuleLoaded("tun") || hasFile("/dev/net/tun") {
		return nil
	}
	return fmt.Errorf("WireGuard 内核模块不可用且 tun 模块未加载，无法创建隧道。请确认内核支持 wireguard（modprobe wireguard）或 tun（modprobe tun）；若为 VPS 受限内核，请在管理后台开启或联系供应商")
}

func systemctlAction(unit string, args ...string) error {
	cmd := exec.Command("systemctl", append([]string{unit}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s 失败: %v, %s", unit, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func wgGenKey() (string, error) {
	out, err := exec.Command("wg", "genkey").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func wgPubKey(privateKey string) (string, error) {
	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// normalizeSubnet turns "10.250.250.1/24" into the network "10.250.250.0/24".
func normalizeSubnet(addr string) (string, bool) {
	ip, ipnet, err := net.ParseCIDR(strings.TrimSpace(addr))
	if err != nil {
		return "", false
	}
	_ = ip
	ones, _ := ipnet.Mask.Size()
	return fmt.Sprintf("%s/%d", ipnet.IP.String(), ones), true
}

func tunnelEnsureNATRule(subnet, iface string) error {
	check := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", subnet, "-o", iface, "-j", "MASQUERADE")
	if _, err := check.CombinedOutput(); err == nil {
		return nil
	}
	add := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", subnet, "-o", iface, "-j", "MASQUERADE")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("添加 NAT 规则失败: %v, %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureForwardRule(iface string) error {
	rules := [][]string{
		{"-A", "FORWARD", "-i", iface, "-j", "ACCEPT"},
		{"-A", "FORWARD", "-o", iface, "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		check := exec.Command("iptables", rule...)
		if _, err := check.CombinedOutput(); err == nil {
			continue
		}
		add := exec.Command("iptables", rule...)
		if out, err := add.CombinedOutput(); err != nil {
			return fmt.Errorf("添加 FORWARD 规则失败: %v, %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func sanitizeComment(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}
