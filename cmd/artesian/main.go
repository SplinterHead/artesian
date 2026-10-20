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

	"artesian/internal/adapters/epic"
	"artesian/internal/adapters/steam"
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
	dbPathFlag := flag.String("db", "", "Path to SQLite database (default: /data/artesian.db or ./data/db/artesian.db)")
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

	// 1. Determine Database Path
	targetDB := *dbPathFlag
	if targetDB == "" {
		if _, err := os.Stat("/data"); err == nil {
			targetDB = "/data/artesian.db"
		} else if _, err := os.Stat("./data/db/artesian.db"); err == nil {
			targetDB = "./data/db/artesian.db"
		} else {
			targetDB = "/data/artesian.db"
		}
	}

	// 2. Initialize State Database (seeds defaults and scheduler rules on initial setup)
	database, err := db.Open(targetDB)
	if err != nil {
		log.Fatalf("[Artesian] Database connection failure (%s): %v", targetDB, err)
	}
	defer database.Close()
	log.Printf("[Artesian] State database loaded (%s)", targetDB)

	// 3. Load Configuration from SQLite Database Settings
	cfg := config.LoadFromDB(database)

	// 4. Initialize Storage Layer
	storage, err := cache.NewStorage(cfg.Cache)
	if err != nil {
		log.Fatalf("[Artesian] Cache storage error: %v", err)
	}

	// 5. Initialize Platform Adapters & Register with Service Registry
	registry := services.NewRegistry()

	steamAdapter := steam.NewAdapter(database, storage, &cfg.DNS)
	registry.Register(steamAdapter)
	log.Println("[Artesian] Service registered: Steam")

	epicAdapter := epic.NewAdapter(database, storage, &cfg.DNS)
	registry.Register(epicAdapter)
	log.Println("[Artesian] Service registered: Epic Games")

	// 6. Initialize Prefill & Scheduler Subsystems
	prefillMgr := prefill.NewManager(storage, database, cfg.DNS, registry)
	sched := scheduler.NewScheduler(database, prefillMgr, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Start(ctx)

	// 7. Launch DNS Interceptor & Forwarder
	var dnsServer *dns.Server
	if cfg.DNS.Enabled {
		dnsServer = dns.NewServer(cfg.DNS, registry.AllDomains())
		go func() {
			if err := dnsServer.Start(ctx); err != nil {
				log.Printf("[Artesian DNS] Server error: %v", err)
			}
		}()
		defer dnsServer.Stop()

		if cfg.DNS.InterceptIPv4 == "127.0.0.1" {
			log.Println("[Artesian DNS] Notice: Intercept Target IP is set to 127.0.0.1. For remote LAN clients, configure your host LAN IP in Settings.")
		}
	}

	// 8. Launch HTTP Caching Reverse Proxy Engine
	proxyServer := proxy.NewServer(cfg.Proxy, cfg.DNS, storage, database, registry)
	prefillMgr.SetTrafficRecorder(proxyServer)
	go func() {
		if err := proxyServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Artesian Proxy] Fatal error: %v", err)
		}
	}()
	defer proxyServer.Close()

	// 9. Launch Web Management UI & REST API
	apiServer := api.NewServer(cfg.Server, &cfg.DNS, dnsServer, storage, database, registry, prefillMgr, sched, proxyServer)
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

	// 10. Graceful Shutdown
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
