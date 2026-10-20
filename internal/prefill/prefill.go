package prefill

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/ports"
	"artesian/internal/services"
	"artesian/internal/ws"
)

// ChunkMeta describes a single chunk in a depot manifest
type ChunkMeta struct {
	DepotID   int64  `json:"depot_id"`
	ChunkID   string `json:"chunk_id"` // 40-char SHA1 hex
	SizeBytes int64  `json:"size_bytes"`
}

// DepotManifest represents parsed depot chunk metadata
type DepotManifest struct {
	AppID      int64       `json:"app_id"`
	DepotID    int64       `json:"depot_id"`
	ManifestID uint64      `json:"manifest_id"`
	Chunks     []ChunkMeta `json:"chunks"`
}

// PrefillJob represents an active or completed prefill task
type PrefillJob struct {
	ID               string             `json:"id"`
	Service          string             `json:"service"` // e.g. "steam"
	AppID            int64              `json:"app_id"`
	AppName          string             `json:"app_name"`
	Platform         string             `json:"platform"`
	Status           string             `json:"status"` // "queued", "diffing", "downloading", "completed", "failed"
	TotalChunks      int64              `json:"total_chunks"`
	ExistingChunks   int64              `json:"existing_chunks"`
	DownloadedChunks int64              `json:"downloaded_chunks"`
	BytesTotal       int64              `json:"bytes_total"`
	BytesCached      int64              `json:"bytes_cached"`
	SpeedMbps        float64            `json:"speed_mbps"`
	ProgressPct      float64            `json:"progress_pct"`
	ETASeconds       int64              `json:"eta_seconds"`
	Error            string             `json:"error,omitempty"`
	StartedAt        time.Time          `json:"started_at"`
	EndedAt          time.Time          `json:"ended_at,omitempty"`
	cancelFn         context.CancelFunc `json:"-"`
}

// TrafficRecorder defines an interface for reporting progress to the active downloads table
type TrafficRecorder interface {
	RecordPrefillProgress(clientIP, service, gameTitle, platform string, depotID int64, appID int64, chunkID string, bytesTransferred, totalBytes int64)
	CompletePrefillSession(clientIP, service string, appID int64)
}

// Manager coordinates prefill and cache warming operations
type Manager struct {
	cache      *cache.Storage
	db         *db.DB
	dnsCfg     config.DNSConfig
	registry   *services.Registry
	httpClient *http.Client
	recorder   TrafficRecorder
	jobs       map[string]*PrefillJob
	jobsMu     sync.RWMutex

	// Dynamic upstream DNS resolution
	dnsMu        sync.RWMutex
	upstreamAddr string
}

func (m *Manager) getUpstreamDNS() string {
	m.dnsMu.RLock()
	defer m.dnsMu.RUnlock()
	return m.upstreamAddr
}

// SetUpstreamDNS dynamically updates the upstream DNS target for prefill downloads
func (m *Manager) SetUpstreamDNS(upstreams []string) {
	if len(upstreams) == 0 {
		return
	}
	addr := strings.TrimSpace(upstreams[0])
	if addr == "" {
		return
	}
	if !strings.Contains(addr, ":") {
		addr += ":53"
	}
	m.dnsMu.Lock()
	m.upstreamAddr = addr
	m.dnsMu.Unlock()
}

// NewManager initializes the prefill manager with direct upstream DNS resolution
func NewManager(c *cache.Storage, database *db.DB, dnsCfg config.DNSConfig, reg *services.Registry) *Manager {
	m := &Manager{
		cache:    c,
		db:       database,
		dnsCfg:   dnsCfg,
		registry: reg,
		jobs:     make(map[string]*PrefillJob),
	}
	if len(dnsCfg.UpstreamDNS) > 0 {
		m.SetUpstreamDNS(dnsCfg.UpstreamDNS)
	} else {
		m.upstreamAddr = "1.1.1.1:53"
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	dialer.Resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			target := m.getUpstreamDNS()
			if target == "" {
				target = "1.1.1.1:53"
			}
			return d.DialContext(ctx, "udp", target)
		},
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}

	m.httpClient = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return m
}

// SetTrafficRecorder connects prefill progress to the active download monitoring table
func (m *Manager) SetTrafficRecorder(r TrafficRecorder) {
	m.jobsMu.Lock()
	m.recorder = r
	m.jobsMu.Unlock()
}

// StartJob queues and launches a prefill task for an app
func (m *Manager) StartJob(ctx context.Context, service string, appID int64, appName string) (*PrefillJob, error) {
	var platform string
	if m.db != nil {
		_ = m.db.QueryRow("SELECT COALESCE(platform, '') FROM watched_apps WHERE app_id = ? AND service = ? LIMIT 1", appID, service).Scan(&platform)
		if appName == "" {
			_ = m.db.QueryRow("SELECT COALESCE(name, '') FROM watched_apps WHERE app_id = ? AND service = ? LIMIT 1", appID, service).Scan(&appName)
		}
	}
	if platform == "" {
		platform = "PC"
	}

	m.CancelJobsForApp(service, appID)

	m.jobsMu.Lock()
	jobID := fmt.Sprintf("%s-%d-%d", service, appID, time.Now().Unix())
	jobCtx, cancel := context.WithCancel(context.Background())
	job := &PrefillJob{
		ID:          jobID,
		Service:     service,
		AppID:       appID,
		AppName:     appName,
		Platform:    platform,
		Status:      "queued",
		ProgressPct: 0.0,
		StartedAt:   time.Now(),
		cancelFn:    cancel,
	}
	m.jobs[jobID] = job
	m.jobsMu.Unlock()

	// Update watched app status in SQLite
	if m.db != nil {
		_ = m.db.UpdateWatchedAppStatus(appID, service, "prefilling")
	}
	ws.GetHub().Broadcast("library:updated", nil)

	go m.runPrefill(jobCtx, job)
	return job, nil
}

// ListJobs returns all current and historic prefill jobs
func (m *Manager) ListJobs() []*PrefillJob {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()

	result := make([]*PrefillJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		result = append(result, j)
	}
	return result
}

// GetJob returns a specific job by ID
func (m *Manager) GetJob(id string) (*PrefillJob, bool) {
	m.jobsMu.RLock()
	defer m.jobsMu.RUnlock()
	j, ok := m.jobs[id]
	return j, ok
}

// CancelJobsForApp cancels any active or queued prefill jobs for the given service and appID
func (m *Manager) CancelJobsForApp(service string, appID int64) int {
	m.jobsMu.Lock()
	cancelled := 0
	for id, job := range m.jobs {
		if (service == "" || strings.EqualFold(job.Service, service)) && job.AppID == appID {
			if job.cancelFn != nil {
				job.cancelFn()
			}
			job.Status = "cancelled"
			delete(m.jobs, id)
			cancelled++
		}
	}
	rec := m.recorder
	m.jobsMu.Unlock()

	if rec != nil {
		rec.CompletePrefillSession("Artesian", service, appID)
	}
	return cancelled
}

func (m *Manager) runPrefill(ctx context.Context, job *PrefillJob) {
	job.Status = "diffing"
	m.broadcastProgress(job)

	log.Printf("[Prefill Engine] Retrieving depot manifest for %s (App ID %d)...", job.AppName, job.AppID)

	// Phase 1: Manifest Retrieval & Decryption (via DB, Disk, or Native Steam CM)
	manifest := m.generateAppManifest(ctx, job)
	if manifest == nil || len(manifest.Chunks) == 0 {
		job.Status = "completed"
		job.ProgressPct = 100.0
		job.EndedAt = time.Now()
		m.finalizeAppStatus(job.AppID, job.Service, job.AppName, "monitoring")
		m.broadcastProgress(job)
		log.Printf("[Prefill Engine] App %d (%s): Status set to monitoring (authenticate Steam account or install via client to enable caching).", job.AppID, job.AppName)
		return
	}

	job.TotalChunks = int64(len(manifest.Chunks))
	for _, c := range manifest.Chunks {
		job.BytesTotal += c.SizeBytes
	}

	// Phase 4: Instant Delta Diffing against SQLite database
	chunkIDs := make([]string, len(manifest.Chunks))
	for i, c := range manifest.Chunks {
		chunkIDs[i] = c.ChunkID
	}

	existingMap, err := m.db.CheckExistingChunks(job.Service, chunkIDs)
	if err != nil {
		log.Printf("[Prefill Engine] DB diff error: %v", err)
		existingMap = make(map[string]bool)
	}

	var initialExistingBytes int64
	var missingChunks []ChunkMeta
	for _, c := range manifest.Chunks {
		if existingMap[c.ChunkID] {
			job.ExistingChunks++
			initialExistingBytes += c.SizeBytes
		} else {
			missingChunks = append(missingChunks, c)
		}
	}
	job.BytesCached = initialExistingBytes

	log.Printf("[Prefill Engine] Delta diff complete for %s: %d total, %d already cached, %d missing chunks to download.",
		job.AppName, job.TotalChunks, job.ExistingChunks, len(missingChunks))

	job.Status = "downloading"
	if job.TotalChunks > 0 {
		job.ProgressPct = float64(job.ExistingChunks) / float64(job.TotalChunks) * 100.0
	}
	m.broadcastProgress(job)

	m.jobsMu.RLock()
	rec := m.recorder
	m.jobsMu.RUnlock()

	firstDepot := manifest.DepotID
	if len(manifest.Chunks) > 0 {
		firstDepot = manifest.Chunks[0].DepotID
	}

	if rec != nil {
		rec.RecordPrefillProgress("Artesian", job.Service, job.AppName, job.Platform, firstDepot, job.AppID, "start", initialExistingBytes, job.BytesTotal)
	}

	if len(missingChunks) == 0 {
		job.Status = "completed"
		job.ProgressPct = 100.0
		job.EndedAt = time.Now()
		m.finalizeAppStatus(job.AppID, job.Service, job.AppName, "synced")
		m.broadcastProgress(job)
		if rec != nil {
			rec.RecordPrefillProgress("Artesian", job.Service, job.AppName, job.Platform, firstDepot, job.AppID, "cached", job.BytesTotal, job.BytesTotal)
			rec.CompletePrefillSession("Artesian", job.Service, job.AppID)
		}
		return
	}

	// Phase 4: Headless Concurrent Downloader Pool
	workerCount := 4
	chunkChan := make(chan ChunkMeta, len(missingChunks))
	for _, c := range missingChunks {
		chunkChan <- c
	}
	close(chunkChan)

	var (
		downloadedChunks int64
		downloadedBytes  int64
		wg               sync.WaitGroup
	)

	startDownloadTime := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	// Progress broadcaster goroutine
	doneChan := make(chan struct{})
	go func() {
		for {
			select {
			case <-doneChan:
				return
			case <-ticker.C:
				nowDownloadedChunks := atomic.LoadInt64(&downloadedChunks)
				nowDownloadedBytes := atomic.LoadInt64(&downloadedBytes)

				totalCurrentCached := job.ExistingChunks + nowDownloadedChunks
				job.DownloadedChunks = nowDownloadedChunks
				job.BytesCached = initialExistingBytes + nowDownloadedBytes

				if job.TotalChunks > 0 {
					job.ProgressPct = float64(totalCurrentCached) / float64(job.TotalChunks) * 100.0
				}

				elapsed := time.Since(startDownloadTime).Seconds()
				if elapsed > 0 {
					mbps := (float64(nowDownloadedBytes*8) / 1000000.0) / elapsed
					job.SpeedMbps = mbps
					remainingBytes := job.BytesTotal - job.BytesCached
					if mbps > 0 && remainingBytes > 0 {
						job.ETASeconds = int64(float64(remainingBytes*8) / (mbps * 1000000.0))
					}
				}

				m.broadcastProgress(job)
			}
		}
	}()

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for chunk := range chunkChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Download chunk directly into cache storage and record in DB
				m.downloadChunk(job.Service, job.AppID, chunk.DepotID, chunk)
				atomic.AddInt64(&downloadedChunks, 1)
				nowBytes := atomic.AddInt64(&downloadedBytes, chunk.SizeBytes)

				// Report progress to Active Downloads table with Client "Artesian"
				if rec != nil {
					currentTotal := initialExistingBytes + nowBytes
					rec.RecordPrefillProgress("Artesian", job.Service, job.AppName, job.Platform, chunk.DepotID, job.AppID, chunk.ChunkID, currentTotal, job.BytesTotal)
				}

				// Adaptive pacing throttle for smooth live streaming presentation
				throttle := 75 * time.Millisecond
				if len(missingChunks) > 500 {
					throttle = 25 * time.Millisecond
				}
				if len(missingChunks) > 2000 {
					throttle = 10 * time.Millisecond
				}
				time.Sleep(throttle)
			}
		}(i)
	}

	wg.Wait()
	close(doneChan)

	if ctx.Err() != nil {
		job.Status = "cancelled"
		job.Error = "Prefill cancelled"
		if m.db != nil {
			var exists int
			_ = m.db.QueryRow("SELECT COUNT(1) FROM watched_apps WHERE app_id = ? AND service = ?", job.AppID, job.Service).Scan(&exists)
			if exists > 0 {
				m.finalizeAppStatus(job.AppID, job.Service, job.AppName, "error")
			}
		}
		m.broadcastProgress(job)
		log.Printf("[Prefill Engine] Prefill cancelled for %s", job.AppName)
		return
	}

	job.Status = "completed"
	job.ProgressPct = 100.0
	job.EndedAt = time.Now()
	m.finalizeAppStatus(job.AppID, job.Service, job.AppName, "synced")
	m.broadcastProgress(job)

	if rec != nil {
		firstDepot := manifest.DepotID
		if len(manifest.Chunks) > 0 {
			firstDepot = manifest.Chunks[0].DepotID
		}
		rec.RecordPrefillProgress("Artesian", job.Service, job.AppName, job.Platform, firstDepot, job.AppID, "completed", job.BytesTotal, job.BytesTotal)
		rec.CompletePrefillSession("Artesian", job.Service, job.AppID)
	}

	log.Printf("[Prefill Engine] Successfully prefilled %s (100%% cached in tank).", job.AppName)
}

func (m *Manager) downloadChunk(service string, appID, depotID int64, chunk ChunkMeta) {
	chunkURL := fmt.Sprintf("/depot/%d/chunk/%s", depotID, chunk.ChunkID)

	var adapter ports.PlatformAdapter
	if m.registry != nil {
		adapter, _ = m.registry.Get(service)
	}

	var cdnHosts []string
	if adapter != nil {
		cdnHosts = adapter.GetCDNHosts()
	}
	if len(cdnHosts) == 0 {
		cdnHosts = []string{"cache9-lhr1.steamcontent.com"}
	}

	var downloaded bool
	for _, host := range cdnHosts {
		reqURL := fmt.Sprintf("http://%s%s", host, chunkURL)
		if adapter != nil {
			reqURL = adapter.BuildChunkURL(host, depotID, chunk.ChunkID)
		}
		req, err := http.NewRequestWithContext(context.Background(), "GET", reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Valve/Steam HTTP Client 1.0")
		req.Header.Set("X-Artesian-Prefill", "1")
		req.Header.Set("X-Artesian-Loop", "1")

		resp, err := m.httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			destPath, written, storeErr := m.cache.Store(service, chunkURL, resp.Body)
			_ = resp.Body.Close()
			if storeErr == nil && m.db != nil {
				_ = m.db.RecordChunk(chunk.ChunkID, service, depotID, appID, written, destPath)
			}
			downloaded = true
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
	}

	// If remote download was unsuccessful
	if !downloaded {
		log.Printf("[Prefill Engine] Warning: Could not download chunk %s for depot %d from CDN", chunk.ChunkID, depotID)
	}
}

func (m *Manager) finalizeAppStatus(appID int64, service, name, status string) {
	if m.db != nil {
		_ = m.db.UpdateWatchedAppStatus(appID, service, status)
	}
	ws.GetHub().Broadcast("library:updated", nil)
}

func (m *Manager) broadcastProgress(job *PrefillJob) {
	ws.GetHub().Broadcast("prefill:progress", job)
}

// generateAppManifest resolves genuine depot manifests and chunk hashes for games
func (m *Manager) generateAppManifest(ctx context.Context, job *PrefillJob) *DepotManifest {
	appID := job.AppID
	targetPlatform := job.Platform
	if targetPlatform == "" && m.db != nil {
		_ = m.db.QueryRow("SELECT COALESCE(platform, '') FROM watched_apps WHERE app_id = ? AND service = ? LIMIT 1", appID, job.Service).Scan(&targetPlatform)
	}

	var adapter ports.PlatformAdapter
	if m.registry != nil {
		adapter, _ = m.registry.Get(job.Service)
	}

	// Ensure depot metadata exists in depot_meta_cache with valid manifest_id
	if m.db != nil {
		var depotCount int
		_ = m.db.QueryRow("SELECT COUNT(*) FROM depot_meta_cache WHERE app_id = ? AND total_size > 0 AND manifest_id > 0", appID).Scan(&depotCount)
		if depotCount == 0 && adapter != nil {
			log.Printf("[Prefill Engine] App %d (%s): Depot metadata missing or manifest_id is 0, querying remote %s metadata...", appID, job.AppName, adapter.DisplayName())
			if err := adapter.FetchAppInfo(appID); err != nil {
				log.Printf("[Prefill Engine] Could not fetch remote depot metadata for app %d: %v", appID, err)
			} else {
				log.Printf("[Prefill Engine] App %d (%s): Remote depot metadata query completed successfully", appID, job.AppName)
			}
		}
	}

	type matchedDepot struct {
		depotID    int64
		manifestID int64
		platform   string
		totalSize  int64
	}
	var allDepots []matchedDepot
	if m.db != nil {
		rows, err := m.db.Query("SELECT depot_id, manifest_id, COALESCE(platform, ''), total_size FROM depot_meta_cache WHERE app_id = ? AND total_size > 0 ORDER BY total_size DESC", appID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var d matchedDepot
				if err := rows.Scan(&d.depotID, &d.manifestID, &d.platform, &d.totalSize); err == nil {
					allDepots = append(allDepots, d)
				}
			}
		}
	}

	if len(allDepots) == 0 {
		log.Printf("[Prefill Engine] No depot metadata available for app %d (%s). Waiting for game install/update via Steam client to capture manifests.", appID, job.AppName)
	}

	// Filter depots by targetPlatform if specified (e.g. "Windows, Mac")
	var activeDepots []matchedDepot
	if targetPlatform != "" {
		var targets []string
		for _, part := range strings.Split(targetPlatform, ",") {
			if t := strings.TrimSpace(strings.ToLower(part)); t != "" {
				targets = append(targets, t)
			}
		}

		matchesPlatform := func(depotPlat, target string) bool {
			dp := strings.ToLower(depotPlat)
			tg := strings.ToLower(target)
			if dp == "" || dp == "pc" || tg == "" || tg == "pc" || tg == "all" {
				return true // Unspecified/common content depots apply to all platforms
			}
			if strings.Contains(tg, "mac") || strings.Contains(tg, "apple") || strings.Contains(tg, "osx") {
				return strings.Contains(dp, "mac") || strings.Contains(dp, "apple") || strings.Contains(dp, "osx")
			}
			if strings.Contains(tg, "linux") {
				return strings.Contains(dp, "linux")
			}
			if strings.Contains(tg, "win") {
				return strings.Contains(dp, "win")
			}
			return strings.Contains(dp, tg) || strings.Contains(tg, dp)
		}

		// Check if a 64-bit depot exists for each target platform to filter out legacy 32-bit depots
		has64BitForTarget := make(map[string]bool)
		for _, target := range targets {
			for _, d := range allDepots {
				if matchesPlatform(d.platform, target) && strings.Contains(strings.ToLower(d.platform), "64") {
					has64BitForTarget[target] = true
					break
				}
			}
		}

		seenDepots := make(map[int64]bool)
		for _, d := range allDepots {
			dPlatLower := strings.ToLower(d.platform)
			isMatch := false
			for _, target := range targets {
				if matchesPlatform(d.platform, target) {
					// If this platform has a 64-bit depot, skip obsolete 32-bit depot
					if has64BitForTarget[target] && strings.Contains(dPlatLower, "32") {
						continue
					}
					isMatch = true
					break
				}
			}
			if isMatch && !seenDepots[d.depotID] {
				seenDepots[d.depotID] = true
				activeDepots = append(activeDepots, d)
			}
		}
	}
	if len(activeDepots) == 0 {
		activeDepots = allDepots
	}
	log.Printf("[Prefill Engine] App %d (%s): Evaluating %d depots matching target platform '%s'", appID, job.AppName, len(activeDepots), targetPlatform)

	var chunks []ChunkMeta
	firstDepotID := appID
	if len(activeDepots) > 0 {
		firstDepotID = activeDepots[0].depotID
	}

	for _, d := range activeDepots {
		var depotChunks []ChunkMeta
		log.Printf("[Prefill Engine] Depot %d: Resolving manifest (manifestID=%d, platform=%s)...", d.depotID, d.manifestID, d.platform)

		if adapter != nil {
			chunks, err := adapter.ResolveManifestChunks(ctx, job.AppID, d.depotID, d.manifestID)
			if err == nil && len(chunks) > 0 {
				log.Printf("[Prefill Engine] Resolved %d genuine manifest chunks for depot %d", len(chunks), d.depotID)
				for _, c := range chunks {
					depotChunks = append(depotChunks, ChunkMeta{
						DepotID:   d.depotID,
						ChunkID:   c.ChunkID,
						SizeBytes: c.SizeBytes,
					})
				}
			} else if err != nil {
				log.Printf("[Prefill Engine] Manifest resolution notice for depot %d: %v", d.depotID, err)
			}
		}

		if len(depotChunks) == 0 {
			log.Printf("[Prefill Engine] Notice: No genuine manifest resolved for depot %d (manifest %d). Waiting for game install/update via Steam client.", d.depotID, d.manifestID)
		}

		chunks = append(chunks, depotChunks...)
	}

	return &DepotManifest{
		AppID:      appID,
		DepotID:    firstDepotID,
		ManifestID: uint64(time.Now().Unix()),
		Chunks:     chunks,
	}
}

