package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"clicd/internal/config"
)

const (
	caddyfilePath = "/etc/caddy/Caddyfile"
	caddyService  = "caddy"
)

// proxyRuleNextID returns a fresh rule id for the given container.
func proxyRuleNextID(c *config.Container) int {
	max := 0
	for _, r := range c.ProxyRules {
		if r.ID > max {
			max = r.ID
		}
	}
	return max + 1
}

func proxyRuleByID(c *config.Container, id int) *config.ProxyRule {
	for i := range c.ProxyRules {
		if c.ProxyRules[i].ID == id {
			return &c.ProxyRules[i]
		}
	}
	return nil
}

func proxyRuleIndex(c *config.Container, id int) int {
	for i := range c.ProxyRules {
		if c.ProxyRules[i].ID == id {
			return i
		}
	}
	return -1
}

// getProxyRules lists the container's reverse-proxy rules plus the container IP
// (so the UI can show what each rule points at).
func getProxyRules(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"rules":         c.ProxyRules,
		"limit":         c.ProxyRuleLimit,
		"container_ip":  c.IP,
		"caddy_installed": caddyInstalled(),
	}})
}

func caddyInstalled() bool {
	_, err := exec.LookPath("caddy")
	return err == nil
}

// addProxyRule is the user-facing action: add a reverse-proxy rule to a container.
func addProxyRule(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if c.ProxyRuleLimit <= 0 {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "该容器未分配反代配额"})
		return
	}
	if len(c.ProxyRules) >= c.ProxyRuleLimit {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: fmt.Sprintf("反代配额已满: %d/%d", len(c.ProxyRules), c.ProxyRuleLimit)})
		return
	}
	var req struct {
		Domain     string `json:"domain"`
		Path       string `json:"path"`
		TargetPort int    `json:"target_port"`
		Name       string `json:"name"`
		TLS        bool   `json:"tls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if err := validateProxyRuleFields(req.Domain, req.Path, req.TargetPort); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if proxyDomainUsedBy(c, domain, req.Path, -1) {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "该域名+路径已被占用"})
		return
	}
	rule := config.ProxyRule{
		ID:         proxyRuleNextID(c),
		Domain:     domain,
		Path:       strings.TrimSpace(req.Path),
		TargetPort: req.TargetPort,
		Name:       strings.TrimSpace(req.Name),
		Enabled:    true,
		TLS:        req.TLS,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	c.ProxyRules = append(c.ProxyRules, rule)
	config.SaveConfig()
	auditRequest(r, "proxy:create", fmt.Sprintf("%s(%d)", c.Name, c.ID), fmt.Sprintf("添加反代 %s%s -> :%d", domain, rule.Path, rule.TargetPort), true, "")
	if err := writeAndReloadCaddy(); err != nil {
		// non-fatal: rule saved, caddy may need manual reload
		fmt.Printf("warning: caddy reload failed: %v\n", err)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c.ProxyRules})
}

// updateProxyRule toggles enabled / edits fields (user-facing).
func updateProxyRule(w http.ResponseWriter, r *http.Request, id int, idStr string) {
	ruleID := parseIntSafe(idStr)
	if ruleID <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid proxy rule id"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	idx := proxyRuleIndex(c, ruleID)
	if idx < 0 {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Proxy rule not found"})
		return
	}
	var req struct {
		Domain   string `json:"domain"`
		Path     string `json:"path"`
		TargetPort int  `json:"target_port"`
		Name     string `json:"name"`
		Enabled  *bool  `json:"enabled"`
		TLS      *bool  `json:"tls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	rule := c.ProxyRules[idx]
	if req.Domain != "" {
		domain := strings.ToLower(strings.TrimSpace(req.Domain))
		if proxyDomainUsedBy(c, domain, req.Path, idx) {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "该域名+路径已被占用"})
			return
		}
		rule.Domain = domain
	}
	if req.Path != "" || req.Path == "" {
		rule.Path = strings.TrimSpace(req.Path)
	}
	if req.TargetPort != 0 {
		if err := validatePort(req.TargetPort); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		rule.TargetPort = req.TargetPort
	}
	if req.Name != "" {
		rule.Name = strings.TrimSpace(req.Name)
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.TLS != nil {
		rule.TLS = *req.TLS
	}
	c.ProxyRules[idx] = rule
	config.SaveConfig()
	auditRequest(r, "proxy:update", fmt.Sprintf("%s(%d)", c.Name, c.ID), fmt.Sprintf("更新反代 #%d", ruleID), true, "")
	if err := writeAndReloadCaddy(); err != nil {
		fmt.Printf("warning: caddy reload failed: %v\n", err)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c.ProxyRules})
}

// deleteProxyRule removes a rule (user-facing).
func deleteProxyRule(w http.ResponseWriter, r *http.Request, id int, idStr string) {
	ruleID := parseIntSafe(idStr)
	if ruleID <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid proxy rule id"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	idx := proxyRuleIndex(c, ruleID)
	if idx < 0 {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Proxy rule not found"})
		return
	}
	c.ProxyRules = append(c.ProxyRules[:idx], c.ProxyRules[idx+1:]...)
	config.SaveConfig()
	auditRequest(r, "proxy:delete", fmt.Sprintf("%s(%d)", c.Name, c.ID), fmt.Sprintf("删除反代 #%d", ruleID), true, "")
	if err := writeAndReloadCaddy(); err != nil {
		fmt.Printf("warning: caddy reload failed: %v\n", err)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c.ProxyRules})
}

// ----- helpers -----

func validateProxyRuleFields(domain, path string, port int) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain too long")
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /")
	}
	return validatePort(port)
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("target_port must be 1-65535")
	}
	return nil
}

// proxyDomainUsedBy reports whether another rule in the same container already binds domain+path.
func proxyDomainUsedBy(c *config.Container, domain, path string, excludeIdx int) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	path = strings.TrimSpace(path)
	for i, r := range c.ProxyRules {
		if i == excludeIdx {
			continue
		}
		if strings.EqualFold(r.Domain, domain) && strings.TrimSpace(r.Path) == path {
			return true
		}
	}
	return false
}

func parseIntSafe(s string) int {
	n := 0
	for _, ch := range strings.TrimSpace(s) {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// writeAndReloadCaddy regenerates /etc/caddy/Caddyfile from every container's enabled
// proxy rules and reloads the caddy service. Errors are returned but non-fatal.
func writeAndReloadCaddy() error {
	var b strings.Builder
	hasRules := false
	for _, c := range config.AppConfig.Containers {
		for _, r := range c.ProxyRules {
			if !r.Enabled || c.IP == "" {
				continue
			}
			hasRules = true
			scheme := "http://"
			if r.TLS {
				scheme = "https://"
			}
			b.WriteString(r.Domain + " {\n")
			if r.Path != "" {
				b.WriteString("\thandle " + r.Path + " {\n")
				b.WriteString("\t\treverse_proxy " + scheme + c.IP + ":" + fmt.Sprintf("%d", r.TargetPort) + "\n")
				b.WriteString("\t}\n")
			} else {
				b.WriteString("\treverse_proxy " + scheme + c.IP + ":" + fmt.Sprintf("%d", r.TargetPort) + "\n")
			}
			if r.TLS {
				// Let Caddy manage ACME automatically (default).
			}
			b.WriteString("}\n\n")
		}
	}
	// If caddy isn't installed we cannot write — skip silently.
	if _, err := exec.LookPath("caddy"); err != nil {
		return nil
	}
	content := b.String()
	if !hasRules {
		content = "# no proxy rules\n"
	}
	if err := os.WriteFile(caddyfilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	// ensure caddy is running, then reload
	_ = exec.Command("systemctl", "start", caddyService).Run()
	if err := exec.Command("systemctl", "reload", caddyService).Run(); err != nil {
		return fmt.Errorf("reload caddy: %w", err)
	}
	return nil
}
