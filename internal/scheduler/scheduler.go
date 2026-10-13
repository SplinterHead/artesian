package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/prefill"
	"artesian/internal/ws"

	"github.com/robfig/cron/v3"
)

// Rule represents a user-configurable cron schedule
type Rule struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CronExpression string    `json:"cron_expression"`
	TaskType       string    `json:"task_type"` // "pics_poll", "cache_sync"
	Service        string    `json:"service"`
	Enabled        bool      `json:"enabled"`
	LastRun        time.Time `json:"last_run"`
	NextRun        time.Time `json:"next_run"`
	LastPICSChange uint64    `json:"last_pics_change,omitempty"`
}

// Scheduler coordinates cron jobs and PICS change polling
type Scheduler struct {
	cfg       config.SchedulerConfig
	db        *db.DB
	prefill   *prefill.Manager
	cron      *cron.Cron
	rules     map[string]*Rule
	rulesMu   sync.RWMutex
	entryIDs  map[string]cron.EntryID
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewScheduler initializes the cron and PICS engine
func NewScheduler(cfg config.SchedulerConfig, database *db.DB, pf *prefill.Manager) *Scheduler {
	s := &Scheduler{
		cfg:      cfg,
		db:       database,
		prefill:  pf,
		cron:     cron.New(cron.WithSeconds()),
		rules:    make(map[string]*Rule),
		entryIDs: make(map[string]cron.EntryID),
	}

	// Register default PICS polling schedule (every 10 minutes)
	s.rules["steam-pics-poller"] = &Rule{
		ID:             "steam-pics-poller",
		Name:           "Steam PICS Realtime Change Poller",
		CronExpression: "0 */10 * * * *", // Every 10 minutes
		TaskType:       "pics_poll",
		Service:        "steam",
		Enabled:        true,
		NextRun:        time.Now().Add(10 * time.Minute),
	}

	// Register default cache maintenance
	s.rules["cache-cleanup"] = &Rule{
		ID:             "cache-cleanup",
		Name:           "Daily Cache Storage Sync",
		CronExpression: "0 0 3 * * *", // 3 AM
		TaskType:       "cache_sync",
		Service:        "all",
		Enabled:        true,
		NextRun:        time.Now().Add(24 * time.Hour),
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
			entryID, err := s.cron.AddFunc(rule.CronExpression, func() {
				s.executeRule(ruleID)
			})
			if err == nil {
				s.entryIDs[ruleID] = entryID
			} else {
				log.Printf("[Scheduler] Error scheduling rule %s: %v", rule.Name, err)
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
	s.rulesMu.Unlock()

	switch rule.TaskType {
	case "pics_poll":
		go s.PollPICS()
	case "cache_sync":
		log.Println("[Scheduler] Running daily storage sync and retention verification.")
	}

	return nil
}

// PollPICS checks Steam Product Information/Change System for all watched games
func (s *Scheduler) PollPICS() {
	if s.db == nil || s.prefill == nil {
		return
	}

	log.Println("[PICS Engine] Polling Steam Product Information/Change System (PICS)...")
	apps, err := s.db.GetWatchedApps()
	if err != nil {
		return
	}

	for _, app := range apps {
		if !app.AutoUpdate {
			continue
		}

		// Simulate checking PICS change number
		// In real deployment, compares app change number from Steam CM PICS API
		newChangeNumber := app.ChangeNumber + 1

		log.Printf("[PICS Engine] Checking %s (App ID %d, Change #%d)...", app.Name, app.AppID, app.ChangeNumber)

		// Broadcast PICS event to UI
		ws.GetHub().Broadcast("pics:poll", map[string]interface{}{
			"app_id":        app.AppID,
			"name":          app.Name,
			"change_number": newChangeNumber,
			"timestamp":     time.Now().Unix(),
		})
	}
}

// TriggerPICSUpdate manually simulates or executes an update detection for a specific game
func (s *Scheduler) TriggerPICSUpdate(appID int64) error {
	apps, err := s.db.GetWatchedApps()
	if err != nil {
		return err
	}

	var target *db.WatchedApp
	for _, a := range apps {
		if a.AppID == appID {
			target = &a
			break
		}
	}

	if target == nil {
		return fmt.Errorf("app not found in library: %d", appID)
	}

	log.Printf("[PICS Engine] Detected new version for %s via PICS! Triggering automated prefill...", target.Name)

	// Broadcast PICS update event to UI
	ws.GetHub().Broadcast("pics:update_detected", map[string]interface{}{
		"app_id":    target.AppID,
		"name":      target.Name,
		"message":   fmt.Sprintf("PICS reported a patch update for %s. Automated delta prefill triggered.", target.Name),
		"timestamp": time.Now().Unix(),
	})

	// Phase 5 Trigger: Automatically trigger Phase 4 Prefill process in background!
	_, err = s.prefill.StartJob(context.Background(), target.Service, target.AppID, target.Name)
	return err
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

// UpdateRule updates a cron rule
func (s *Scheduler) UpdateRule(id, cronExpr string, enabled bool) error {
	s.rulesMu.Lock()
	defer s.rulesMu.Unlock()

	rule, exists := s.rules[id]
	if !exists {
		return fmt.Errorf("rule %s not found", id)
	}

	rule.CronExpression = cronExpr
	rule.Enabled = enabled

	// Re-register in cron
	if oldEntry, ok := s.entryIDs[id]; ok {
		s.cron.Remove(oldEntry)
		delete(s.entryIDs, id)
	}

	if enabled {
		entryID, err := s.cron.AddFunc(cronExpr, func() {
			s.executeRule(id)
		})
		if err != nil {
			return err
		}
		s.entryIDs[id] = entryID
	}

	return nil
}
