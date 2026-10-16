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
	Platform       string    `json:"platform"` // e.g. "Windows", "Mac 64bit"
	AutoUpdate     bool      `json:"auto_update"`
	CurrentVersion string    `json:"current_version"`
	ChangeNumber   uint64    `json:"change_number"`
	InstalledAt    time.Time `json:"installed_at"`
	LastChecked    time.Time `json:"last_checked"`
	Status         string    `json:"status"` // "synced", "prefilling", "outdated"
	SizeBytes      int64     `json:"size_bytes"`
}

// SchedulerRule represents a persistent cron task rule stored in SQLite
type SchedulerRule struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CronExpression string    `json:"cron_expression"`
	TaskType       string    `json:"task_type"` // e.g. "steam_update_check", "cache_sync"
	Service        string    `json:"service"`
	Enabled        bool      `json:"enabled"`
	LastRun        time.Time `json:"last_run"`
	NextRun        time.Time `json:"next_run"`
}

// UpdateLogEntry represents a persistent record of a discovered game update or check
type UpdateLogEntry struct {
	ID           int64     `json:"id"`
	AppID        int64     `json:"app_id"`
	Service      string    `json:"service"`
	GameTitle    string    `json:"game_title"`
	ChangeNumber uint64    `json:"change_number"`
	ManifestID   string    `json:"manifest_id"`
	Status       string    `json:"status"` // "Verified", "Discovered", "Prefilling", "Synced"
	Message      string    `json:"message"`
	DiscoveredAt time.Time `json:"discovered_at"`
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
	ManifestID  int64     `json:"manifest_id"`
	LastUpdated time.Time `json:"last_updated"`
}

// ManifestChunkRecord represents a chunk parsed from a depot manifest
type ManifestChunkRecord struct {
	DepotID        int64  `json:"depot_id"`
	ManifestID     int64  `json:"manifest_id"`
	ChunkID        string `json:"chunk_id"`
	SizeBytes      int64  `json:"size_bytes"`
	OriginalSize   int64  `json:"original_size"`
	CompressedSize int64  `json:"compressed_size"`
}


// SteamSession represents authenticated Steam credentials
type SteamSession struct {
	Username       string    `json:"username"`
	SteamID        string    `json:"steam_id"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	SessionCookies string    `json:"session_cookies"`
	ClientID       string    `json:"client_id"`
	IsActive       bool      `json:"is_active"`
	UpdatedAt      time.Time `json:"updated_at"`
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

	if err := db.seedDefaults(); err != nil {
		log.Printf("[Artesian DB] Warning: seeding defaults encountered error: %v", err)
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
		platform TEXT NOT NULL DEFAULT '',
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
		steam_id TEXT DEFAULT '',
		access_token TEXT,
		refresh_token TEXT,
		session_cookies TEXT DEFAULT '',
		client_id TEXT,
		is_active BOOLEAN DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS service_credentials (
		service TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		token TEXT DEFAULT '',
		connected_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
		manifest_id INTEGER DEFAULT 0,
		last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_depot_app ON depot_meta_cache(service, app_id);

	CREATE TABLE IF NOT EXISTS depot_manifest_chunks (
		depot_id INTEGER NOT NULL,
		manifest_id INTEGER NOT NULL,
		chunk_id TEXT NOT NULL,
		size_bytes INTEGER DEFAULT 0,
		original_size INTEGER DEFAULT 0,
		compressed_size INTEGER DEFAULT 0,
		PRIMARY KEY (depot_id, chunk_id)
	);

	CREATE INDEX IF NOT EXISTS idx_manifest_chunks_depot ON depot_manifest_chunks(depot_id);

	CREATE TABLE IF NOT EXISTS game_meta_cache (
		id TEXT PRIMARY KEY,
		service TEXT NOT NULL,
		app_id INTEGER DEFAULT 0,
		name TEXT NOT NULL,
		platform TEXT DEFAULT 'PC',
		last_updated DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS update_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		app_id INTEGER NOT NULL,
		service TEXT NOT NULL DEFAULT 'steam',
		game_title TEXT NOT NULL,
		change_number INTEGER DEFAULT 0,
		manifest_id TEXT DEFAULT '',
		status TEXT DEFAULT 'Verified',
		message TEXT NOT NULL,
		discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_update_logs_time ON update_logs(discovered_at DESC);
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

// seedDefaults seeds SQLite tables with initial configurations and default scheduler rules
func (db *DB) seedDefaults() error {
	// 1. Seed default configuration settings if absent
	defaultSettings := map[string]string{
		"dns_upstreams":        "1.1.1.1:53,8.8.8.8:53",
		"dns_listen_addr":      ":53",
		"dns_enabled":          "true",
		"dns_intercept_ipv4":   "127.0.0.1",
		"storage_path":         "/data/cache",
		"manifest_dir":         "/data/manifests",
		"depotcache_dir":       "/data/steam_depotcache",
		"server_listen_addr":   ":8080",
		"proxy_listen_addr":    ":80",
		"cache_max_size_bytes": "2199023255552", // 2 TB
		"cache_min_free_disk":  "53687091200",   // 50 GB
		"cache_chunk_size":             "1048576",       // 1 MB
		"scheduler_enabled":            "true",
		"service_steam_auto_update":    "true",
		"service_epic_auto_update":     "true",
		"service_blizzard_auto_update": "true",
		"service_riot_auto_update":     "true",
		"service_xbox_auto_update":     "true",
	}

	for k, v := range defaultSettings {
		_, _ = db.Exec("INSERT OR IGNORE INTO config_settings (key, value) VALUES (?, ?)", k, v)
	}

	// 2. Seed default scheduler rules if not exists
	defaultRules := []SchedulerRule{
		{
			ID:             "steam-update-check",
			Name:           "Steam Game Update Check",
			CronExpression: "0 */10 * * * *", // Every 10 minutes
			TaskType:       "steam_update_check",
			Service:        "steam",
			Enabled:        true,
			NextRun:        time.Now().Add(10 * time.Minute),
		},
		{
			ID:             "cache-cleanup",
			Name:           "Daily Cache Storage Sync",
			CronExpression: "0 0 3 * * *", // Daily at 3 AM
			TaskType:       "cache_sync",
			Service:        "all",
			Enabled:        true,
			NextRun:        time.Now().Add(24 * time.Hour),
		},
	}

	for _, r := range defaultRules {
		_, _ = db.Exec(`
			INSERT OR IGNORE INTO scheduler_rules (id, name, cron_expression, task_type, service, enabled, next_run)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, r.ID, r.Name, r.CronExpression, r.TaskType, r.Service, r.Enabled, r.NextRun)
	}

	return nil
}

// GetSchedulerRules returns all rules stored in SQLite
func (db *DB) GetSchedulerRules() ([]SchedulerRule, error) {
	rows, err := db.Query("SELECT id, name, cron_expression, task_type, service, enabled, last_run, next_run FROM scheduler_rules ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []SchedulerRule
	for rows.Next() {
		var r SchedulerRule
		var lastRun, nextRun sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.CronExpression, &r.TaskType, &r.Service, &r.Enabled, &lastRun, &nextRun); err != nil {
			continue
		}
		if lastRun.Valid {
			r.LastRun = lastRun.Time
		}
		if nextRun.Valid {
			r.NextRun = nextRun.Time
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// UpsertSchedulerRule inserts or updates a full scheduler rule record
func (db *DB) UpsertSchedulerRule(rule SchedulerRule) error {
	query := `
	INSERT INTO scheduler_rules (id, name, cron_expression, task_type, service, enabled, last_run, next_run)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		cron_expression = excluded.cron_expression,
		task_type = excluded.task_type,
		service = excluded.service,
		enabled = excluded.enabled,
		last_run = CASE WHEN excluded.last_run IS NOT NULL THEN excluded.last_run ELSE scheduler_rules.last_run END,
		next_run = CASE WHEN excluded.next_run IS NOT NULL THEN excluded.next_run ELSE scheduler_rules.next_run END;
	`
	var lastRunVal, nextRunVal interface{}
	if !rule.LastRun.IsZero() {
		lastRunVal = rule.LastRun
	}
	if !rule.NextRun.IsZero() {
		nextRunVal = rule.NextRun
	}
	_, err := db.Exec(query, rule.ID, rule.Name, rule.CronExpression, rule.TaskType, rule.Service, rule.Enabled, lastRunVal, nextRunVal)
	return err
}

// UpdateSchedulerRule updates the cron expression and enabled state of a rule
func (db *DB) UpdateSchedulerRule(id, cronExpr string, enabled bool) error {
	query := `
	UPDATE scheduler_rules
	SET cron_expression = ?, enabled = ?
	WHERE id = ?;
	`
	_, err := db.Exec(query, cronExpr, enabled, id)
	return err
}

// UpdateSchedulerRuleRunTimes updates last_run and next_run timestamps
func (db *DB) UpdateSchedulerRuleRunTimes(id string, lastRun, nextRun time.Time) error {
	query := `
	UPDATE scheduler_rules
	SET last_run = ?, next_run = ?
	WHERE id = ?;
	`
	var lastRunVal, nextRunVal interface{}
	if !lastRun.IsZero() {
		lastRunVal = lastRun
	}
	if !nextRun.IsZero() {
		nextRunVal = nextRun
	}
	_, err := db.Exec(query, lastRunVal, nextRunVal, id)
	return err
}

// GetUpdateLogs returns the most recent update log entries from SQLite
func (db *DB) GetUpdateLogs(limit int) ([]UpdateLogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`
		SELECT id, app_id, service, game_title, change_number, manifest_id, status, message, discovered_at
		FROM update_logs
		ORDER BY discovered_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []UpdateLogEntry
	for rows.Next() {
		var l UpdateLogEntry
		var discAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.AppID, &l.Service, &l.GameTitle, &l.ChangeNumber, &l.ManifestID, &l.Status, &l.Message, &discAt); err != nil {
			continue
		}
		if discAt.Valid {
			l.DiscoveredAt = discAt.Time
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// AddUpdateLog records a discovered game update or check in SQLite
func (db *DB) AddUpdateLog(entry UpdateLogEntry) error {
	query := `
	INSERT INTO update_logs (app_id, service, game_title, change_number, manifest_id, status, message, discovered_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP);
	`
	status := entry.Status
	if status == "" {
		status = "Verified"
	}
	_, err := db.Exec(query, entry.AppID, entry.Service, entry.GameTitle, entry.ChangeNumber, entry.ManifestID, status, entry.Message)
	return err
}

// GetSteamSession retrieves the active Steam session if present
func (db *DB) GetSteamSession() (*SteamSession, error) {
	var sess SteamSession
	var isActive interface{}
	var updatedAtStr sql.NullString
	err := db.QueryRow(`
		SELECT username, COALESCE(steam_id, ''), COALESCE(access_token, ''), COALESCE(refresh_token, ''), COALESCE(session_cookies, ''), COALESCE(client_id, ''), is_active, updated_at
		FROM steam_session WHERE id = 1
	`).Scan(&sess.Username, &sess.SteamID, &sess.AccessToken, &sess.RefreshToken, &sess.SessionCookies, &sess.ClientID, &isActive, &updatedAtStr)
	if err != nil {
		return nil, err
	}
	switch v := isActive.(type) {
	case bool:
		sess.IsActive = v
	case int64:
		sess.IsActive = (v == 1)
	case int:
		sess.IsActive = (v == 1)
	case string:
		sess.IsActive = (v == "1" || strings.ToLower(v) == "true")
	default:
		sess.IsActive = false
	}
	if updatedAtStr.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", updatedAtStr.String); err == nil {
			sess.UpdatedAt = t
		} else if t, err := time.Parse(time.RFC3339, updatedAtStr.String); err == nil {
			sess.UpdatedAt = t
		}
	}
	return &sess, nil
}

// SaveSteamSession saves or updates the active Steam session
func (db *DB) SaveSteamSession(sess *SteamSession) error {
	query := `
	INSERT INTO steam_session (id, username, steam_id, access_token, refresh_token, session_cookies, client_id, is_active, updated_at)
	VALUES (1, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(id) DO UPDATE SET 
		username = excluded.username,
		steam_id = excluded.steam_id,
		access_token = excluded.access_token,
		refresh_token = excluded.refresh_token,
		session_cookies = excluded.session_cookies,
		client_id = excluded.client_id,
		is_active = excluded.is_active,
		updated_at = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, sess.Username, sess.SteamID, sess.AccessToken, sess.RefreshToken, sess.SessionCookies, sess.ClientID, sess.IsActive)
	return err
}

// ClearSteamSession logs out the Steam session
func (db *DB) ClearSteamSession() error {
	_, err := db.Exec("UPDATE steam_session SET is_active = 0, access_token = '', refresh_token = '', session_cookies = '' WHERE id = 1")
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
	if appID == 0 && depotID > 0 {
		_ = db.QueryRow("SELECT app_id FROM depot_meta_cache WHERE depot_id = ? AND app_id > 0 LIMIT 1", depotID).Scan(&appID)
	}

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
		SELECT 
			w.app_id, 
			w.service, 
			w.name, 
			COALESCE(w.platform, ''), 
			w.auto_update, 
			COALESCE(w.current_version, ''), 
			w.change_number, 
			w.status, 
			w.installed_at, 
			w.last_checked,
			COALESCE((SELECT SUM(c.size_bytes) FROM chunk_cache c WHERE c.app_id = w.app_id AND c.service = w.service), 0) AS size_bytes
		FROM watched_apps w 
		ORDER BY w.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []WatchedApp
	for rows.Next() {
		var a WatchedApp
		if err := rows.Scan(
			&a.AppID, 
			&a.Service, 
			&a.Name, 
			&a.Platform, 
			&a.AutoUpdate, 
			&a.CurrentVersion, 
			&a.ChangeNumber, 
			&a.Status, 
			&a.InstalledAt, 
			&a.LastChecked,
			&a.SizeBytes,
		); err == nil {
			apps = append(apps, a)
		}
	}
	return apps, nil
}

// UpsertWatchedApp inserts or updates a watched title
func (db *DB) UpsertWatchedApp(app WatchedApp) error {
	query := `
	INSERT INTO watched_apps (app_id, service, name, platform, auto_update, current_version, change_number, status, last_checked)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(app_id, service) DO UPDATE SET
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE watched_apps.name END,
		platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE watched_apps.platform END,
		auto_update = excluded.auto_update,
		current_version = CASE WHEN excluded.current_version != '' THEN excluded.current_version ELSE watched_apps.current_version END,
		change_number = CASE WHEN excluded.change_number > 0 THEN excluded.change_number ELSE watched_apps.change_number END,
		status = CASE WHEN excluded.status != '' THEN excluded.status ELSE watched_apps.status END,
		last_checked = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, app.AppID, app.Service, app.Name, app.Platform, app.AutoUpdate, app.CurrentVersion, app.ChangeNumber, app.Status)
	return err
}

// UpdateWatchedAppAutoUpdate updates the auto_update flag for a watched app
func (db *DB) UpdateWatchedAppAutoUpdate(appID int64, service string, autoUpdate bool) error {
	if db == nil || appID <= 0 {
		return nil
	}
	if service == "" {
		service = "steam"
	}
	_, err := db.Exec("UPDATE watched_apps SET auto_update = ? WHERE app_id = ? AND service = ?", autoUpdate, appID, service)
	return err
}

// GetServiceAutoUpdate returns whether auto-update is enabled for a specific service
func (db *DB) GetServiceAutoUpdate(service string) bool {
	if db == nil {
		return true
	}
	key := fmt.Sprintf("service_%s_auto_update", strings.ToLower(service))
	val := db.GetSetting(key, "true")
	return val == "true" || val == "1"
}

// SetServiceAutoUpdate updates the auto-update flag for a specific service
func (db *DB) SetServiceAutoUpdate(service string, enabled bool) error {
	if db == nil {
		return nil
	}
	key := fmt.Sprintf("service_%s_auto_update", strings.ToLower(service))
	val := "false"
	if enabled {
		val = "true"
	}
	return db.SetSetting(key, val)
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

// GetDepotIDsForApp retrieves all known depot IDs associated with an app
func (db *DB) GetDepotIDsForApp(appID int64, service string) ([]int64, error) {
	if db == nil {
		return nil, nil
	}
	if service == "" {
		service = "steam"
	}

	var query string
	var args []interface{}
	if appID > 0 {
		query = `
			SELECT depot_id FROM depot_meta_cache WHERE app_id = ? AND service = ?
			UNION
			SELECT DISTINCT depot_id FROM chunk_cache WHERE app_id = ? AND service = ? AND depot_id > 0;
		`
		args = []interface{}{appID, service, appID, service}
	} else {
		query = `SELECT DISTINCT depot_id FROM chunk_cache WHERE service = ? AND depot_id > 0;`
		args = []interface{}{service}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var depotIDs []int64
	for rows.Next() {
		var dID int64
		if err := rows.Scan(&dID); err == nil {
			depotIDs = append(depotIDs, dID)
		}
	}
	return depotIDs, nil
}

// GetChunkPathsForApp returns the file paths of all cached chunks for an app (or its depots)
func (db *DB) GetChunkPathsForApp(appID int64, service string) ([]string, error) {
	if db == nil {
		return nil, nil
	}
	if service == "" {
		service = "steam"
	}

	var query string
	var args []interface{}
	if appID > 0 {
		query = `
			SELECT file_path FROM chunk_cache 
			WHERE (app_id = ? AND service = ?) 
			   OR (service = ? AND depot_id IN (
					SELECT depot_id FROM depot_meta_cache WHERE app_id = ?
					UNION
					SELECT DISTINCT depot_id FROM chunk_cache WHERE app_id = ? AND depot_id > 0
			   ));
		`
		args = []interface{}{appID, service, service, appID, appID}
	} else {
		query = `SELECT file_path FROM chunk_cache WHERE service = ? AND app_id = 0;`
		args = []interface{}{service}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil && p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// DeleteChunksAndManifestsForApp purges all database records for chunks, manifests, and depots for an app
func (db *DB) DeleteChunksAndManifestsForApp(appID int64, service string) error {
	if db == nil {
		return nil
	}
	if service == "" {
		service = "steam"
	}

	if appID > 0 {
		// 1. Delete from depot_manifest_chunks
		_, _ = db.Exec(`
			DELETE FROM depot_manifest_chunks 
			WHERE depot_id IN (
				SELECT depot_id FROM depot_meta_cache WHERE app_id = ?
				UNION
				SELECT DISTINCT depot_id FROM chunk_cache WHERE app_id = ? AND depot_id > 0
			);
		`, appID, appID)

		// 2. Delete from chunk_cache
		_, _ = db.Exec(`
			DELETE FROM chunk_cache 
			WHERE (app_id = ? AND service = ?) 
			   OR (service = ? AND depot_id IN (
					SELECT depot_id FROM depot_meta_cache WHERE app_id = ?
					UNION
					SELECT DISTINCT depot_id FROM chunk_cache WHERE app_id = ? AND depot_id > 0
			   ));
		`, appID, service, service, appID, appID)

		// 3. Delete from watched_apps
		_, _ = db.Exec("DELETE FROM watched_apps WHERE app_id = ? AND service = ?", appID, service)

		// 4. Delete from depot_meta_cache
		_, _ = db.Exec("DELETE FROM depot_meta_cache WHERE app_id = ? AND service = ?", appID, service)
	} else {
		_, _ = db.Exec("DELETE FROM chunk_cache WHERE service = ? AND app_id = 0", service)
	}

	return nil
}


// GetConnectedCredentials returns map of connected services to usernames
func (db *DB) GetConnectedCredentials() (map[string]string, error) {
	creds := make(map[string]string)
	if db == nil {
		return creds, nil
	}
	rows, err := db.Query("SELECT service, username FROM service_credentials")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var svc, user string
			if err := rows.Scan(&svc, &user); err == nil {
				creds[svc] = user
			}
		}
	}
	return creds, nil
}

// SetServiceCredential saves credentials for a game platform service
func (db *DB) SetServiceCredential(service, username, token string) error {
	if db == nil {
		return nil
	}
	query := `
	INSERT INTO service_credentials (service, username, token, connected_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(service) DO UPDATE SET
		username = excluded.username,
		token = excluded.token,
		connected_at = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, service, username, token)
	return err
}

// RemoveServiceCredential removes credentials for a game platform service
func (db *DB) RemoveServiceCredential(service string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec("DELETE FROM service_credentials WHERE service = ?", service)
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

	query := `
	SELECT 
		sub.app_id,
		sub.service,
		COALESCE(
			(SELECT d.game_title FROM depot_meta_cache d WHERE d.app_id = sub.app_id AND d.game_title != '' LIMIT 1),
			(SELECT g.name FROM game_meta_cache g WHERE g.service = sub.service AND g.app_id = sub.app_id AND g.name != '' LIMIT 1),
			(SELECT d2.game_title FROM depot_meta_cache d2 WHERE d2.depot_id = sub.sample_depot AND d2.game_title != '' LIMIT 1),
			(SELECT g2.name FROM game_meta_cache g2 WHERE g2.service = sub.service AND g2.name != '' LIMIT 1),
			CASE WHEN sub.service = 'steam' AND sub.app_id > 0 THEN 'Steam App ' || sub.app_id ELSE 'Game Asset' END
		) as name,
		COALESCE(
			(SELECT d.platform FROM depot_meta_cache d WHERE d.depot_id IN (SELECT c2.depot_id FROM chunk_cache c2 WHERE c2.app_id = sub.app_id OR c2.depot_id = sub.sample_depot) AND d.platform != '' LIMIT 1),
			(SELECT d.platform FROM depot_meta_cache d WHERE d.app_id = sub.app_id AND d.platform != '' LIMIT 1),
			(SELECT g.platform FROM game_meta_cache g WHERE g.service = sub.service AND g.app_id = sub.app_id AND g.platform != '' LIMIT 1),
			'PC'
		) as platform,
		COALESCE(
			(SELECT SUM(DISTINCT d.total_size) FROM depot_meta_cache d WHERE d.app_id = sub.app_id),
			0
		) as total_size,
		COUNT(sub.chunk_id) as cached_chunks,
		COALESCE(SUM(sub.size_bytes), 0) as cached_bytes,
		COALESCE(MAX(sub.downloaded_at), CURRENT_TIMESTAMP) as last_seen
	FROM (
		SELECT 
			c.chunk_id,
			c.service,
			c.size_bytes,
			c.downloaded_at,
			c.depot_id as sample_depot,
			COALESCE(
				NULLIF(c.app_id, 0),
				(SELECT d.app_id FROM depot_meta_cache d WHERE d.depot_id = c.depot_id AND d.app_id > 0 LIMIT 1),
				c.depot_id,
				0
			) as app_id
		FROM chunk_cache c
	) sub
	WHERE (sub.app_id > 0 AND sub.app_id NOT IN (SELECT app_id FROM watched_apps WHERE service = sub.service))
	   OR (sub.app_id = 0 AND sub.sample_depot NOT IN (SELECT app_id FROM watched_apps WHERE service = sub.service))
	GROUP BY sub.service, sub.app_id
	HAVING COUNT(sub.chunk_id) > 0
	ORDER BY last_seen DESC;
	`

	rows, err := db.Query(query)
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
		SELECT depot_id, app_id, service, game_title, platform, total_size, COALESCE(manifest_id, 0), last_updated 
		FROM depot_meta_cache WHERE depot_id = ?
	`, depotID).Scan(&d.DepotID, &d.AppID, &d.Service, &d.GameTitle, &d.Platform, &d.TotalSize, &d.ManifestID, &lu)
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
		INSERT INTO depot_meta_cache (depot_id, app_id, service, game_title, platform, total_size, manifest_id, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(depot_id) DO UPDATE SET
			app_id = excluded.app_id,
			service = excluded.service,
			game_title = excluded.game_title,
			platform = excluded.platform,
			total_size = excluded.total_size,
			manifest_id = CASE WHEN excluded.manifest_id > 0 THEN excluded.manifest_id ELSE depot_meta_cache.manifest_id END,
			last_updated = CURRENT_TIMESTAMP;
	`
	_, err := db.Exec(query, meta.DepotID, meta.AppID, meta.Service, meta.GameTitle, meta.Platform, meta.TotalSize, meta.ManifestID)
	if err == nil && meta.AppID > 0 {
		_, _ = db.Exec("UPDATE chunk_cache SET app_id = ? WHERE depot_id = ? AND (app_id = 0 OR app_id IS NULL)", meta.AppID, meta.DepotID)
	}
	return err
}

// SaveManifestChunks persists parsed depot manifest chunks in batch
func (db *DB) SaveManifestChunks(depotID, manifestID int64, chunks []ManifestChunkRecord) error {
	if db == nil || len(chunks) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO depot_manifest_chunks (depot_id, manifest_id, chunk_id, size_bytes, original_size, compressed_size)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(depot_id, chunk_id) DO UPDATE SET
			manifest_id = excluded.manifest_id,
			size_bytes = excluded.size_bytes,
			original_size = excluded.original_size,
			compressed_size = excluded.compressed_size;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range chunks {
		mID := manifestID
		if c.ManifestID > 0 {
			mID = c.ManifestID
		}
		dID := depotID
		if c.DepotID > 0 {
			dID = c.DepotID
		}
		_, err := stmt.Exec(dID, mID, c.ChunkID, c.SizeBytes, c.OriginalSize, c.CompressedSize)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetManifestChunks returns all parsed chunk metadata for a given depot
func (db *DB) GetManifestChunks(depotID int64) ([]ManifestChunkRecord, error) {
	if db == nil || depotID <= 0 {
		return nil, nil
	}

	rows, err := db.Query("SELECT depot_id, manifest_id, chunk_id, size_bytes, original_size, compressed_size FROM depot_manifest_chunks WHERE depot_id = ?", depotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ManifestChunkRecord
	for rows.Next() {
		var c ManifestChunkRecord
		if err := rows.Scan(&c.DepotID, &c.ManifestID, &c.ChunkID, &c.SizeBytes, &c.OriginalSize, &c.CompressedSize); err == nil {
			chunks = append(chunks, c)
		}
	}
	return chunks, nil
}

// HasManifestChunks checks if chunks are already indexed for a depot
func (db *DB) HasManifestChunks(depotID int64) bool {
	if db == nil || depotID <= 0 {
		return false
	}
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM depot_manifest_chunks WHERE depot_id = ?", depotID).Scan(&count)
	return count > 0
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

// GetWatchedAppPlatform returns the configured platform string for a watched app
func (db *DB) GetWatchedAppPlatform(appID int64, service string) (string, error) {
	if db == nil || appID <= 0 {
		return "", sql.ErrNoRows
	}
	var platform string
	err := db.QueryRow("SELECT COALESCE(platform, '') FROM watched_apps WHERE app_id = ? AND service = ? LIMIT 1", appID, service).Scan(&platform)
	return platform, err
}

// MergePlatformStrings combines two comma-separated platform strings into a clean, canonical comma-separated list
func MergePlatformStrings(existing, newPlat string) string {
	platformsSet := make(map[string]bool)
	var ordered []string

	add := func(s string) {
		for _, part := range strings.Split(s, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			pNorm := p
			pLower := strings.ToLower(p)
			if strings.Contains(pLower, "win") {
				pNorm = "Windows"
			} else if strings.Contains(pLower, "mac") || strings.Contains(pLower, "apple") || strings.Contains(pLower, "darwin") || strings.Contains(pLower, "osx") {
				pNorm = "Mac"
			} else if strings.Contains(pLower, "linux") {
				pNorm = "Linux"
			}
			if !platformsSet[pNorm] {
				platformsSet[pNorm] = true
				ordered = append(ordered, pNorm)
			}
		}
	}

	add(existing)
	add(newPlat)

	preferred := []string{"Windows", "Mac", "Linux"}
	var result []string
	for _, pref := range preferred {
		if platformsSet[pref] {
			result = append(result, pref)
			delete(platformsSet, pref)
		}
	}
	for _, o := range ordered {
		if platformsSet[o] {
			result = append(result, o)
			delete(platformsSet, o)
		}
	}
	return strings.Join(result, ", ")
}


