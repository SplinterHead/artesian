package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)


// DB wraps standard sql.DB with Artesian helper methods
type DB struct {
	*sql.DB
}

// WatchedApp represents an application configured for automatic tracking & prefilling
type WatchedApp struct {
	AppID          int64     `json:"app_id"`
	Service        string    `json:"service"` // e.g. "steam"
	Name           string    `json:"name"`
	AutoUpdate     bool      `json:"auto_update"`
	CurrentVersion string    `json:"current_version"`
	ChangeNumber   uint64    `json:"change_number"`
	InstalledAt    time.Time `json:"installed_at"`
	LastChecked    time.Time `json:"last_checked"`
	Status         string    `json:"status"` // "synced", "prefilling", "outdated"
}

// ChunkEntry represents a cached file chunk
type ChunkEntry struct {
	ChunkID      string    `json:"chunk_id"`
	Service      string    `json:"service"`
	DepotID      int64     `json:"depot_id"`
	AppID        int64     `json:"app_id"`
	SizeBytes    int64     `json:"size_bytes"`
	DownloadedAt time.Time `json:"downloaded_at"`
	FilePath     string    `json:"file_path"`
	HitCount     int64     `json:"hit_count"`
}

// DepotMeta caches dynamic lookup metadata for a specific depot
type DepotMeta struct {
	DepotID     int64     `json:"depot_id"`
	AppID       int64     `json:"app_id"`
	Service     string    `json:"service"`
	GameTitle   string    `json:"game_title"`
	Platform    string    `json:"platform"`
	TotalSize   int64     `json:"total_size"`
	LastUpdated time.Time `json:"last_updated"`
}

// SteamSession represents authenticated Steam credentials
type SteamSession struct {
	Username     string    `json:"username"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ClientID     string    `json:"client_id"`
	IsActive     bool      `json:"is_active"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StatSummary holds bandwidth and cache hit statistics
type StatSummary struct {
	TotalRequests int64   `json:"total_requests"`
	CacheHits     int64   `json:"cache_hits"`
	CacheMisses   int64   `json:"cache_misses"`
	HitRatio      float64 `json:"hit_ratio"`
	BytesServed   int64   `json:"bytes_served"`
	BytesSaved    int64   `json:"bytes_saved"`
	ReadBytesSec  int64   `json:"read_bytes_sec"`
	WriteBytesSec int64   `json:"write_bytes_sec"`
}

// Open initializes the SQLite database and sets up clean initial schema
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", path, err)
	}

	db := &DB{sqlDB}
	if err := db.initSchema(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("database schema initialization failed: %w", err)
	}

	return db, nil
}

func (db *DB) initSchema() error {
	pragmas := `
	PRAGMA journal_mode = DELETE;
	PRAGMA synchronous = NORMAL;
	PRAGMA busy_timeout = 5000;
	`
	_, _ = db.Exec(pragmas)

	schema := `
	CREATE TABLE IF NOT EXISTS config_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS watched_apps (
		app_id INTEGER NOT NULL,
		service TEXT NOT NULL DEFAULT 'steam',
		name TEXT NOT NULL,
		auto_update BOOLEAN DEFAULT 1,
		current_version TEXT DEFAULT '',
		change_number INTEGER DEFAULT 0,
		status TEXT DEFAULT 'synced',
		installed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (app_id, service)
	);

	CREATE TABLE IF NOT EXISTS chunk_cache (
		chunk_id TEXT PRIMARY KEY,
		service TEXT NOT NULL DEFAULT 'steam',
		depot_id INTEGER DEFAULT 0,
		app_id INTEGER DEFAULT 0,
		size_bytes INTEGER DEFAULT 0,
		downloaded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		file_path TEXT NOT NULL,
		hit_count INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_chunk_depot ON chunk_cache(service, depot_id);
	CREATE INDEX IF NOT EXISTS idx_chunk_app ON chunk_cache(service, app_id);

	CREATE TABLE IF NOT EXISTS steam_session (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		username TEXT NOT NULL,
		access_token TEXT,
		refresh_token TEXT,
		client_id TEXT,
		is_active BOOLEAN DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_ip TEXT NOT NULL,
		host TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		status INTEGER DEFAULT 200,
		is_hit BOOLEAN DEFAULT 0,
		bytes_transferred INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_logs_created ON request_logs(created_at);

	CREATE TABLE IF NOT EXISTS scheduler_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		cron_expression TEXT NOT NULL,
		task_type TEXT NOT NULL,
		service TEXT NOT NULL,
		enabled BOOLEAN DEFAULT 1,
		last_run DATETIME,
		next_run DATETIME
	);

	CREATE TABLE IF NOT EXISTS depot_meta_cache (
		depot_id INTEGER PRIMARY KEY,
		app_id INTEGER NOT NULL,
		service TEXT NOT NULL DEFAULT 'steam',
		game_title TEXT NOT NULL,
		platform TEXT NOT NULL,
		total_size INTEGER DEFAULT 0,
		last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_depot_app ON depot_meta_cache(service, app_id);

	CREATE TABLE IF NOT EXISTS game_meta_cache (
		id TEXT PRIMARY KEY,
		service TEXT NOT NULL,
		app_id INTEGER DEFAULT 0,
		name TEXT NOT NULL,
		platform TEXT DEFAULT 'PC',
		last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(schema)
	return err
}

// GetSetting retrieves a configuration value
func (db *DB) GetSetting(key, defaultVal string) string {
	var val string
	err := db.QueryRow("SELECT value FROM config_settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		return defaultVal
	}
	return val
}

// SetSetting stores a configuration key-value pair
func (db *DB) SetSetting(key, val string) error {
	query := `
	INSERT INTO config_settings (key, value, updated_at) 
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, key, val)
	return err
}

// GetSteamSession retrieves the active Steam session if present
func (db *DB) GetSteamSession() (*SteamSession, error) {
	var sess SteamSession
	var isActive int
	err := db.QueryRow(`
		SELECT username, COALESCE(access_token, ''), COALESCE(refresh_token, ''), COALESCE(client_id, ''), is_active, updated_at
		FROM steam_session WHERE id = 1
	`).Scan(&sess.Username, &sess.AccessToken, &sess.RefreshToken, &sess.ClientID, &isActive, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sess.IsActive = (isActive == 1)
	return &sess, nil
}

// SaveSteamSession saves or updates the active Steam session
func (db *DB) SaveSteamSession(sess *SteamSession) error {
	query := `
	INSERT INTO steam_session (id, username, access_token, refresh_token, client_id, is_active, updated_at)
	VALUES (1, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(id) DO UPDATE SET 
		username = excluded.username,
		access_token = excluded.access_token,
		refresh_token = excluded.refresh_token,
		client_id = excluded.client_id,
		is_active = excluded.is_active,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, sess.Username, sess.AccessToken, sess.RefreshToken, sess.ClientID, sess.IsActive)
	return err
}

// ClearSteamSession logs out the Steam session
func (db *DB) ClearSteamSession() error {
	_, err := db.Exec("UPDATE steam_session SET is_active = 0, access_token = '', refresh_token = '' WHERE id = 1")
	return err
}

// CheckExistingChunks performs instant Delta Diffing against SQLite without scanning disk
func (db *DB) CheckExistingChunks(service string, chunkIDs []string) (map[string]bool, error) {
	existing := make(map[string]bool)
	if len(chunkIDs) == 0 {
		return existing, nil
	}

	// Batch queries in chunks of 500 to prevent SQLite variable limits
	batchSize := 500
	for i := 0; i < len(chunkIDs); i += batchSize {
		end := i + batchSize
		if end > len(chunkIDs) {
			end = len(chunkIDs)
		}
		batch := chunkIDs[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch)+1)
		args[0] = service
		for j, id := range batch {
			placeholders[j] = "?"
			args[j+1] = id
		}

		query := fmt.Sprintf(
			"SELECT chunk_id FROM chunk_cache WHERE service = ? AND chunk_id IN (%s)",
			strings.Join(placeholders, ","),
		)

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				existing[id] = true
			}
		}
		_ = rows.Close()
	}

	return existing, nil
}

// RecordChunk registers a saved chunk into SQLite
func (db *DB) RecordChunk(chunkID, service string, depotID, appID, sizeBytes int64, filePath string) error {
	query := `
	INSERT INTO chunk_cache (chunk_id, service, depot_id, app_id, size_bytes, file_path, hit_count, downloaded_at)
	VALUES (?, ?, ?, ?, ?, ?, 0, CURRENT_TIMESTAMP)
	ON CONFLICT(chunk_id) DO UPDATE SET 
		hit_count = chunk_cache.hit_count + 1;
	`
	_, err := db.Exec(query, chunkID, service, depotID, appID, sizeBytes, filePath)
	return err
}

// IncrementChunkHit records a cache hit for a chunk
func (db *DB) IncrementChunkHit(chunkID string) {
	_, _ = db.Exec("UPDATE chunk_cache SET hit_count = hit_count + 1 WHERE chunk_id = ?", chunkID)
}

// GetWatchedApps returns all monitored titles
func (db *DB) GetWatchedApps() ([]WatchedApp, error) {
	rows, err := db.Query(`
		SELECT app_id, service, name, auto_update, COALESCE(current_version, ''), change_number, status, installed_at, last_checked
		FROM watched_apps ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []WatchedApp
	for rows.Next() {
		var a WatchedApp
		if err := rows.Scan(&a.AppID, &a.Service, &a.Name, &a.AutoUpdate, &a.CurrentVersion, &a.ChangeNumber, &a.Status, &a.InstalledAt, &a.LastChecked); err == nil {
			apps = append(apps, a)
		}
	}
	return apps, nil
}

// UpsertWatchedApp inserts or updates a watched title
func (db *DB) UpsertWatchedApp(app WatchedApp) error {
	query := `
	INSERT INTO watched_apps (app_id, service, name, auto_update, current_version, change_number, status, last_checked)
	VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(app_id, service) DO UPDATE SET
		name = excluded.name,
		auto_update = excluded.auto_update,
		current_version = excluded.current_version,
		change_number = excluded.change_number,
		status = excluded.status,
		last_checked = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, app.AppID, app.Service, app.Name, app.AutoUpdate, app.CurrentVersion, app.ChangeNumber, app.Status)
	return err
}

// DeleteWatchedApp removes an app from the watched library
func (db *DB) DeleteWatchedApp(appID int64, service string) error {
	if db == nil || appID <= 0 {
		return nil
	}
	if service == "" {
		service = "steam"
	}
	_, err := db.Exec("DELETE FROM watched_apps WHERE app_id = ? AND service = ?", appID, service)
	return err
}

// InterceptedGame represents a game discovered/intercepted in cache that is not in watched_apps
type InterceptedGame struct {
	AppID        int64     `json:"app_id"`
	Service      string    `json:"service"`
	Name         string    `json:"name"`
	Platform     string    `json:"platform"`
	TotalSize    int64     `json:"total_size"`
	CachedBytes  int64     `json:"cached_bytes"`
	CachedChunks int64     `json:"cached_chunks"`
	LastSeen     time.Time `json:"last_seen"`
}

// GetInterceptedGames returns cached/intercepted games that are not in watched_apps
func (db *DB) GetInterceptedGames() ([]InterceptedGame, error) {
	if db == nil {
		return []InterceptedGame{}, nil
	}

	var games []InterceptedGame

	// 1. Steam games from depot_meta_cache + chunk_cache
	steamQuery := `
	SELECT 
		d.app_id,
		d.service,
		d.game_title,
		COALESCE((SELECT d2.platform FROM depot_meta_cache d2 JOIN chunk_cache c2 ON c2.depot_id = d2.depot_id WHERE d2.app_id = d.app_id LIMIT 1), d.platform) as platform,
		COALESCE((SELECT SUM(DISTINCT d3.total_size) FROM depot_meta_cache d3 WHERE d3.app_id = d.app_id), 0) as total_size,
		COUNT(c.chunk_id) as cached_chunks,
		COALESCE(SUM(c.size_bytes), 0) as cached_bytes,
		COALESCE(MAX(c.downloaded_at), MAX(d.last_updated)) as last_seen
	FROM depot_meta_cache d
	LEFT JOIN chunk_cache c ON c.depot_id = d.depot_id
	WHERE d.app_id > 0
	  AND d.app_id NOT IN (SELECT app_id FROM watched_apps WHERE service = d.service)
	GROUP BY d.app_id, d.service
	HAVING COUNT(c.chunk_id) > 0
	ORDER BY last_seen DESC;
	`

	rows, err := db.Query(steamQuery)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var g InterceptedGame
			var lastSeenStr string
			if err := rows.Scan(&g.AppID, &g.Service, &g.Name, &g.Platform, &g.TotalSize, &g.CachedChunks, &g.CachedBytes, &lastSeenStr); err == nil {
				t, parseErr := time.Parse("2006-01-02 15:04:05", lastSeenStr)
				if parseErr != nil {
					t, _ = time.Parse(time.RFC3339, lastSeenStr)
				}
				g.LastSeen = t
				games = append(games, g)
			}
		}
	}

	// 2. Epic Games and other services from game_meta_cache + chunk_cache
	epicQuery := `
	SELECT 
		COALESCE(g.app_id, 0),
		g.service,
		g.name,
		g.platform,
		0 as total_size,
		COUNT(c.chunk_id) as cached_chunks,
		COALESCE(SUM(c.size_bytes), 0) as cached_bytes,
		COALESCE(MAX(c.downloaded_at), MAX(g.last_updated)) as last_seen
	FROM game_meta_cache g
	LEFT JOIN chunk_cache c ON c.service = g.service
	WHERE g.name NOT IN (SELECT name FROM watched_apps WHERE service = g.service)
	GROUP BY g.id, g.service
	HAVING COUNT(c.chunk_id) > 0
	ORDER BY last_seen DESC;
	`
	epicRows, err := db.Query(epicQuery)
	if err == nil {
		defer epicRows.Close()
		for epicRows.Next() {
			var g InterceptedGame
			var lastSeenStr string
			if err := epicRows.Scan(&g.AppID, &g.Service, &g.Name, &g.Platform, &g.TotalSize, &g.CachedChunks, &g.CachedBytes, &lastSeenStr); err == nil {
				t, parseErr := time.Parse("2006-01-02 15:04:05", lastSeenStr)
				if parseErr != nil {
					t, _ = time.Parse(time.RFC3339, lastSeenStr)
				}
				g.LastSeen = t
				games = append(games, g)
			}
		}
	}

	return games, nil
}

// GetStats returns aggregated metrics and hit ratio
func (db *DB) GetStats() (StatSummary, error) {
	var stats StatSummary
	query := `
	SELECT 
		COUNT(*) as total_requests,
		COALESCE(SUM(CASE WHEN is_hit = 1 THEN 1 ELSE 0 END), 0) as cache_hits,
		COALESCE(SUM(CASE WHEN is_hit = 0 THEN 1 ELSE 0 END), 0) as cache_misses,
		COALESCE(SUM(bytes_transferred), 0) as bytes_served,
		COALESCE(SUM(CASE WHEN is_hit = 1 THEN bytes_transferred ELSE 0 END), 0) as bytes_saved
	FROM request_logs;
	`
	err := db.QueryRow(query).Scan(
		&stats.TotalRequests,
		&stats.CacheHits,
		&stats.CacheMisses,
		&stats.BytesServed,
		&stats.BytesSaved,
	)
	if err != nil {
		return stats, err
	}

	if stats.TotalRequests > 0 {
		stats.HitRatio = (float64(stats.CacheHits) / float64(stats.TotalRequests)) * 100.0
	}
	return stats, nil
}

// RequestLog represents a logged proxy request entry
type RequestLog struct {
	ID               int64     `json:"id"`
	ClientIP         string    `json:"client_ip"`
	Host             string    `json:"host"`
	Method           string    `json:"method"`
	Path             string    `json:"path"`
	Status           int       `json:"status"`
	IsHit            bool      `json:"is_hit"`
	BytesTransferred int64     `json:"bytes_transferred"`
	DurationMs       int64     `json:"duration_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

// GetRecentRequestLogs returns the most recent request logs from SQLite
func (db *DB) GetRecentRequestLogs(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, client_ip, host, method, path, status, is_hit, bytes_transferred, duration_ms, created_at
		FROM request_logs
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var createdAtStr string
		if err := rows.Scan(&l.ID, &l.ClientIP, &l.Host, &l.Method, &l.Path, &l.Status, &l.IsHit, &l.BytesTransferred, &l.DurationMs, &createdAtStr); err != nil {
			log.Printf("[Artesian DB] GetRecentRequestLogs scan error: %v", err)
			continue
		}
		t, parseErr := time.Parse("2006-01-02 15:04:05", createdAtStr)
		if parseErr != nil {
			t, _ = time.Parse(time.RFC3339, createdAtStr)
		}
		l.CreatedAt = t
		logs = append(logs, l)
	}
	return logs, nil
}

// GetDepotMeta retrieves cached depot metadata from SQLite
func (db *DB) GetDepotMeta(depotID int64) (*DepotMeta, error) {
	if db == nil || depotID <= 0 {
		return nil, sql.ErrNoRows
	}
	var d DepotMeta
	var lu string
	err := db.QueryRow(`
		SELECT depot_id, app_id, service, game_title, platform, total_size, last_updated 
		FROM depot_meta_cache WHERE depot_id = ?
	`, depotID).Scan(&d.DepotID, &d.AppID, &d.Service, &d.GameTitle, &d.Platform, &d.TotalSize, &lu)
	if err != nil {
		return nil, err
	}
	d.LastUpdated, _ = time.Parse("2006-01-02 15:04:05", lu)
	return &d, nil
}

// UpsertDepotMeta stores or refreshes cached depot metadata in SQLite
func (db *DB) UpsertDepotMeta(meta DepotMeta) error {
	if db == nil || meta.DepotID <= 0 {
		return nil
	}
	query := `
		INSERT INTO depot_meta_cache (depot_id, app_id, service, game_title, platform, total_size, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(depot_id) DO UPDATE SET
			app_id = excluded.app_id,
			service = excluded.service,
			game_title = excluded.game_title,
			platform = excluded.platform,
			total_size = excluded.total_size,
			last_updated = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, meta.DepotID, meta.AppID, meta.Service, meta.GameTitle, meta.Platform, meta.TotalSize)
	return err
}

// GetGameMeta retrieves cached game metadata by ID
func (db *DB) GetGameMeta(id string) (name, platform string, err error) {
	if db == nil || id == "" {
		return "", "", sql.ErrNoRows
	}
	err = db.QueryRow("SELECT name, platform FROM game_meta_cache WHERE id = ?", id).Scan(&name, &platform)
	return name, platform, err
}

// UpsertGameMeta caches game metadata in SQLite
func (db *DB) UpsertGameMeta(id, service string, appID int64, name, platform string) error {
	if db == nil || id == "" {
		return nil
	}
	query := `
		INSERT INTO game_meta_cache (id, service, app_id, name, platform, last_updated)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			platform = excluded.platform,
			last_updated = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, id, service, appID, name, platform)
	return err
}


