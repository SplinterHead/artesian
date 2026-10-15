package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/prefill"
	"artesian/internal/proxy"
	"artesian/internal/scheduler"
	"artesian/internal/services"
	"artesian/internal/steam"
	"artesian/internal/ws"
	"artesian/web"
)


// Server coordinates HTTP endpoints and WebSocket streaming
type Server struct {
	cfg       config.ServerConfig
	dnsCfg    *config.DNSConfig
	cache     *cache.Storage
	db        *db.DB
	registry  *services.Registry
	prefill   *prefill.Manager
	scheduler *scheduler.Scheduler
	steam     *steam.Client
	proxy     *proxy.Server
}

// NewServer initializes the API server
func NewServer(
	cfg config.ServerConfig,
	dnsCfg *config.DNSConfig,
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
		cache:     c,
		db:        database,
		registry:  reg,
		prefill:   pf,
		scheduler: sched,
		steam:     steam.GetClient(database),
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

	// Game Services Hub Endpoints (Steam, Epic, Blizzard, Riot, Xbox)
	mux.HandleFunc("/api/v1/services", s.handleServices)
	mux.HandleFunc("/api/v1/services/toggle", s.handleServiceToggle)

	// Phase 3: Steam Connection & Authentication Endpoints
	mux.HandleFunc("/api/v1/steam/status", s.handleSteamStatus)
	mux.HandleFunc("/api/v1/steam/login", s.handleSteamLogin)
	mux.HandleFunc("/api/v1/steam/2fa", s.handleSteam2FA)
	mux.HandleFunc("/api/v1/steam/logout", s.handleSteamLogout)

	// Phase 4: Library & Prefill Engine Endpoints
	mux.HandleFunc("/api/v1/library", s.handleLibrary)
	mux.HandleFunc("/api/v1/prefill/jobs", s.handlePrefillJobs)
	mux.HandleFunc("/api/v1/prefill/start", s.handlePrefillStart)

	// Phase 5: Automation & PICS Endpoints
	mux.HandleFunc("/api/v1/scheduler/rules", s.handleSchedulerRules)
	mux.HandleFunc("/api/v1/scheduler/pics/poll", s.handlePICSPoll)
	mux.HandleFunc("/api/v1/scheduler/pics/trigger", s.handlePICSTrigger)

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
			upstreams = strings.Split(dbVal, ",")
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"upstream_dns":   upstreams,
			"intercept_ipv4": s.dnsCfg.InterceptIPv4,
			"enabled":        s.dnsCfg.Enabled,
		})
	case http.MethodPost:
		var req struct {
			UpstreamDNS []string `json:"upstream_dns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}
		if len(req.UpstreamDNS) > 0 {
			s.dnsCfg.UpstreamDNS = req.UpstreamDNS
			_ = s.db.SetSetting("dns_upstreams", strings.Join(req.UpstreamDNS, ","))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":      "DNS configuration updated",
			"upstream_dns": s.dnsCfg.UpstreamDNS,
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"storage_path":    baseDir,
			"max_size_bytes":  2 * 1024 * 1024 * 1024 * 1024,
			"free_disk_bytes": 500 * 1024 * 1024 * 1024,
		})
	case http.MethodPost:
		var req struct {
			StoragePath string `json:"storage_path"`
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

		if svcName == "steam" {
			if matches := proxy.DepotRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				depotID, _ = strconv.ParseInt(matches[1], 10, 64)
			}
			if depotID > 0 {
				chunkID = fmt.Sprintf("%d", depotID)
				lookupURL = fmt.Sprintf("https://steamdb.info/depot/%d", depotID)
			}
		} else if svcName == "epic" {
			if matches := proxy.EpicChunkRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				chunkID = matches[1]
			}
			var appName string
			if matches := proxy.EpicBuildRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				appName = matches[1]
			}
			if appName != "" {
				lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(appName))
			} else if chunkID != "" {
				lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(chunkID))
			} else {
				lookupURL = "https://egdata.app"
			}
		}

		var gameTitle string
		var platform string
		var totalSize int64 = l.BytesTransferred

		if svcName == "steam" {
			info := proxy.ResolveSteamDepot(s.db, depotID, "")
			gameTitle = info.GameTitle
			platform = info.Platform
			if info.TotalSize > 0 {
				totalSize = info.TotalSize
			}
		} else if svcName == "epic" {
			var appName string
			if matches := proxy.EpicBuildRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				appName = matches[1]
			}
			gameTitle, platform = proxy.ResolveEpicGame(s.db, appName, "")
		} else if svcName == "xbox" {
			gameTitle = "Xbox Game Package"
			platform = "Xbox / PC"
		} else {
			gameTitle = proxy.ResolveGameTitle(s.db, svcName, depotID, l.Path)
			platform = proxy.FormatPlatform("", "", "")
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

func (s *Server) bundleLogsIntoSessions(logs []db.RequestLog) []*proxy.GameDownloadSession {
	sessionsMap := make(map[string]*proxy.GameDownloadSession)

	for _, l := range logs {
		svc := s.registry.Identify(l.Host)
		if svc == nil {
			continue
		}
		svcName := svc.Name()

		var depotID int64
		var chunkID string
		var lookupURL string

		if svcName == "steam" {
			if matches := proxy.DepotRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				depotID, _ = strconv.ParseInt(matches[1], 10, 64)
			}
			if depotID > 0 {
				chunkID = fmt.Sprintf("%d", depotID)
				lookupURL = fmt.Sprintf("https://steamdb.info/depot/%d", depotID)
			}
		} else if svcName == "epic" {
			if matches := proxy.EpicChunkRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				chunkID = matches[1]
			}
			var appName string
			if matches := proxy.EpicBuildRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				appName = matches[1]
			}
			if appName != "" {
				lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(appName))
			} else if chunkID != "" {
				lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(chunkID))
			} else {
				lookupURL = "https://egdata.app"
			}
		}

		clientIP := l.ClientIP
		if ip, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = ip
		}
		clientIP = strings.TrimPrefix(clientIP, "[")
		clientIP = strings.TrimSuffix(clientIP, "]")

		var gameTitle string
		var platform string
		var baseAppID int64

		if svcName == "steam" {
			info := proxy.ResolveSteamDepot(s.db, depotID, "")
			gameTitle = info.GameTitle
			platform = info.Platform
			baseAppID = info.AppID
		} else if svcName == "epic" {
			var appName string
			if matches := proxy.EpicBuildRegex.FindStringSubmatch(l.Path); len(matches) > 1 {
				appName = matches[1]
			}
			gameTitle, platform = proxy.ResolveEpicGame(s.db, appName, "")
		} else if svcName == "xbox" {
			gameTitle = "Xbox Game Package"
			platform = "Xbox / PC"
		} else {
			gameTitle = proxy.ResolveGameTitle(s.db, svcName, depotID, l.Path)
			platform = proxy.FormatPlatform("", "", "")
		}

		var sessionKey string
		if svcName == "steam" && baseAppID > 0 {
			sessionKey = fmt.Sprintf("%s:steam:%d", clientIP, baseAppID)
		} else if svcName == "epic" {
			sessionKey = fmt.Sprintf("%s:epic:%s", clientIP, strings.ToLower(gameTitle))
		} else {
			sessionKey = fmt.Sprintf("%s:%s:%s", clientIP, svcName, strings.ToLower(gameTitle))
		}

		session, exists := sessionsMap[sessionKey]
		if !exists {
			session = &proxy.GameDownloadSession{
				Key:              sessionKey,
				ClientIP:         clientIP,
				Store:            proxy.FormatStoreName(svcName),
				Service:          svcName,
				GameTitle:        gameTitle,
				AppID:            baseAppID,
				Platform:         platform,
				Depots:           []int64{},
				DepotLinks:       []proxy.DepotLinkInfo{},
				BytesTransferred: 0,
				TotalDepotSize:   0,
				ChunkCount:       0,
				FirstSeen:        l.CreatedAt,
				LastActive:       l.CreatedAt,
				Timestamp:        l.CreatedAt.Format("15:04:05"),
			}
			sessionsMap[sessionKey] = session
		}

		// Update title / platform if improved
		if (strings.HasPrefix(session.GameTitle, "Steam App ") || session.GameTitle == "Steam Game Client" || session.GameTitle == "") && gameTitle != "" && !strings.HasPrefix(gameTitle, "Steam App ") {
			session.GameTitle = gameTitle
		}
		if platform != "" && platform != "PC" {
			session.Platform = platform
		}

		if l.CreatedAt.After(session.LastActive) {
			session.LastActive = l.CreatedAt
			session.Timestamp = l.CreatedAt.Format("15:04:05")
		}

		if depotID > 0 {
			hasDepot := false
			for _, d := range session.Depots {
				if d == depotID {
					hasDepot = true
					break
				}
			}
			if !hasDepot {
				session.Depots = append(session.Depots, depotID)
				session.DepotLinks = append(session.DepotLinks, proxy.DepotLinkInfo{
					DepotID:   depotID,
					LookupURL: lookupURL,
				})
			}
		} else if svcName == "epic" && lookupURL != "" && len(session.DepotLinks) == 0 {
			session.DepotLinks = append(session.DepotLinks, proxy.DepotLinkInfo{
				ChunkID:   chunkID,
				LookupURL: lookupURL,
			})
		}

		session.BytesTransferred += l.BytesTransferred
		session.ChunkCount++
		if l.IsHit {
			session.Hits++
		} else {
			session.Misses++
		}

		var combinedDepotSize int64
		for _, d := range session.Depots {
			info := proxy.ResolveSteamDepot(s.db, d, "")
			if info.TotalSize > 0 {
				combinedDepotSize += info.TotalSize
			}
		}
		if combinedDepotSize > session.BytesTransferred {
			session.TotalDepotSize = combinedDepotSize
		} else {
			session.TotalDepotSize = session.BytesTransferred
		}
	}

	result := make([]*proxy.GameDownloadSession, 0, len(sessionsMap))
	for _, sess := range sessionsMap {
		result = append(result, sess)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastActive.After(result[j].LastActive)
	})
	return result
}


// ServiceItem represents metadata for an integrated or planned game service
type ServiceItem struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	Company          string   `json:"company"`
	Icon             string   `json:"icon"`
	Status           string   `json:"status"` // "connected", "ready", "disabled", "planned"
	InterceptEnabled bool     `json:"intercept_enabled"`
	AuthSupported    bool     `json:"auth_supported"`
	PrefillSupported bool     `json:"prefill_supported"`
	Username         string   `json:"username,omitempty"`
	Domains          []string `json:"domains"`
	Description      string   `json:"description"`
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	steamStatus := s.steam.Status()
	servicesList := []ServiceItem{
		{
			ID:               "steam",
			DisplayName:      "Steam",
			Company:          "Valve",
			Icon:             "🎮",
			Status:           string(steamStatus.State),
			InterceptEnabled: s.registry.IsEnabled("steam"),
			AuthSupported:    true,
			PrefillSupported: true,
			Username:         steamStatus.Username,
			Domains: []string{
				"*.cs.steampowered.com",
				"*.content.steampowered.com",
				"*.hsar.steampowered.com",
				"content*.steampowered.com",
				"*.steamcontent.com",
			},
			Description: "Depot manifest decryption, PICS change polling, and high-throughput chunk caching.",
		},
		{
			ID:               "epic",
			DisplayName:      "Epic Games Store",
			Company:          "Epic Games",
			Icon:             "⚡",
			Status:           "ready",
			InterceptEnabled: s.registry.IsEnabled("epic"),
			AuthSupported:    false,
			PrefillSupported: false,
			Domains: []string{
				"*.download.epicgames.com",
				"*.ol.epicgames.com",
			},
			Description: "Chunked binary manifest (.manifest) and hash-addressed .chunk pass-through caching.",
		},
		{
			ID:               "blizzard",
			DisplayName:      "Battle.net",
			Company:          "Blizzard Entertainment",
			Icon:             "❄️",
			Status:           "ready",
			InterceptEnabled: s.registry.IsEnabled("blizzard"),
			AuthSupported:    false,
			PrefillSupported: false,
			Domains: []string{
				"*.dist.blizzard.com",
				"*.cdn.blizzard.com",
				"level3.blizzard.com",
			},
			Description: "Battle.net TACT archive format and CDN edge server interception.",
		},
		{
			ID:               "riot",
			DisplayName:      "Riot Games",
			Company:          "Riot Games",
			Icon:             "⚔️",
			Status:           "ready",
			InterceptEnabled: s.registry.IsEnabled("riot"),
			AuthSupported:    false,
			PrefillSupported: false,
			Domains: []string{
				"*.dyn.riotcdn.net",
				"*.content.riotgames.com",
			},
			Description: "Valorant & League of Legends RMAN release manifests and client asset caching.",
		},
		{
			ID:               "xbox",
			DisplayName:      "Xbox / Windows Store",
			Company:          "Microsoft",
			Icon:             "🟢",
			Status:           "ready",
			InterceptEnabled: s.registry.IsEnabled("xbox"),
			AuthSupported:    false,
			PrefillSupported: false,
			Domains: []string{
				"*.assets1.xboxlive.com",
				"*.delivery.mp.microsoft.com",
			},
			Description: "Delivery Optimization range-request caching for Xbox game downloads.",
		},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": servicesList,
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

// Phase 3: Steam Authentication Handlers
func (s *Server) handleSteamStatus(w http.ResponseWriter, r *http.Request) {
	status := s.steam.Status()
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleSteamLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid credentials", http.StatusBadRequest)
		return
	}

	if err := s.steam.Login(req.Username, req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Connecting to Steam Connection Manager...",
		"status":  s.steam.Status(),
	})
}

func (s *Server) handleSteam2FA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid code payload", http.StatusBadRequest)
		return
	}

	if err := s.steam.Submit2FA(req.Code); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Steam Guard authentication successful",
		"status":  s.steam.Status(),
	})
}

func (s *Server) handleSteamLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = s.steam.Logout()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Steam session cleared",
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
		// If empty, return standard popular games
		if len(apps) == 0 {
			apps = []db.WatchedApp{
				{AppID: 730, Name: "Counter-Strike 2", Service: "steam", AutoUpdate: true, Status: "synced"},
				{AppID: 570, Name: "Dota 2", Service: "steam", AutoUpdate: true, Status: "synced"},
				{AppID: 440, Name: "Team Fortress 2", Service: "steam", AutoUpdate: true, Status: "synced"},
				{AppID: 1091500, Name: "Cyberpunk 2077", Service: "steam", AutoUpdate: true, Status: "synced"},
			}
			for _, a := range apps {
				_ = s.db.UpsertWatchedApp(a)
			}
		}
		writeJSON(w, http.StatusOK, apps)
	case http.MethodPost:
		var req struct {
			AppID      int64  `json:"app_id"`
			Name       string `json:"name"`
			AutoUpdate bool   `json:"auto_update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		app := db.WatchedApp{
			AppID:      req.AppID,
			Service:    "steam",
			Name:       req.Name,
			AutoUpdate: req.AutoUpdate,
			Status:     "synced",
		}
		_ = s.db.UpsertWatchedApp(app)
		writeJSON(w, http.StatusCreated, app)
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

	job, err := s.prefill.StartJob(r.Context(), req.Service, req.AppID, req.AppName)
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

func (s *Server) handlePICSPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go s.scheduler.PollPICS()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "PICS change polling dispatched",
	})
}

func (s *Server) handlePICSTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appIDStr := r.URL.Query().Get("app_id")
	appID, _ := strconv.ParseInt(appIDStr, 10, 64)
	if appID == 0 {
		appID = 730 // Default to CS2
	}

	if err := s.scheduler.TriggerPICSUpdate(appID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("Triggered PICS update detection and prefill for App ID %d", appID),
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
