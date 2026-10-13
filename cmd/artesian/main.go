package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"artesian/internal/api"
	"artesian/internal/cache"
	"artesian/internal/config"
	"artesian/internal/db"
	"artesian/internal/dns"
	"artesian/internal/prefill"
	"artesian/internal/proxy"
	"artesian/internal/scheduler"
	"artesian/internal/services"
)

func main() {
	configPath := flag.String("config", "configs/artesian.yaml", "Path to configuration file")
	flag.Parse()

	fmt.Println(`
   _____          __                .__               
  /  _  \________/  |_  ____   _____|__|____    ____  
 /  /_\  \_  __ \   __\/ __ \ /  ___/  \__  \  /    \ 
/    |    \  | \/|  | \  ___/ \___ \|  |/ __ \|   |  \
\____|__  /__|   |__|  \___  >____  >__(____  /___|  /
        \/                 \/     \/        \/     \/ 
Unified Game Cache & Delivery Architecture [Go + VueJS]
	`)

	// 1. Load Configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[Artesian] Configuration error: %v", err)
	}

	// 2. Initialize State Database
	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("[Artesian] Database connection failure: %v", err)
	}
	defer database.Close()
	log.Printf("[Artesian] State database loaded (%s)", cfg.Database.Path)

	// 3. Initialize Storage Layer
	storage, err := cache.NewStorage(cfg.Cache)
	if err != nil {
		log.Fatalf("[Artesian] Cache storage error: %v", err)
	}

	// 4. Register Game Platform Services
	registry := services.NewRegistry()
	registry.Register(services.NewSteamService())
	log.Println("[Artesian] Service registered: Steam")

	registry.Register(services.NewEpicService())
	log.Println("[Artesian] Service registered: Epic Games")

	registry.Register(services.NewBlizzardService())
	log.Println("[Artesian] Service registered: Battle.net")

	registry.Register(services.NewRiotService())
	log.Println("[Artesian] Service registered: Riot Games")

	registry.Register(services.NewXboxService())
	log.Println("[Artesian] Service registered: Xbox / Microsoft Store")

	// 5. Initialize Prefill & Scheduler Subsystems
	prefillMgr := prefill.NewManager(storage, database)
	sched := scheduler.NewScheduler(cfg.Scheduler, database, prefillMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Start(ctx)

	// 6. Launch DNS Interceptor & Forwarder
	if cfg.DNS.Enabled {
		dnsServer := dns.NewServer(cfg.DNS, registry.AllDomains())
		go func() {
			if err := dnsServer.Start(ctx); err != nil {
				log.Printf("[Artesian DNS] Server error: %v", err)
			}
		}()
		defer dnsServer.Stop()
	}

	// 7. Launch HTTP Caching Reverse Proxy Engine
	proxyServer := proxy.NewServer(cfg.Proxy, storage, database, registry)
	go func() {
		if err := proxyServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Artesian Proxy] Fatal error: %v", err)
		}
	}()
	defer proxyServer.Close()

	// 8. Launch Web Management UI & REST API
	apiServer := api.NewServer(cfg.Server, &cfg.DNS, storage, database, registry, prefillMgr, sched)
	webHTTP := &http.Server{
		Addr:         cfg.Server.ListenAddr,
		Handler:      apiServer.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("[Artesian Web] Control panel & API active on http://localhost%s", cfg.Server.ListenAddr)
		if err := webHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Artesian Web] Fatal error: %v", err)
		}
	}()

	// 9. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("[Artesian] Initiating graceful shutdown...")
	cancel()
	sched.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = webHTTP.Shutdown(shutdownCtx)

	log.Println("[Artesian] All services stopped cleanly. Goodbye.")
}
