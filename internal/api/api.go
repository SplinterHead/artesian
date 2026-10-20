package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/dns"
	"artesian/internal/manifest"
	"artesian/internal/ports"
	"artesian/internal/prefill"
	"artesian/internal/proxy"
	"artesian/internal/scheduler"
	"artesian/internal/services"
	"artesian/internal/ws"
	"artesian/web"
)


// Server coordinates HTTP endpoints and WebSocket streaming
type Server struct {
	cfg       config.ServerConfig
	dnsCfg    *config.DNSConfig
	dnsServer *dns.Server
	cache     *cache.Storage
	db        *db.DB
	registry  *services.Registry
	prefill   *prefill.Manager
	scheduler *scheduler.Scheduler
	proxy     *proxy.Server
}

func (s *Server) getAdapter(service string) (ports.PlatformAdapter, bool) {
	if s.registry == nil {
		return nil, false
	}
	if service == "" {
		service = "steam"
	}
	return s.registry.Get(service)
}

// NewServer initializes the API server
func NewServer(
	cfg config.ServerConfig,
	dnsCfg *config.DNSConfig,
	dnsServer *dns.Server,
	c *cache.Storage,
	database *db.DB,
	reg *services.Registry,
	pf *prefill.Manager,
	sched *scheduler.Scheduler,
	p *proxy.Server,
) *Server {
	return &Server{
		cfg:       cfg,
		dnsCfg:    dnsCfg,
		dnsServer: dnsServer,
		cache:     c,
		db:        database,
		registry:  reg,
		prefill:   pf,
		scheduler: sched,
		proxy:     p,
	}
}

// Handler returns the HTTP handler with all registered endpoints
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// WebSocket Live Streaming Feed (Traffic, Prefill, Steam 2FA, PICS)
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.GetHub().ServeHTTP(w, r)
	})

	// Phase 1: Configuration Endpoints (Upstream DNS & Storage Paths)
	mux.HandleFunc("/api/v1/config/dns", s.handleConfigDNS)
	mux.HandleFunc("/api/v1/config/storage", s.handleConfigStorage)

	// Phase 1 & 2: Stats & Disk I/O Metrics
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/traffic", s.handleTraffic)

	// Game Services Hub Endpoints (Steam only)
	mux.HandleFunc("/api/v1/services", s.handleServices)
	mux.HandleFunc("/api/v1/services/toggle", s.handleServiceToggle)
	mux.HandleFunc("/api/v1/services/auto-update", s.handleServiceAutoUpdateToggle)
	mux.HandleFunc("/api/v1/services/available-games", s.handleAvailableGames)
	mux.HandleFunc("/api/v1/games/platforms", s.handleGamePlatforms)

	// Generic & Platform Authentication Endpoints
	mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/2fa", s.handleAuth2FA)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)

	// Phase 4: Library & Prefill Engine Endpoints
	mux.HandleFunc("/api/v1/library", s.handleLibrary)
	mux.HandleFunc("/api/v1/library/cached", s.handleCachedLibrary)
	mux.HandleFunc("/api/v1/prefill/jobs", s.handlePrefillJobs)
	mux.HandleFunc("/api/v1/prefill/start", s.handlePrefillStart)

	// Phase 5: Automation & Update Check Endpoints
	mux.HandleFunc("/api/v1/scheduler/rules", s.handleSchedulerRules)
	mux.HandleFunc("/api/v1/scheduler/check", s.handleUpdateCheck)
	mux.HandleFunc("/api/v1/updates/log", s.handleUpdateLogs)

	// Embedded Vue/Vuetify SPA & Static Assets
	fileServer := http.FileServer(web.Assets())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		trimmedPath := strings.TrimPrefix(r.URL.Path, "/")
		if trimmedPath == "" {
			trimmedPath = "index.html"
		}

		f, err := web.Assets().Open(trimmedPath)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for client-side routing
		indexFile, err := web.Assets().Open("index.html")
		if err == nil {
			defer indexFile.Close()
			stat, err := indexFile.Stat()
			var modTime time.Time
			if err == nil {
				modTime = stat.ModTime()
			}
			http.ServeContent(w, r, "index.html", modTime, indexFile)
			return
		}

		s.handleFallback(w, r)
	})

	return s.enableCORS(mux)
}

// Phase 1: DNS Configuration
func (s *Server) handleConfigDNS(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		upstreams := s.dnsCfg.UpstreamDNS
		if dbVal := s.db.GetSetting("dns_upstreams", ""); dbVal != "" {
			parts := strings.Split(dbVal, ",")
			var cleaned []string
			for _, p := range parts {
				if t := strings.TrimSpace(p); t != "" {
					cleaned = append(cleaned, t)
				}
			}
			if len(cleaned) > 0 {
				upstreams = cleaned
			}
		}
		interceptIP := s.dnsCfg.InterceptIPv4
		if dbIP := s.db.GetSetting("dns_intercept_ipv4", ""); dbIP != "" {
			interceptIP = dbIP
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"upstream_dns":   upstreams,
			"intercept_ipv4": interceptIP,
			"enabled":        s.dnsCfg.Enabled,
		})
	case http.MethodPost:
		var req struct {
			UpstreamDNS   []string `json:"upstream_dns"`
			InterceptIPv4 string   `json:"intercept_ipv4"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}
		if len(req.UpstreamDNS) > 0 {
			var cleaned []string
			for _, u := range req.UpstreamDNS {
				target := strings.TrimSpace(u)
				if target == "" {
					continue
				}
				if !strings.Contains(target, ":") {
					target += ":53"
				}
				cleaned = append(cleaned, target)
			}
			if len(cleaned) > 0 {
				s.dnsCfg.UpstreamDNS = cleaned
				_ = s.db.SetSetting("dns_upstreams", strings.Join(cleaned, ","))
				if s.proxy != nil {
					s.proxy.SetUpstreamDNS(cleaned)
				}
				if s.prefill != nil {
					s.prefill.SetUpstreamDNS(cleaned)
				}
			}
		}
		if req.InterceptIPv4 != "" {
			s.dnsCfg.InterceptIPv4 = req.InterceptIPv4
			_ = s.db.SetSetting("dns_intercept_ipv4", req.InterceptIPv4)
		}
		if s.dnsServer != nil {
			s.dnsServer.UpdateConfig(s.dnsCfg.InterceptIPv4, s.dnsCfg.UpstreamDNS)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":        "DNS configuration updated",
			"upstream_dns":   s.dnsCfg.UpstreamDNS,
			"intercept_ipv4": s.dnsCfg.InterceptIPv4,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Phase 1: Storage Path Configuration
func (s *Server) handleConfigStorage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		baseDir := s.cache.GetBaseDir()
		maxBytes := int64(2 * 1024 * 1024 * 1024 * 1024)
		if maxStr := s.db.GetSetting("cache_max_size_bytes", ""); maxStr != "" {
			if v, err := strconv.ParseInt(maxStr, 10, 64); err == nil && v > 0 {
				maxBytes = v
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"storage_path":    baseDir,
			"max_size_bytes":  maxBytes,
			"free_disk_bytes": 500 * 1024 * 1024 * 1024,
		})
	case http.MethodPost:
		var req struct {
			StoragePath  string `json:"storage_path"`
			MaxSizeBytes int64  `json:"max_size_bytes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}
		if req.StoragePath != "" {
			if err := s.cache.SetBaseDir(req.StoragePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = s.db.SetSetting("storage_path", req.StoragePath)
		}
		if req.MaxSizeBytes > 0 {
			_ = s.db.SetSetting("cache_max_size_bytes", strconv.FormatInt(req.MaxSizeBytes, 10))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":      "Storage path updated on the tank",
			"storage_path": s.cache.GetBaseDir(),
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Phase 1 & 2: Stats with live disk I/O
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := s.db.GetStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	readRate, writeRate := s.cache.GetIOStats()
	stats.ReadBytesSec = readRate
	stats.WriteBytesSec = writeRate

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stats":   stats,
		"version": "1.0.0",
	})
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if s.proxy != nil {
			s.proxy.ClearActiveDownloads()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Active downloads cleared",
		})
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	var activeDownloads []*proxy.GameDownloadSession
	if s.proxy != nil {
		activeDownloads = s.proxy.GetActiveDownloads()
	}

	logs, err := s.db.GetRecentRequestLogs(limit)
	if err != nil {
		logs = []db.RequestLog{}
	}

	traffic := make([]map[string]interface{}, 0, len(logs))
	for _, l := range logs {
		svc := s.registry.Identify(l.Host)
		if svc == nil {
			continue
		}
		svcName := svc.Name()

		var depotID int64
		var chunkID string
		var lookupURL string

		if matches := proxy.DepotRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
			depotID, _ = strconv.ParseInt(matches[1], 10, 64)
		}
		if depotID > 0 && svc != nil {
			chunkID = fmt.Sprintf("%d", depotID)
			lookupURL = svc.DepotLookupURL(depotID)
		}

		var gameTitle string
		var platform string
		var totalSize int64 = l.BytesTransferred

		if svc != nil {
			info := svc.ResolveDepot(depotID, "")
			gameTitle = info.GameTitle
			platform = info.Platform
			if info.TotalSize > 0 {
				totalSize = info.TotalSize
			}
		}

		ts := l.CreatedAt.Format("15:04:05")
		if l.CreatedAt.IsZero() {
			ts = time.Now().Format("15:04:05")
		}

		clientIP := l.ClientIP
		if ip, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = ip
		}
		clientIP = strings.TrimPrefix(clientIP, "[")
		clientIP = strings.TrimSuffix(clientIP, "]")

		traffic = append(traffic, map[string]interface{}{
			"timestamp":         ts,
			"time":              ts,
			"client_ip":         clientIP,
			"host":              l.Host,
			"method":            l.Method,
			"url":               l.Path,
			"bytes_transferred": l.BytesTransferred,
			"is_hit":            l.IsHit,
			"status":            l.Status,
			"duration_ms":       l.DurationMs,
			"service":           svcName,
			"store":             proxy.FormatStoreName(svcName),
			"game_title":        gameTitle,
			"platform":          platform,
			"depot_id":          depotID,
			"chunk_id":          chunkID,
			"lookup_url":        lookupURL,
			"total_depot_size":  totalSize,
		})
	}

	// activeDownloads contains ONLY downloads currently ongoing in the proxy engine
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"downloads": activeDownloads,
		"traffic":   traffic,
	})
}



// ServiceItem represents metadata for an integrated or planned game service
type ServiceItem struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	Company          string   `json:"company"`
	Status           string   `json:"status"` // "connected", "ready", "disabled", "planned"
	InterceptEnabled bool     `json:"intercept_enabled"`
	AutoUpdate       bool     `json:"auto_update"`
	Username         string   `json:"username,omitempty"`
	Domains          []string `json:"domains"`
	Description      string   `json:"description"`
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var servicesList []ServiceItem
	if s.registry != nil {
		for _, adapter := range s.registry.All() {
			status := adapter.Status()
			autoUpdate := true
			if s.db != nil {
				autoUpdate = s.db.GetServiceAutoUpdate(adapter.ID())
			}

			servicesList = append(servicesList, ServiceItem{
				ID:               adapter.ID(),
				DisplayName:      adapter.DisplayName(),
				Company:          adapter.Company(),
				Status:           string(status.State),
				InterceptEnabled: s.registry.IsEnabled(adapter.ID()),
				AutoUpdate:       autoUpdate,
				Username:         status.Username,
				Domains:          adapter.Domains(),
				Description:      adapter.Description(),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": servicesList,
	})
}

type AvailableGame struct {
	AppID     int64    `json:"app_id"`
	Service   string   `json:"service"`
	Name      string   `json:"name"`
	Platforms []string `json:"platforms"`
}

func (s *Server) handleAvailableGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	service := r.URL.Query().Get("service")
	if service == "" {
		service = "steam"
	}

	adapter, ok := s.getAdapter(service)
	if !ok {
		http.Error(w, fmt.Sprintf("Service '%s' not registered", service), http.StatusNotFound)
		return
	}

	if !adapter.Status().HasValidAuth {
		http.Error(w, fmt.Sprintf("%s service is not authenticated", adapter.DisplayName()), http.StatusUnauthorized)
		return
	}

	var games []AvailableGame
	var privacyRestricted bool
	var privacyHint string

	res, err := adapter.GetOwnedGames()
	if err != nil {
		log.Printf("[API] Failed to fetch %s account games: %v", adapter.DisplayName(), err)
	} else if res != nil {
		for _, g := range res.Games {
			games = append(games, AvailableGame{
				AppID:     g.AppID,
				Service:   service,
				Name:      g.Name,
				Platforms: g.Platforms,
			})
		}
		privacyRestricted = res.PrivacyRestricted
		privacyHint = res.Hint
	}

	if games == nil {
		games = []AvailableGame{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service":            service,
		"games":              games,
		"privacy_restricted": privacyRestricted,
		"hint":               privacyHint,
	})
}

func (s *Server) handleGamePlatforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	appIDStr := r.URL.Query().Get("app_id")
	appID, _ := strconv.ParseInt(appIDStr, 10, 64)
	if appID <= 0 {
		http.Error(w, "Invalid app_id", http.StatusBadRequest)
		return
	}
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "steam"
	}

	var name string
	var platforms []string
	if adapter, ok := s.getAdapter(service); ok {
		name, platforms = adapter.ResolveAppPlatforms(appID, "")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"app_id":    appID,
		"service":   service,
		"name":      name,
		"platforms": platforms,
	})
}

func (s *Server) handleServiceToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	s.registry.SetEnabled(req.ID, req.Enabled)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      req.ID,
		"enabled": s.registry.IsEnabled(req.ID),
	})
}

func (s *Server) handleServiceAutoUpdateToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID         string `json:"id"`
		AutoUpdate bool   `json:"auto_update"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if s.db != nil {
		_ = s.db.SetServiceAutoUpdate(req.ID, req.AutoUpdate)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":          req.ID,
		"auto_update": req.AutoUpdate,
	})
}

// Generic Platform Authentication Handlers
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "steam"
	}
	adapter, ok := s.getAdapter(service)
	if !ok {
		http.Error(w, fmt.Sprintf("Service '%s' not registered", service), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, adapter.Status())
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service  string `json:"service"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid credentials payload", http.StatusBadRequest)
		return
	}
	if req.Service == "" {
		req.Service = "steam"
	}

	adapter, ok := s.getAdapter(req.Service)
	if !ok {
		http.Error(w, fmt.Sprintf("Service '%s' not registered", req.Service), http.StatusNotFound)
		return
	}

	if err := adapter.Login(req.Username, req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("Connecting to %s authentication service...", adapter.DisplayName()),
		"status":  adapter.Status(),
	})
}

func (s *Server) handleAuth2FA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
		Code    string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid code payload", http.StatusBadRequest)
		return
	}
	if req.Service == "" {
		req.Service = "steam"
	}

	adapter, ok := s.getAdapter(req.Service)
	if !ok {
		http.Error(w, fmt.Sprintf("Service '%s' not registered", req.Service), http.StatusNotFound)
		return
	}

	if err := adapter.Submit2FA(req.Code); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("%s 2FA verification successful", adapter.DisplayName()),
		"status":  adapter.Status(),
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Service == "" {
		req.Service = "steam"
	}

	adapter, ok := s.getAdapter(req.Service)
	if !ok {
		http.Error(w, fmt.Sprintf("Service '%s' not registered", req.Service), http.StatusNotFound)
		return
	}

	_ = adapter.Logout()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("%s session cleared", adapter.DisplayName()),
		"status":  adapter.Status(),
	})
}

// Phase 4: Library Handlers
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apps, err := s.db.GetWatchedApps()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if apps == nil {
			apps = []db.WatchedApp{}
		}
		writeJSON(w, http.StatusOK, apps)
	case http.MethodPost:
		var req struct {
			AppID            int64  `json:"app_id"`
			Service          string `json:"service"`
			Name             string `json:"name"`
			Platform         string `json:"platform"`
			AutoUpdate       *bool  `json:"auto_update"`
			ReplacePlatforms bool   `json:"replace_platforms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if req.Service == "" {
			req.Service = "steam"
		}

		var platformToSave string
		if req.ReplacePlatforms {
			platformToSave = req.Platform
		} else {
			var existingPlat string
			if s.db != nil {
				existingPlat, _ = s.db.GetWatchedAppPlatform(req.AppID, req.Service)
			}
			platformToSave = db.MergePlatformStrings(existingPlat, req.Platform)
		}

		autoUpdateVal := true
		if req.AutoUpdate != nil {
			autoUpdateVal = *req.AutoUpdate
		} else if s.db != nil {
			autoUpdateVal = s.db.GetServiceAutoUpdate(req.Service)
		}

		app := db.WatchedApp{
			AppID:      req.AppID,
			Service:    req.Service,
			Name:       req.Name,
			Platform:   platformToSave,
			AutoUpdate: autoUpdateVal,
			Status:     "prefilling",
		}
		_ = s.db.UpsertWatchedApp(app)

		// Record persistent update log entry in SQLite
		if s.db != nil {
			logEntry := db.UpdateLogEntry{
				AppID:        req.AppID,
				Service:      req.Service,
				GameTitle:    req.Name,
				Status:       "Discovered",
				Message:      fmt.Sprintf("Game added to managed library. Automated caching and prefill initiated for %s.", req.Name),
				DiscoveredAt: time.Now(),
			}
			_ = s.db.AddUpdateLog(logEntry)
			ws.GetHub().Broadcast("update:discovered", logEntry)
		}

		// Prefetch depot metadata in background if registered platform adapter
		if s.registry != nil {
			if adapter, ok := s.registry.Get(req.Service); ok {
				go func(a ports.PlatformAdapter, appID int64) {
					_ = a.FetchAppInfo(appID)
				}(adapter, req.AppID)
			}
		}

		// Automatically start caching the depot
		if s.prefill != nil {
			go func() {
				s.prefill.CancelJobsForApp(req.Service, req.AppID)
				_, _ = s.prefill.StartJob(context.Background(), req.Service, req.AppID, req.Name)
			}()
		}

		writeJSON(w, http.StatusCreated, app)
	case http.MethodDelete:
		appIDStr := r.URL.Query().Get("app_id")
		service := r.URL.Query().Get("service")
		if service == "" {
			service = "steam"
		}
		appID, _ := strconv.ParseInt(appIDStr, 10, 64)
		s.purgeGameData(service, appID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Game removed from library and cache purged",
		})
	case http.MethodPatch, http.MethodPut:
		var req struct {
			AppID      int64  `json:"app_id"`
			Service    string `json:"service"`
			AutoUpdate *bool  `json:"auto_update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AppID <= 0 || req.AutoUpdate == nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if req.Service == "" {
			req.Service = "steam"
		}
		if s.db != nil {
			_ = s.db.UpdateWatchedAppAutoUpdate(req.AppID, req.Service, *req.AutoUpdate)
		}
		ws.GetHub().Broadcast("library:updated", map[string]interface{}{
			"app_id":      req.AppID,
			"service":     req.Service,
			"auto_update": *req.AutoUpdate,
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"app_id":      req.AppID,
			"service":     req.Service,
			"auto_update": *req.AutoUpdate,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// purgeGameData purges all disk chunks, manifests, and database records for an app or service
func (s *Server) purgeGameData(service string, appID int64) {
	if service == "" {
		service = "steam"
	}

	// a) Cancel any active downloads (prefill engine and proxy active download sessions)
	if s.prefill != nil {
		s.prefill.CancelJobsForApp(service, appID)
	}
	if s.proxy != nil {
		s.proxy.CancelActiveDownloadsForApp(service, appID)
	}

	// b) Delete associated chunks and manifests on disk and in database
	var depotIDs []int64
	var chunkPaths []string
	if s.db != nil {
		depotIDs, _ = s.db.GetDepotIDsForApp(appID, service)
		chunkPaths, _ = s.db.GetChunkPathsForApp(appID, service)
	}

	// 1. Remove individual chunk files from disk
	for _, p := range chunkPaths {
		if p != "" {
			_ = os.Remove(p)
		}
	}

	// 2. Remove depot chunk directories from cache storage
	if s.cache != nil {
		for _, dID := range depotIDs {
			_ = s.cache.DeleteDepot(service, dID)
		}
	}

	// 3. Remove manifest files from disk (.manifest files)
	storageDir := "/data/cache"
	if s.cache != nil {
		storageDir = s.cache.GetBaseDir()
	}
	for _, dID := range depotIDs {
		_ = manifest.DeleteManifestFiles(storageDir, dID)
	}

	// 4. Delete records from database (depot_manifest_chunks, chunk_cache, watched_apps, depot_meta_cache)
	if s.db != nil {
		_ = s.db.DeleteChunksAndManifestsForApp(appID, service)
	}

	// 5. Broadcast updates over WebSocket
	ws.GetHub().Broadcast("library:updated", map[string]interface{}{
		"deleted_app_id": appID,
		"service":        service,
	})
	if s.db != nil {
		stats, err := s.db.GetStats()
		if err == nil {
			ws.GetHub().Broadcast("stats:updated", stats)
		}
	}

	log.Printf("[Library] Purged game %d (%s): cancelled active downloads and purged cached chunks/manifests", appID, service)
}

func (s *Server) handleCachedLibrary(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		games, err := s.db.GetInterceptedGames()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if games == nil {
			games = []db.InterceptedGame{}
		}
		writeJSON(w, http.StatusOK, games)
	case http.MethodDelete:
		appIDStr := r.URL.Query().Get("app_id")
		service := r.URL.Query().Get("service")
		if service == "" {
			service = "steam"
		}
		appID, _ := strconv.ParseInt(appIDStr, 10, 64)
		s.purgeGameData(service, appID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Cached game and content purged",
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Phase 4: Prefill Handlers
func (s *Server) handlePrefillJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.prefill.ListJobs()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
	})
}

func (s *Server) handlePrefillStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
		AppID   int64  `json:"app_id"`
		AppName string `json:"app_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	if req.Service == "" {
		req.Service = "steam"
	}

	job, err := s.prefill.StartJob(context.Background(), req.Service, req.AppID, req.AppName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

// Phase 5: Scheduler & PICS Handlers
func (s *Server) handleSchedulerRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := s.scheduler.GetRules()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"rules": rules,
		})
	case http.MethodPost:
		var req struct {
			ID             string `json:"id"`
			CronExpression string `json:"cron_expression"`
			Enabled        bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}
		if err := s.scheduler.UpdateRule(req.ID, req.CronExpression, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Scheduler rule updated",
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go s.scheduler.CheckUpdates()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Update check dispatched",
	})
}

func (s *Server) handleUpdateLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	logs, err := s.db.GetUpdateLogs(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []db.UpdateLogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs": logs,
	})
}

func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Artesian Control Center</title>
</head>
<body>
    <h1>Artesian Control Center</h1>
    <p>Proxy engine and control server active.</p>
</body>
</html>`
	_, _ = w.Write([]byte(html))
}

func (s *Server) enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
