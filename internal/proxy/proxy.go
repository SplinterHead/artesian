package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/manifest"
	"artesian/internal/services"
	"artesian/internal/ws"
)

// DepotLinkInfo represents a depot with its external database inspection URL
type DepotLinkInfo struct {
	DepotID   int64  `json:"depot_id,omitempty"`
	ChunkID   string `json:"chunk_id,omitempty"`
	LookupURL string `json:"lookup_url"`
}

// GameDownloadSession represents an active game download session bundled across all depots and files
type GameDownloadSession struct {
	Key              string          `json:"key"`                // client_ip:store:game_key
	ClientIP         string          `json:"client_ip"`          // e.g. "192.168.65.1"
	Store            string          `json:"store"`              // e.g. "Steam", "Epic Games"
	Service          string          `json:"service"`            // e.g. "steam", "epic"
	GameTitle        string          `json:"game_title"`         // e.g. "Game Dev Tycoon", "Counter-Strike 2"
	AppID            int64           `json:"app_id,omitempty"`   // Steam App ID
	Platform         string          `json:"platform"`           // "PC", "Xbox / PC"
	Depots           []int64         `json:"depots"`             // Unique depot IDs downloaded for this game
	DepotLinks       []DepotLinkInfo `json:"depot_links"`        // SteamDB / EGData links for each depot
	BytesTransferred int64           `json:"bytes_transferred"`  // Sum of all chunk bytes transferred
	TotalDepotSize   int64           `json:"total_depot_size"`   // Full combined depot size across all depots
	ChunkCount       int64           `json:"chunk_count"`        // Total files/chunks downloaded
	Hits             int64           `json:"hits"`
	Misses           int64           `json:"misses"`
	FirstSeen        time.Time       `json:"first_seen"`
	LastActive       time.Time       `json:"last_active"`
	Timestamp        string          `json:"timestamp"`          // Initial chunk timestamp "15:04:05"
}

// TrafficEvent is streamed via WebSocket to the frontend in real time
type TrafficEvent struct {
	Timestamp        string `json:"timestamp"`
	ClientIP         string `json:"client_ip"`
	Host             string `json:"host"`
	Method           string `json:"method"`
	URL              string `json:"url"`
	BytesTransferred int64  `json:"bytes_transferred"`
	IsHit            bool   `json:"is_hit"`
	Status           int    `json:"status"`
	DurationMs       int64  `json:"duration_ms"`
	Service          string `json:"service"`

	// Enriched fields for Active Downloads panel
	Store          string               `json:"store"`             // "Steam", "Epic Games", "Battle.net", "Riot Games", "Xbox"
	GameTitle      string               `json:"game_title"`        // e.g. "Counter-Strike 2", "Fortnite"
	Platform       string               `json:"platform"`          // e.g. "PC"
	DepotID        int64                `json:"depot_id,omitempty"` // e.g. 731
	ChunkID        string               `json:"chunk_id,omitempty"` // e.g. "chunk_01" or "1a9ae1fc921de40b"
	LookupURL      string               `json:"lookup_url,omitempty"` // steamdb or egdata link!
	TotalDepotSize int64                `json:"total_depot_size"`  // Total bytes for this depot or game
	Session        *GameDownloadSession `json:"session,omitempty"` // Unified game download session
}

type dbLogEntry struct {
	clientIP string
	host     string
	method   string
	path     string
	status   int
	isHit    bool
	bytes    int64
	duration int64
}

type proxyReqContextKey struct{}

type proxyContextData struct {
	service     services.Service
	serviceName string
	cacheKey    string
	canCache    bool
	start       time.Time
}

// DepotRegex extracts depot IDs from game CDN paths
var DepotRegex = regexp.MustCompile(`/depot/(\d+)`)

// EpicBuildRegex extracts app names from Epic CDN paths (e.g. /Builds/Fortnite/...)
var EpicBuildRegex = regexp.MustCompile(`/Builds/([^/]+)`)

// EpicChunkRegex extracts chunk and manifest file names from Epic CDN paths
var EpicChunkRegex = regexp.MustCompile(`([^/]+)\.(chunk|manifest)`)

// FormatStoreName normalizes internal service names to display store titles
func FormatStoreName(service string) string {
	switch strings.ToLower(service) {
	case "steam":
		return "Steam"
	case "epic":
		return "Epic Games"
	case "blizzard":
		return "Battle.net"
	case "riot":
		return "Riot Games"
	case "xbox":
		return "Xbox"
	default:
		return strings.Title(service)
	}
}


// responseObserver wraps http.ResponseWriter to capture response status, bytes, and support Flusher
type responseObserver struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (o *responseObserver) WriteHeader(code int) {
	o.statusCode = code
	o.ResponseWriter.WriteHeader(code)
}

func (o *responseObserver) Write(b []byte) (int, error) {
	if o.statusCode == 0 {
		o.statusCode = http.StatusOK
	}
	n, err := o.ResponseWriter.Write(b)
	o.bytesWritten += int64(n)
	return n, err
}

func (o *responseObserver) Flush() {
	if f, ok := o.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Server implements the HTTP caching reverse proxy
type Server struct {
	cfg          config.ProxyConfig
	dnsCfg       config.DNSConfig
	cache        *cache.Storage
	db           *db.DB
	registry     *services.Registry
	httpServer   *http.Server
	transport    *http.Transport
	reverseProxy *httputil.ReverseProxy

	// WebSocket broadcast rate limiting (Item E)
	wsMu       sync.Mutex
	wsLastSent time.Time

	// Real-time bundled game download sessions
	sessionsMu      sync.RWMutex
	activeDownloads map[string]*GameDownloadSession

	// Async DB logging queue
	logQueue chan dbLogEntry
	closeCh  chan struct{}
	wg       sync.WaitGroup
}

// isSelfOrLoopback checks if a host header targets Artesian itself
func isSelfOrLoopback(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if idx := strings.Index(h, ":"); idx != -1 {
		h = h[:idx]
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0" || h == "artesian" || h == "" || h == "lancache.steamcontent.com"
}

// NewServer initializes the proxy engine
func NewServer(cfg config.ProxyConfig, dnsCfg config.DNSConfig, c *cache.Storage, database *db.DB, registry *services.Registry) *Server {
	maxConns := cfg.MaxConnsPerHost
	if maxConns <= 0 {
		maxConns = 500
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Route proxy DNS lookups to real upstream DNS to avoid local Docker loopback interception
	if len(dnsCfg.UpstreamDNS) > 0 {
		upstreamAddr := dnsCfg.UpstreamDNS[0]
		if !strings.Contains(upstreamAddr, ":") {
			upstreamAddr += ":53"
		}
		dialer.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", upstreamAddr)
			},
		}
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          2000,
		MaxIdleConnsPerHost:   500,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true, // Critical: preserves byte-range chunks without decompressing
	}

	s := &Server{
		cfg:             cfg,
		dnsCfg:          dnsCfg,
		cache:           c,
		db:              database,
		registry:        registry,
		transport:       transport,
		activeDownloads: make(map[string]*GameDownloadSession),
		logQueue:        make(chan dbLogEntry, 5000),
		closeCh:         make(chan struct{}),
	}


	// Singleton reusable reverse proxy with pooled transport
	s.reverseProxy = &httputil.ReverseProxy{
		Transport: transport,
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = "http"
			outReq.URL.Host = outReq.Host
			outReq.Header.Set("X-Artesian-Loop", "1")
			outReq.Header.Set("Via", "1.1 artesian")
			if clientIP := outReq.RemoteAddr; clientIP != "" {
				if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
					outReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
				} else {
					outReq.Header.Set("X-Forwarded-For", clientIP)
				}
			}
		},
		ModifyResponse: func(res *http.Response) error {
			ctxData, ok := res.Request.Context().Value(proxyReqContextKey{}).(*proxyContextData)
			if !ok || ctxData == nil {
				return nil
			}

			res.Header.Set("X-LanCache-Processed-By", "artesian")
			if ctxData.service != nil {
				res.Header.Set("X-Artesian-Cache", "MISS")
				res.Header.Set("X-Artesian-Service", ctxData.serviceName)
			}

			if ctxData.canCache && res.StatusCode == http.StatusOK {
				bodyBytes, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err == nil {
					res.Body = io.NopCloser(bytes.NewReader(bodyBytes))

					// Async write to tank
					go func(svcName, cKey string, data []byte) {
						destPath, written, err := s.cache.Store(svcName, cKey, bytes.NewReader(data))
						if err == nil && s.db != nil {
							// 1. Steam chunk caching
							depotID, chunkHash, ok := cache.ParseSteamChunkExtract(cKey)
							if ok {
								_ = s.db.RecordChunk(chunkHash, svcName, depotID, 0, written, destPath)
							}

							// 2. Steam manifest interception and automatic chunk indexing
							if mDepotID, mID, isManifest := manifest.ParseManifestDepotFromURL(cKey); isManifest {
								storageDir := "/data/cache"
								if s.cache != nil {
									storageDir = s.cache.GetBaseDir()
								}
								manifest.SaveRawManifest(storageDir, mDepotID, mID, data)
								parsedChunks, pErr := manifest.ParseManifestBytes(data)
								if pErr == nil && len(parsedChunks) > 0 {
									log.Printf("[Artesian Proxy] Intercepted and indexed %d genuine chunks for depot %d (manifest %d)", len(parsedChunks), mDepotID, mID)
									_ = s.db.SaveManifestChunks(mDepotID, mID, parsedChunks)
								} else if pErr != nil {
									log.Printf("[Artesian Proxy] Warning: Could not parse intercepted manifest for depot %d: %v", mDepotID, pErr)
								}
							}
						}
					}(ctxData.serviceName, ctxData.cacheKey, bodyBytes)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			log.Printf("[Artesian Proxy] Upstream proxy error for %s: %v", req.Host, err)
			http.Error(w, fmt.Sprintf("Artesian upstream error: %v", err), http.StatusBadGateway)
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleProxy)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start background async SQLite log worker
	s.wg.Add(1)
	go s.processLogQueue()

	// Start background session cleanup ticker
	s.wg.Add(1)
	go s.cleanupInactiveDownloads()

	return s
}

// Start runs the proxy listener
func (s *Server) Start() error {
	log.Printf("[Artesian Proxy] Core proxy engine online on %s", s.cfg.ListenAddr)
	return s.httpServer.ListenAndServe()
}

// Close gracefully stops the proxy
func (s *Server) Close() error {
	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
	s.wg.Wait()
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	return s.httpServer.Close()
}

func (s *Server) handleProxy(w http.ResponseWriter, req *http.Request) {
	// 1. Loop and internal prefill prevention check
	if req.Header.Get("X-Artesian-Loop") != "" || req.Header.Get("X-Artesian-Prefill") != "" {
		http.Error(w, "Proxy loop detected: request forwarded back to Artesian", http.StatusLoopDetected)
		return
	}

	// 2. Self / Localhost probe protection
	if isSelfOrLoopback(req.Host) {
		w.Header().Set("X-LanCache-Processed-By", "artesian")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Artesian Unified Game Content Cache Proxy\nProxy active on :80\nWeb UI active at http://localhost:8080\n"))
		return
	}

	start := time.Now()
	service := s.registry.Identify(req.Host)
	cacheKey := req.URL.RequestURI()
	serviceName := "generic"
	if service != nil {
		serviceName = service.Name()
	}

	// Cache GET and HEAD requests for identified services only according to platform rules
	canCache := service != nil && service.IsCacheable(req)

	// Phase 2: Check SQLite and disk storage
	if canCache && s.cache.Has(serviceName, cacheKey) {
		file, size, err := s.cache.Open(serviceName, cacheKey)
		if err == nil {
			defer file.Close()

			w.Header().Set("X-LanCache-Processed-By", "artesian")
			w.Header().Set("X-Artesian-Cache", "HIT")
			w.Header().Set("X-Artesian-Service", serviceName)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
			w.WriteHeader(http.StatusOK)

			if req.Method == http.MethodGet {
				_, _ = io.Copy(w, file)
			}

			duration := time.Since(start).Milliseconds()

			// Increment hit in SQLite
			if depotID, chunkHash, ok := cache.ParseSteamChunkExtract(cacheKey); ok && s.db != nil {
				_ = depotID
				s.db.IncrementChunkHit(chunkHash)
			}

			// ONLY log & broadcast if service is one of our tracked CDNs
			if service != nil {
				s.publishTraffic(req.RemoteAddr, req.Host, req.Method, req.URL.RequestURI(), req.Header.Get("User-Agent"), http.StatusOK, true, size, duration, serviceName)
			}
			return
		}
	}

	// Pass-Through via reusable reverse proxy
	s.passThrough(w, req, service, serviceName, cacheKey, start, canCache)
}

func (s *Server) passThrough(w http.ResponseWriter, req *http.Request, svc services.Service, serviceName, cacheKey string, start time.Time, canCache bool) {
	ctxData := &proxyContextData{
		service:     svc,
		serviceName: serviceName,
		cacheKey:    cacheKey,
		canCache:    canCache,
		start:       start,
	}

	reqWithCtx := req.WithContext(context.WithValue(req.Context(), proxyReqContextKey{}, ctxData))

	obs := &responseObserver{ResponseWriter: w}
	s.reverseProxy.ServeHTTP(obs, reqWithCtx)

	duration := time.Since(start).Milliseconds()
	statusCode := obs.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	// User requirement: ONLY log and broadcast if request matches one of our tracked CDNs (svc != nil)
	if svc != nil {
		s.publishTraffic(req.RemoteAddr, req.Host, req.Method, req.URL.RequestURI(), req.Header.Get("User-Agent"), statusCode, false, obs.bytesWritten, duration, serviceName)
	}
}

// ResolveSteamBaseAppID finds the base App ID from a depot ID
func ResolveSteamBaseAppID(depotID int64) int64 {
	if depotID <= 0 {
		return 0
	}
	if depotID%10 == 0 {
		return depotID
	}
	return depotID - (depotID % 10)
}

// ResolveGameTitle attempts to identify a user-friendly game title from depot ID, app names, and store
func ResolveGameTitle(database *db.DB, service string, depotID int64, uri string) string {
	if service == "steam" {
		if depotID > 0 {
			info := ResolveSteamDepot(database, depotID, "")
			return info.GameTitle
		}
		return "Steam Game Client"
	}
	if service == "epic" {
		var appName string
		if matches := EpicBuildRegex.FindStringSubmatch(uri); len(matches) > 1 {
			appName = matches[1]
		}
		title, _ := ResolveEpicGame(database, appName, "")
		return title
	}
	if service == "blizzard" {
		return "Battle.net Game"
	}
	if service == "riot" {
		return "Valorant / Riot Game"
	}
	if service == "xbox" {
		return "Xbox Game Package"
	}
	return "Game Asset"
}

// RecordGameTraffic bundles all chunk requests into a single unified session per game and client IP
func (s *Server) RecordGameTraffic(clientIP, service, gameTitle, platform, uri string, depotID int64, baseAppID int64, chunkID, lookupURL string, bytesCount int64, isHit bool) *GameDownloadSession {
	if service == "steam" && baseAppID <= 0 && depotID > 0 {
		baseAppID = ResolveSteamBaseAppID(depotID)
	}

	var sessionKey string
	if service == "steam" && baseAppID > 0 {
		sessionKey = fmt.Sprintf("%s:steam:%d", clientIP, baseAppID)
	} else if service == "epic" {
		sessionKey = fmt.Sprintf("%s:epic:%s", clientIP, strings.ToLower(gameTitle))
	} else {
		sessionKey = fmt.Sprintf("%s:%s:%s", clientIP, service, strings.ToLower(gameTitle))
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	session, exists := s.activeDownloads[sessionKey]
	now := time.Now()
	nowStr := now.Format("15:04:05")

	if !exists {
		session = &GameDownloadSession{
			Key:              sessionKey,
			ClientIP:         clientIP,
			Store:            FormatStoreName(service),
			Service:          service,
			GameTitle:        gameTitle,
			AppID:            baseAppID,
			Platform:         platform,
			Depots:           []int64{},
			DepotLinks:       []DepotLinkInfo{},
			BytesTransferred: 0,
			TotalDepotSize:   0,
			ChunkCount:       0,
			FirstSeen:        now,
			LastActive:       now,
			Timestamp:        nowStr,
		}
		s.activeDownloads[sessionKey] = session
	}

	// Update game title if improved (e.g. from generic "Steam App 239820" to "Game Dev Tycoon")
	if (strings.HasPrefix(session.GameTitle, "Steam App ") || session.GameTitle == "Steam Game Client" || session.GameTitle == "") && gameTitle != "" && !strings.HasPrefix(gameTitle, "Steam App ") {
		session.GameTitle = gameTitle
	}

	if platform != "" && platform != "PC" {
		session.Platform = platform
	}

	if session.AppID <= 0 && baseAppID > 0 {
		session.AppID = baseAppID
	}

	// Track unique depots for this game
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
			sort.Slice(session.Depots, func(i, j int) bool { return session.Depots[i] < session.Depots[j] })
			session.DepotLinks = append(session.DepotLinks, DepotLinkInfo{
				DepotID:   depotID,
				LookupURL: lookupURL,
			})
		}
	} else if service == "epic" && lookupURL != "" {
		if len(session.DepotLinks) == 0 {
			session.DepotLinks = append(session.DepotLinks, DepotLinkInfo{
				ChunkID:   chunkID,
				LookupURL: lookupURL,
			})
		}
	}

	session.LastActive = now
	if session.Timestamp == "" {
		session.Timestamp = nowStr
	}
	session.BytesTransferred += bytesCount
	session.ChunkCount++
	if isHit {
		session.Hits++
	} else {
		session.Misses++
	}

	// Bundle size calculation: dynamically looks up size for all depots in this session
	var combinedDepotSize int64
	for _, d := range session.Depots {
		info := ResolveSteamDepot(s.db, d, "")
		if info.TotalSize > 0 {
			combinedDepotSize += info.TotalSize
		}
	}
	if combinedDepotSize > 0 {
		session.TotalDepotSize = combinedDepotSize
	}

	sessionCopy := *session
	sessionCopy.Depots = append([]int64(nil), session.Depots...)
	sessionCopy.DepotLinks = append([]DepotLinkInfo(nil), session.DepotLinks...)
	return &sessionCopy
}

// RecordPrefillProgress logs prefill download progress and updates active downloads table
func (s *Server) RecordPrefillProgress(clientIP, service, gameTitle, platform string, depotID int64, appID int64, chunkID string, bytesTransferred, totalBytes int64) {
	if clientIP == "" {
		clientIP = "Artesian"
	}
	if service == "" {
		service = "steam"
	}
	store := FormatStoreName(service)
	sessionKey := fmt.Sprintf("%s:%s:%d", clientIP, service, appID)

	s.sessionsMu.Lock()
	now := time.Now()
	nowStr := now.Format("15:04:05")

	session, exists := s.activeDownloads[sessionKey]
	if !exists {
		session = &GameDownloadSession{
			Key:              sessionKey,
			ClientIP:         clientIP,
			Store:            store,
			Service:          service,
			GameTitle:        gameTitle,
			AppID:            appID,
			Platform:         platform,
			Depots:           []int64{depotID},
			DepotLinks:       []DepotLinkInfo{{DepotID: depotID, LookupURL: fmt.Sprintf("https://steamdb.info/depot/%d", depotID)}},
			BytesTransferred: bytesTransferred,
			TotalDepotSize:   totalBytes,
			ChunkCount:       1,
			FirstSeen:        now,
			LastActive:       now,
			Timestamp:        nowStr,
		}
		s.activeDownloads[sessionKey] = session
	} else {
		session.LastActive = now
		if session.Timestamp == "" {
			session.Timestamp = nowStr
		}
		session.BytesTransferred = bytesTransferred
		if totalBytes > 0 {
			session.TotalDepotSize = totalBytes
		}
		session.ChunkCount++
		if gameTitle != "" && !strings.HasPrefix(gameTitle, "Steam App ") {
			session.GameTitle = gameTitle
		}
		if platform != "" && platform != "PC" {
			session.Platform = platform
		}
		hasDepot := false
		for _, d := range session.Depots {
			if d == depotID {
				hasDepot = true
				break
			}
		}
		if !hasDepot && depotID > 0 {
			session.Depots = append(session.Depots, depotID)
			session.DepotLinks = append(session.DepotLinks, DepotLinkInfo{
				DepotID:   depotID,
				LookupURL: fmt.Sprintf("https://steamdb.info/depot/%d", depotID),
			})
		}
	}

	sessionCopy := *session
	sessionCopy.Depots = append([]int64(nil), session.Depots...)
	sessionCopy.DepotLinks = append([]DepotLinkInfo(nil), session.DepotLinks...)
	s.sessionsMu.Unlock()

	ws.GetHub().Broadcast("proxy:download_update", &sessionCopy)
}

// CompletePrefillSession marks the prefill session as finished so it is cleanly pruned after grace period
func (s *Server) CompletePrefillSession(clientIP, service string, appID int64) {
	if clientIP == "" {
		clientIP = "Artesian"
	}
	sessionKey := fmt.Sprintf("%s:%s:%d", clientIP, service, appID)

	s.sessionsMu.Lock()
	if sess, ok := s.activeDownloads[sessionKey]; ok {
		sess.BytesTransferred = sess.TotalDepotSize
		sess.LastActive = time.Now()
		sessionCopy := *sess
		sessionCopy.Depots = append([]int64(nil), sess.Depots...)
		sessionCopy.DepotLinks = append([]DepotLinkInfo(nil), sess.DepotLinks...)
		s.sessionsMu.Unlock()
		ws.GetHub().Broadcast("proxy:download_update", &sessionCopy)
		return
	}
	s.sessionsMu.Unlock()
}

// cleanupInactiveDownloads periodically checks for downloads that finished or stopped
func (s *Server) cleanupInactiveDownloads() {
	defer s.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return
		case <-ticker.C:
			s.pruneInactiveSessions()
		}
	}
}

// pruneInactiveSessions removes downloads that have completed, cancelled, or disconnected
func (s *Server) pruneInactiveSessions() {
	s.sessionsMu.Lock()
	now := time.Now()
	var removedKeys []string

	for key, sess := range s.activeDownloads {
		isComplete := sess.TotalDepotSize > 0 && sess.BytesTransferred >= sess.TotalDepotSize
		// If download completed (transferred all bytes), remove after 3s grace period
		if isComplete && now.Sub(sess.LastActive) >= 3*time.Second {
			delete(s.activeDownloads, key)
			removedKeys = append(removedKeys, key)
			continue
		}
		// If download stopped/cancelled/disconnected (no chunks in 12s), remove it
		if now.Sub(sess.LastActive) >= 12*time.Second {
			delete(s.activeDownloads, key)
			removedKeys = append(removedKeys, key)
			continue
		}
	}
	s.sessionsMu.Unlock()

	for _, key := range removedKeys {
		ws.GetHub().Broadcast("proxy:download_removed", map[string]string{
			"key": key,
		})
	}
}

// GetActiveDownloads returns only currently ongoing game download sessions sorted by LastActive descending
func (s *Server) GetActiveDownloads() []*GameDownloadSession {
	s.sessionsMu.Lock()
	now := time.Now()
	for key, sess := range s.activeDownloads {
		isComplete := sess.TotalDepotSize > 0 && sess.BytesTransferred >= sess.TotalDepotSize
		if (isComplete && now.Sub(sess.LastActive) >= 3*time.Second) || now.Sub(sess.LastActive) >= 12*time.Second {
			delete(s.activeDownloads, key)
		}
	}
	result := make([]*GameDownloadSession, 0, len(s.activeDownloads))
	for _, sess := range s.activeDownloads {
		cp := *sess
		cp.Depots = append([]int64(nil), sess.Depots...)
		cp.DepotLinks = append([]DepotLinkInfo(nil), sess.DepotLinks...)
		result = append(result, &cp)
	}
	s.sessionsMu.Unlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].FirstSeen.After(result[j].FirstSeen)
	})

	return result
}

// ClearActiveDownloads flushes all in-memory active download sessions
func (s *Server) ClearActiveDownloads() {
	s.sessionsMu.Lock()
	s.activeDownloads = make(map[string]*GameDownloadSession)
	s.sessionsMu.Unlock()
	ws.GetHub().Broadcast("proxy:downloads_cleared", map[string]interface{}{})
}

// CancelActiveDownloadsForApp removes any active download sessions for a given appID and broadcasts removal
func (s *Server) CancelActiveDownloadsForApp(service string, appID int64) {
	s.sessionsMu.Lock()
	var removedKeys []string
	for key, sess := range s.activeDownloads {
		if (service == "" || strings.EqualFold(sess.Service, service)) && sess.AppID == appID {
			delete(s.activeDownloads, key)
			removedKeys = append(removedKeys, key)
		}
	}
	s.sessionsMu.Unlock()

	for _, key := range removedKeys {
		ws.GetHub().Broadcast("proxy:download_removed", map[string]string{
			"key": key,
		})
	}
}

func (s *Server) publishTraffic(rawClientIP, host, method, uri, userAgent string, status int, isHit bool, bytesCount, duration int64, service string) {
	// Drop port from client IP
	clientIP := rawClientIP
	if ip, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = ip
	}
	clientIP = strings.TrimPrefix(clientIP, "[")
	clientIP = strings.TrimSuffix(clientIP, "]")

	// Never publish internal container loopback traffic as client downloads
	if clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" {
		return
	}

	store := FormatStoreName(service)

	var depotID int64
	var chunkID string
	var lookupURL string
	var gameTitle string
	var platform string
	var baseAppID int64

	if service == "steam" {
		if matches := DepotRegex.FindStringSubmatch(uri); len(matches) > 1 {
			depotID, _ = strconv.ParseInt(matches[1], 10, 64)
		}
		if depotID > 0 {
			chunkID = fmt.Sprintf("%d", depotID)
			lookupURL = fmt.Sprintf("https://steamdb.info/depot/%d", depotID)
			info := ResolveSteamDepot(s.db, depotID, userAgent)
			gameTitle = info.GameTitle
			platform = info.Platform
			baseAppID = info.AppID
		} else {
			gameTitle = "Steam Game Client"
			platform = FormatPlatform("", "", userAgent)
		}
	} else if service == "epic" {
		if matches := EpicChunkRegex.FindStringSubmatch(uri); len(matches) > 1 {
			chunkID = matches[1]
		}
		var appName string
		if matches := EpicBuildRegex.FindStringSubmatch(uri); len(matches) > 1 {
			appName = matches[1]
		}
		if appName != "" {
			lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(appName))
		} else if chunkID != "" {
			lookupURL = fmt.Sprintf("https://egdata.app/search?title=%s", url.QueryEscape(chunkID))
		} else {
			lookupURL = "https://egdata.app"
		}
		gameTitle, platform = ResolveEpicGame(s.db, appName, userAgent)
	} else if service == "blizzard" {
		gameTitle = "Battle.net Game"
		platform = FormatPlatform("", "", userAgent)
	} else if service == "riot" {
		gameTitle = "Valorant / Riot Game"
		platform = FormatPlatform("", "", userAgent)
	} else if service == "xbox" {
		gameTitle = "Xbox Game Package"
		platform = "Xbox / PC"
	} else {
		gameTitle = "Game Asset"
		platform = FormatPlatform("", "", userAgent)
	}

	session := s.RecordGameTraffic(clientIP, service, gameTitle, platform, uri, depotID, baseAppID, chunkID, lookupURL, bytesCount, isHit)

	evt := TrafficEvent{
		Timestamp:        time.Now().Format("15:04:05"),
		ClientIP:         clientIP,
		Host:             host,
		Method:           method,
		URL:              uri,
		BytesTransferred: bytesCount,
		IsHit:            isHit,
		Status:           status,
		DurationMs:       duration,
		Service:          service,
		Store:            store,
		GameTitle:        gameTitle,
		Platform:         platform,
		DepotID:          depotID,
		ChunkID:          chunkID,
		LookupURL:        lookupURL,
		TotalDepotSize:   session.TotalDepotSize,
		Session:          session,
	}

	// 1. Throttled WebSocket broadcast (Item E: max 20 events/sec to prevent UI overload)
	s.wsMu.Lock()
	now := time.Now()
	shouldBroadcast := now.Sub(s.wsLastSent) >= 50*time.Millisecond
	if shouldBroadcast {
		s.wsLastSent = now
	}
	s.wsMu.Unlock()

	if shouldBroadcast {
		ws.GetHub().Broadcast("proxy:traffic", evt)
		ws.GetHub().Broadcast("proxy:download_update", session)
	}

	// 2. Non-blocking queue for database persistence
	if s.db != nil && s.logQueue != nil {
		select {
		case s.logQueue <- dbLogEntry{
			clientIP: clientIP,
			host:     host,
			method:   method,
			path:     uri,
			status:   status,
			isHit:    isHit,
			bytes:    bytesCount,
			duration: duration,
		}:
		default:
			// Queue full under extreme load, drop request log to keep proxy latency low
		}
	}
}

func (s *Server) processLogQueue() {
	defer s.wg.Done()
	batch := make([]dbLogEntry, 0, 100)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 || s.db == nil {
			return
		}
		s.flushLogs(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-s.closeCh:
			// Drain remaining logs on shutdown
			for {
				select {
				case entry := <-s.logQueue:
					batch = append(batch, entry)
					if len(batch) >= 100 {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case entry := <-s.logQueue:
			batch = append(batch, entry)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Server) flushLogs(entries []dbLogEntry) {
	if s.db == nil || len(entries) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		query := `INSERT INTO request_logs (client_ip, host, method, path, status, is_hit, bytes_transferred, duration_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
		for _, e := range entries {
			_, _ = s.db.Exec(query, e.clientIP, e.host, e.method, e.path, e.status, e.isHit, e.bytes, e.duration)
		}
		return
	}
	stmt, err := tx.Prepare(`
		INSERT INTO request_logs (client_ip, host, method, path, status, is_hit, bytes_transferred, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.clientIP == "127.0.0.1" || e.clientIP == "::1" || e.clientIP == "localhost" {
			continue
		}
		_, _ = stmt.Exec(e.clientIP, e.host, e.method, e.path, e.status, e.isHit, e.bytes, e.duration)
	}
	_ = tx.Commit()
}
