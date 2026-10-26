package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
)

type proxyRuleResponse struct {
	ID                int    `json:"id"`
	Domain            string `json:"domain"`
	Path              string `json:"path,omitempty"`
	TargetContainerID int    `json:"target_container_id"`
	TargetContainer   string `json:"target_container"`
	TargetPort        int    `json:"target_port"`
	Name              string `json:"name,omitempty"`
	Enabled           bool   `json:"enabled"`
	TLS               bool   `json:"tls"`
	CreatedAt         string `json:"created_at"`
}

type proxyRuleCreateRequest struct {
	Domain            string `json:"domain"`
	Path              string `json:"path,omitempty"`
	TargetContainerID int    `json:"target_container_id"`
	TargetPort        int    `json:"target_port"`
	Name              string `json:"name,omitempty"`
	TLS               bool   `json:"tls"`
}

type proxyRuleUpdateRequest struct {
	Domain            string `json:"domain,omitempty"`
	Path              string `json:"path,omitempty"`
	TargetContainerID int    `json:"target_container_id,omitempty"`
	TargetPort        int    `json:"target_port,omitempty"`
	Name              string `json:"name,omitempty"`
	Enabled           *bool  `json:"enabled,omitempty"`
	TLS               *bool  `json:"tls,omitempty"`
}

var proxyRulesMu sync.Mutex

const (
	caddyfilePath   = "/etc/caddy/Caddyfile"
	caddyService    = "caddy"
)

// proxyRuleNextID returns a fresh rule id (simple monotonically increasing id).
func proxyRuleNextID() int {
	max := 0
	for _, r := range config.AppConfig.ProxyRules {
		if r.ID > max {
			max = r.ID
		}
	}
	return max + 1
}

func proxyRuleByID(id int) *config.ProxyRule {
	for i := range config.AppConfig.ProxyRules {
		if config.AppConfig.ProxyRules[i].ID == id {
			return &config.AppConfig.ProxyRules[i]
		}
	}
	return nil
}

func proxyRuleFromPath(path string) string {
	// path like /api/proxy/rules/123
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[len(parts)-1]
	}
	return ""
}

func normalizeProxyRule(req proxyRuleCreateRequest) error {
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	domain = strings.ToLower(domain)
	req.Domain = domain

	if req.TargetContainerID <= 0 {
		return fmt.Errorf("target_container_id is required")
	}
	c := config.FindContainer(req.TargetContainerID)
	if c == nil {
		return fmt.Errorf("container not found: %d", req.TargetContainerID)
	}
	if c.IP == "" {
		return fmt.Errorf("container %s has no IP assigned", c.Name)
	}
	if req.TargetPort <= 0 || req.TargetPort > 65535 {
		return fmt.Errorf("target_port must be between 1 and 65535")
	}
	if req.Path != "" && !strings.HasPrefix(req.Path, "/") {
		return fmt.Errorf("path must start with '/'")
	}
	return nil
}

// toProxyRuleResponse builds a response struct from a stored rule.
func toProxyRuleResponse(rule config.ProxyRule) proxyRuleResponse {
	resp := proxyRuleResponse{
		ID:                rule.ID,
		Domain:            rule.Domain,
		Path:              rule.Path,
		TargetContainerID: rule.TargetContainerID,
		TargetPort:        rule.TargetPort,
		Name:              rule.Name,
		Enabled:           rule.Enabled,
		TLS:               rule.TLS,
		CreatedAt:         rule.CreatedAt,
	}
	if c := config.FindContainer(rule.TargetContainerID); c != nil {
		resp.TargetContainer = c.Name
	}
	return resp
}

// writeCaddyfile renders all enabled proxy rules into /etc/caddy/Caddyfile.
func writeCaddyfile() error {
	var lines []string
	lines = append(lines, "# Managed by CLICD — do not edit by hand. Edit rules in the panel.")
	lines = append(lines, "")

	for _, rule := range config.AppConfig.ProxyRules {
		if !rule.Enabled {
			continue
		}
		c := config.FindContainer(rule.TargetContainerID)
		if c == nil || c.IP == "" {
			continue
		}
		scheme := ""
		if rule.TLS {
			scheme = "https://"
		} else {
			scheme = "http://"
		}
		host := scheme + rule.Domain
		if rule.Path != "" {
			host = host + rule.Path
		}
		lines = append(lines, host+" {")
		lines = append(lines, "\treverse_proxy "+c.IP+":"+strconv.Itoa(rule.TargetPort))
		if rule.Path != "" {
			lines = append(lines, "\turi strip_prefix "+rule.Path)
		}
		lines = append(lines, "}")
		lines = append(lines, "")
	}

	if len(lines) <= 2 {
		lines = []string{"# No active proxy rules managed by CLICD", ""}
	}

	return os.WriteFile(caddyfilePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func reloadCaddy() error {
	// Start service if not running, then reload config.
	if out, err := exec.Command("systemctl", "start", caddyService).CombinedOutput(); err != nil {
		// Service may not exist yet on a fresh install — ignore, Caddy may be managed elsewhere.
		_ = out
	}
	if out, err := exec.Command("systemctl", "reload", caddyService).CombinedOutput(); err != nil {
		return fmt.Errorf("reload failed: %v, output: %s", err, string(out))
	}
	return nil
}

// HandleProxyRules handles GET /api/proxy/rules and POST /api/proxy/rules.
func HandleProxyRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		proxyRulesMu.Lock()
		rules := make([]proxyRuleResponse, 0, len(config.AppConfig.ProxyRules))
		for _, rule := range config.AppConfig.ProxyRules {
			rules = append(rules, toProxyRuleResponse(rule))
		}
		proxyRulesMu.Unlock()
		sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: rules})
	case http.MethodPost:
		if !requireScope(w, r, "proxy:write") {
			return
		}
		var req proxyRuleCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		if err := normalizeProxyRule(req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}

		proxyRulesMu.Lock()
		rule := config.ProxyRule{
			ID:                proxyRuleNextID(),
			Domain:            strings.ToLower(strings.TrimSpace(req.Domain)),
			Path:              strings.TrimSpace(req.Path),
			TargetContainerID: req.TargetContainerID,
			TargetPort:        req.TargetPort,
			Name:              strings.TrimSpace(req.Name),
			Enabled:           true,
			TLS:               req.TLS,
			CreatedAt:         time.Now().Format("2006-01-02T15:04:05Z07:00"),
		}
		config.AppConfig.ProxyRules = append(config.AppConfig.ProxyRules, rule)
		if err := config.SaveConfig(); err != nil {
			config.AppConfig.ProxyRules = config.AppConfig.ProxyRules[:len(config.AppConfig.ProxyRules)-1]
			proxyRulesMu.Unlock()
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save proxy rule"})
			return
		}
		_ = writeCaddyfile()
		_ = reloadCaddy()
		proxyRulesMu.Unlock()

		auditRequest(r, "proxy.create", rule.Domain, fmt.Sprintf("container=%d port=%d tls=%v", rule.TargetContainerID, rule.TargetPort, rule.TLS), true, "")
		jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Data: toProxyRuleResponse(rule), Message: "Proxy rule created"})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleProxyRule handles GET/PUT/DELETE /api/proxy/rules/{id}.
func HandleProxyRule(w http.ResponseWriter, r *http.Request) {
	idStr := proxyRuleFromPath(r.URL.Path)
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Rule ID required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		proxyRulesMu.Lock()
		rule := proxyRuleByID(id)
		proxyRulesMu.Unlock()
		if rule == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Proxy rule not found"})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: toProxyRuleResponse(*rule)})

	case http.MethodPut:
		if !requireScope(w, r, "proxy:write") {
			return
		}
		var req proxyRuleUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}

		proxyRulesMu.Lock()
		rule := proxyRuleByID(id)
		if rule == nil {
			proxyRulesMu.Unlock()
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Proxy rule not found"})
			return
		}
		if req.Domain != "" {
			rule.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
		}
		if req.Path != "" {
			rule.Path = strings.TrimSpace(req.Path)
		}
		if req.TargetContainerID != 0 {
			c := config.FindContainer(req.TargetContainerID)
			if c == nil || c.IP == "" {
				proxyRulesMu.Unlock()
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Target container not found or has no IP"})
				return
			}
			rule.TargetContainerID = req.TargetContainerID
		}
		if req.TargetPort != 0 {
			if req.TargetPort <= 0 || req.TargetPort > 65535 {
				proxyRulesMu.Unlock()
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "target_port must be between 1 and 65535"})
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

		// replace the entry in place
		for i := range config.AppConfig.ProxyRules {
			if config.AppConfig.ProxyRules[i].ID == id {
				config.AppConfig.ProxyRules[i] = *rule
				break
			}
		}
		if err := config.SaveConfig(); err != nil {
			proxyRulesMu.Unlock()
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save proxy rule"})
			return
		}
		_ = writeCaddyfile()
		_ = reloadCaddy()
		proxyRulesMu.Unlock()

		auditRequest(r, "proxy.update", rule.Domain, fmt.Sprintf("id=%d tls=%v enabled=%v", id, rule.TLS, rule.Enabled), true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: toProxyRuleResponse(*rule), Message: "Proxy rule updated"})

	case http.MethodDelete:
		if !requireScope(w, r, "proxy:write") {
			return
		}
		proxyRulesMu.Lock()
		rule := proxyRuleByID(id)
		if rule == nil {
			proxyRulesMu.Unlock()
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Proxy rule not found"})
			return
		}
		domain := rule.Domain
		idx := -1
		for i := range config.AppConfig.ProxyRules {
			if config.AppConfig.ProxyRules[i].ID == id {
				idx = i
				break
			}
		}
		if idx >= 0 {
			config.AppConfig.ProxyRules = append(config.AppConfig.ProxyRules[:idx], config.AppConfig.ProxyRules[idx+1:]...)
		}
		if err := config.SaveConfig(); err != nil {
			proxyRulesMu.Unlock()
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to delete proxy rule"})
			return
		}
		_ = writeCaddyfile()
		_ = reloadCaddy()
		proxyRulesMu.Unlock()

		auditRequest(r, "proxy.delete", domain, fmt.Sprintf("id=%d", id), true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Proxy rule deleted"})

	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}
