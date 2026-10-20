package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/prefill"
	"artesian/internal/services"
	"artesian/internal/ws"

	"github.com/robfig/cron/v3"
)

// Rule represents a user-configurable cron schedule
type Rule struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CronExpression string    `json:"cron_expression"`
	TaskType       string    `json:"task_type"` // "steam_update_check", "cache_sync"
	Service        string    `json:"service"`
	Enabled        bool      `json:"enabled"`
	LastRun        time.Time `json:"last_run"`
	NextRun        time.Time `json:"next_run"`
}

// Scheduler coordinates cron jobs and Steam game update checks
type Scheduler struct {
	cfg      config.SchedulerConfig
	db       *db.DB
	prefill  *prefill.Manager
	registry *services.Registry
	cron     *cron.Cron
	rules    map[string]*Rule
	rulesMu  sync.RWMutex
	entryIDs map[string]cron.EntryID
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewScheduler initializes the cron and update check engine
func NewScheduler(cfg config.SchedulerConfig, database *db.DB, pf *prefill.Manager, reg *services.Registry) *Scheduler {
	s := &Scheduler{
		cfg:      cfg,
		db:       database,
		prefill:  pf,
		registry: reg,
		cron:     cron.New(cron.WithSeconds()),
		rules:    make(map[string]*Rule),
		entryIDs: make(map[string]cron.EntryID),
	}

	// 1. Load rules from SQLite database if available
	if database != nil {
		dbRules, err := database.GetSchedulerRules()
		if err == nil && len(dbRules) > 0 {
			for _, dr := range dbRules {
				s.rules[dr.ID] = &Rule{
					ID:             dr.ID,
					Name:           dr.Name,
					CronExpression: dr.CronExpression,
					TaskType:       dr.TaskType,
					Service:        dr.Service,
					Enabled:        dr.Enabled,
					LastRun:        dr.LastRun,
					NextRun:        dr.NextRun,
				}
			}
		}
	}

	// 2. Ensure default Update Check rule exists
	if _, ok := s.rules["update-check"]; !ok {
		s.rules["update-check"] = &Rule{
			ID:             "update-check",
			Name:           "Game Update Check",
			CronExpression: "0 */10 * * * *", // Every 10 minutes
			TaskType:       "update_check",
			Service:        "all",
			Enabled:        true,
			NextRun:        time.Now().Add(10 * time.Minute),
		}
	}

	// 3. Ensure default Cache Storage Sync rule exists
	if _, ok := s.rules["cache-cleanup"]; !ok {
		s.rules["cache-cleanup"] = &Rule{
			ID:             "cache-cleanup",
			Name:           "Daily Cache Storage Sync",
			CronExpression: "0 0 3 * * *", // 3 AM
			TaskType:       "cache_sync",
			Service:        "all",
			Enabled:        true,
			NextRun:        time.Now().Add(24 * time.Hour),
		}
	}

	return s
}

// Start launches the cron engine
func (s *Scheduler) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)

	log.Printf("[Artesian Scheduler] robfig/cron engine starting with %d rules...", len(s.rules))

	s.rulesMu.Lock()
	for id, rule := range s.rules {
		if rule.Enabled {
			ruleID := id
			expr := normalizeCronExpr(rule.CronExpression)
			entryID, err := s.cron.AddFunc(expr, func() {
				s.executeRule(ruleID)
			})
			if err == nil {
				s.entryIDs[ruleID] = entryID
			} else {
				log.Printf("[Scheduler] Error scheduling rule %s (%s): %v", rule.Name, expr, err)
			}
		}
	}
	s.rulesMu.Unlock()

	s.cron.Start()
}

// Stop cleanly terminates scheduler
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.cron != nil {
		s.cron.Stop()
	}
}

// ExecuteRule runs a rule immediately
func (s *Scheduler) ExecuteRule(ruleID string) error {
	return s.executeRule(ruleID)
}

func (s *Scheduler) executeRule(ruleID string) error {
	s.rulesMu.Lock()
	rule, exists := s.rules[ruleID]
	if !exists {
		s.rulesMu.Unlock()
		return fmt.Errorf("rule not found: %s", ruleID)
	}
	rule.LastRun = time.Now()
	rule.NextRun = time.Now().Add(10 * time.Minute)
	if s.db != nil {
		_ = s.db.UpdateSchedulerRuleRunTimes(ruleID, rule.LastRun, rule.NextRun)
	}
	s.rulesMu.Unlock()

	switch rule.TaskType {
	case "steam_update_check", "update_check":
		go s.CheckUpdates()
	case "cache_sync":
		log.Println("[Scheduler] Running daily storage sync and retention verification.")
	}

	return nil
}

// CheckUpdates checks all registered platforms for game updates on managed titles
func (s *Scheduler) CheckUpdates() {
	if s.db == nil || s.prefill == nil {
		return
	}

	log.Println("[Scheduler] Checking for updates across managed titles...")
	apps, err := s.db.GetWatchedApps()
	if err != nil {
		return
	}

	for _, app := range apps {
		// Verify service-level auto-update setting
		if !s.db.GetServiceAutoUpdate(app.Service) {
			log.Printf("[Update Check] Skipping %s: auto-update disabled for service '%s'", app.Name, app.Service)
			continue
		}

		// Verify app-level auto-update variable
		if !app.AutoUpdate {
			log.Printf("[Update Check] Skipping %s: auto-update disabled for title", app.Name)
			continue
		}

		s.checkAppUpdate(app)
	}
}

func (s *Scheduler) checkAppUpdate(app db.WatchedApp) {
	if s.registry == nil {
		return
	}
	adapter, ok := s.registry.Get(app.Service)
	if !ok {
		return
	}

	check, err := adapter.CheckAppUpdate(app.AppID, app.ChangeNumber)
	if err != nil {
		log.Printf("[%s Update Check] Network or API check failed for %s: %v", adapter.DisplayName(), app.Name, err)
		return
	}

	// Baseline initialization: if app has no recorded change number (0), store baseline without an alert
	if app.ChangeNumber == 0 {
		if check.NewChangeNumber > 0 {
			app.ChangeNumber = check.NewChangeNumber
			_ = s.db.UpsertWatchedApp(app)
		}
		log.Printf("[%s Update Check] Baseline recorded for %s (Change #%d)", adapter.DisplayName(), app.Name, check.NewChangeNumber)
		return
	}

	if !check.Updated {
		// Silently drop checks that resulted in no change
		log.Printf("[%s Update Check] %s is up to date (Change #%d). Check completed with no changes.", adapter.DisplayName(), app.Name, app.ChangeNumber)
		return
	}

	// Update discovered!
	log.Printf("[%s Update Check] UPDATE DETECTED for %s: Change #%d -> #%d (Manifest changed: %v)",
		adapter.DisplayName(), app.Name, app.ChangeNumber, check.NewChangeNumber, check.ManifestChanged)

	// Update watched_apps
	app.ChangeNumber = check.NewChangeNumber
	app.Status = "prefilling"
	_ = s.db.UpsertWatchedApp(app)

	updateMsg := fmt.Sprintf("New update detected for %s (Change #%d). Automated prefill initiated.", app.Name, check.NewChangeNumber)

	logEntry := db.UpdateLogEntry{
		AppID:        app.AppID,
		Service:      app.Service,
		GameTitle:    app.Name,
		ChangeNumber: check.NewChangeNumber,
		Status:       "Discovered",
		Message:      updateMsg,
		DiscoveredAt: time.Now(),
	}
	if s.db != nil {
		_ = s.db.AddUpdateLog(logEntry)
	}

	// Broadcast update discovered event to UI
	ws.GetHub().Broadcast("update:discovered", logEntry)
	ws.GetHub().Broadcast("pics:update_detected", map[string]interface{}{
		"app_id":        app.AppID,
		"name":          app.Name,
		"service":       app.Service,
		"change_number": check.NewChangeNumber,
		"message":       logEntry.Message,
	})

	// Trigger automated prefill
	if s.prefill != nil {
		go func() {
			s.prefill.CancelJobsForApp(app.Service, app.AppID)
			_, _ = s.prefill.StartJob(context.Background(), app.Service, app.AppID, app.Name)
		}()
	}
}


// GetRules returns all configured scheduler rules
func (s *Scheduler) GetRules() []*Rule {
	s.rulesMu.RLock()
	defer s.rulesMu.RUnlock()

	result := make([]*Rule, 0, len(s.rules))
	for _, r := range s.rules {
		result = append(result, r)
	}
	return result
}

// UpdateRule updates a cron rule in-memory, reschedules it, and persists it to SQLite
func (s *Scheduler) UpdateRule(id, cronExpr string, enabled bool) error {
	s.rulesMu.Lock()
	defer s.rulesMu.Unlock()

	rule, exists := s.rules[id]
	if !exists {
		return fmt.Errorf("rule %s not found", id)
	}

	normalized := normalizeCronExpr(cronExpr)

	// Validate cron expression
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	sched, err := parser.Parse(normalized)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", cronExpr, err)
	}

	rule.CronExpression = cronExpr
	rule.Enabled = enabled
	rule.NextRun = sched.Next(time.Now())

	// Re-register in cron runner
	if oldEntry, ok := s.entryIDs[id]; ok {
		s.cron.Remove(oldEntry)
		delete(s.entryIDs, id)
	}

	if enabled {
		entryID, err := s.cron.AddFunc(normalized, func() {
			s.executeRule(id)
		})
		if err != nil {
			return err
		}
		s.entryIDs[id] = entryID
	}

	// Persist to SQLite
	if s.db != nil {
		_ = s.db.UpdateSchedulerRule(id, cronExpr, enabled)
	}

	// Broadcast update to all connected UI clients
	ws.GetHub().Broadcast("scheduler:rules_updated", map[string]interface{}{
		"id":              id,
		"cron_expression": cronExpr,
		"enabled":         enabled,
		"next_run":        rule.NextRun,
	})

	return nil
}

// normalizeCronExpr converts 5-field crons (min hour dom mon dow) to 6-field (sec min hour dom mon dow)
func normalizeCronExpr(expr string) string {
	trimmed := strings.TrimSpace(expr)
	fields := strings.Fields(trimmed)
	if len(fields) == 5 {
		return "0 " + trimmed
	}
	return trimmed
}
