package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/services"
	"artesian/internal/ws"
)

// TrafficEvent is streamed via WebSocket to the frontend in real time
type TrafficEvent struct {
	ClientIP         string `json:"client_ip"`
	Host             string `json:"host"`
	Method           string `json:"method"`
	URL              string `json:"url"`
	BytesTransferred int64  `json:"bytes_transferred"`
	IsHit            bool   `json:"is_hit"`
	Status           int    `json:"status"`
	DurationMs       int64  `json:"duration_ms"`
	Service          string `json:"service"`
}

// Server implements the HTTP caching reverse proxy
type Server struct {
	cfg        config.ProxyConfig
	cache      *cache.Storage
	db         *db.DB
	registry   *services.Registry
	httpServer *http.Server
}

// NewServer initializes the proxy engine
func NewServer(cfg config.ProxyConfig, c *cache.Storage, database *db.DB, registry *services.Registry) *Server {
	s := &Server{
		cfg:      cfg,
		cache:    c,
		db:       database,
		registry: registry,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleProxy)

	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// Start runs the proxy listener
func (s *Server) Start() error {
	log.Printf("[Artesian Proxy] Core proxy engine online on %s", s.cfg.ListenAddr)
	return s.httpServer.ListenAndServe()
}

// Close gracefully stops the proxy
func (s *Server) Close() error {
	return s.httpServer.Close()
}

func (s *Server) handleProxy(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	service := s.registry.Identify(req.Host)
	cacheKey := req.URL.RequestURI()
	serviceName := "generic"
	if service != nil {
		serviceName = service.Name()
	}

	// Cache GET and HEAD requests for identified services
	canCache := service != nil && (req.Method == http.MethodGet || req.Method == http.MethodHead)

	// Phase 2: Check SQLite and disk storage
	if canCache && s.cache.Has(serviceName, cacheKey) {
		file, size, err := s.cache.Open(serviceName, cacheKey)
		if err == nil {
			defer file.Close()

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

			// Broadcast live event over WebSocket
			s.publishTraffic(req.RemoteAddr, req.Host, req.Method, req.URL.RequestURI(), 200, true, size, duration, serviceName)
			return
		}
	}

	// Phase 1 Pass-Through & Phase 2 Async Staging
	s.passThrough(w, req, service, cacheKey, start, canCache)
}

func (s *Server) passThrough(w http.ResponseWriter, req *http.Request, svc services.Service, cacheKey string, start time.Time, canCache bool) {
	serviceName := "generic"
	if svc != nil {
		serviceName = svc.Name()
	}

	director := func(outReq *http.Request) {
		outReq.URL.Scheme = "http"
		outReq.URL.Host = req.Host
		if clientIP := req.RemoteAddr; clientIP != "" {
			outReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}

	var bytesCount int64

	proxy := &httputil.ReverseProxy{
		Director: director,
		ModifyResponse: func(res *http.Response) error {
			res.Header.Set("X-Artesian-Cache", "MISS")
			res.Header.Set("X-Artesian-Service", serviceName)

			if canCache && res.StatusCode == http.StatusOK {
				bodyBytes, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err == nil {
					bytesCount = int64(len(bodyBytes))
					// Async write to tank
					go func(data []byte) {
						destPath, written, err := s.cache.Store(serviceName, cacheKey, bytes.NewReader(data))
						if err == nil && s.db != nil {
							depotID, chunkHash, ok := cache.ParseSteamChunkExtract(cacheKey)
							if ok {
								_ = s.db.RecordChunk(chunkHash, serviceName, depotID, 0, written, destPath)
							}
						}
					}(bodyBytes)

					res.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}
			} else {
				bytesCount = res.ContentLength
			}
			return nil
		},
	}

	proxy.ServeHTTP(w, req)
	duration := time.Since(start).Milliseconds()

	// Broadcast live event over WebSocket
	s.publishTraffic(req.RemoteAddr, req.Host, req.Method, req.URL.RequestURI(), 200, false, bytesCount, duration, serviceName)
}

func (s *Server) publishTraffic(clientIP, host, method, uri string, status int, isHit bool, bytesCount, duration int64, service string) {
	evt := TrafficEvent{
		ClientIP:         clientIP,
		Host:             host,
		Method:           method,
		URL:              uri,
		BytesTransferred: bytesCount,
		IsHit:            isHit,
		Status:           status,
		DurationMs:       duration,
		Service:          service,
	}

	// 1. Broadcast over WebSocket to all UI dashboards
	ws.GetHub().Broadcast("proxy:traffic", evt)

	// 2. Persist in database
	if s.db != nil {
		query := `
		INSERT INTO request_logs (client_ip, host, method, path, status, is_hit, bytes_transferred, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`
		_, _ = s.db.Exec(query, clientIP, host, method, uri, status, isHit, bytesCount, duration)
	}
}
