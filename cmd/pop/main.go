package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
	"github.com/fanzy618/pop/internal/upstream"
)

func main() {
	configPath := flag.String("config", "./pop.json", "config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	sqlitePath := cfg.SQLitePath
	if sqlitePath == "" {
		sqlitePath = filepath.Join(filepath.Dir(*configPath), "pop.sqlite")
	}

	db, err := store.OpenSQLite(sqlitePath)
	if err != nil {
		log.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	upstreamItems, err := db.ListUpstreams()
	if err != nil {
		log.Fatalf("load upstreams from sqlite failed: %v", err)
	}
	ruleItems, err := db.ListRules()
	if err != nil {
		log.Fatalf("load rules from sqlite failed: %v", err)
	}
	if err := config.ValidateRuntime(upstreamItems, ruleItems); err != nil {
		log.Fatalf("validate runtime config failed: %v", err)
	}

	upstreams, err := upstream.NewManager(config.BuildUpstreamConfigs(upstreamItems))
	if err != nil {
		log.Fatalf("build upstreams failed: %v", err)
	}
	telStore := telemetry.NewStore(10000, 30*time.Minute)

	handler := proxy.NewServerWithDeps(cfg.BuildMatcher(ruleItems), upstreams)
	handler.SetTelemetry(telStore)

	consoleHandler, err := console.NewServer(cfg, *configPath, db, handler, telStore)
	if err != nil {
		log.Fatalf("build console server failed: %v", err)
	}
	consoleSrv := &http.Server{Addr: cfg.ConsoleListen, Handler: consoleHandler}
	go func() {
		log.Printf("pop console listening on %s", cfg.ConsoleListen)
		if err := consoleSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("console server stopped: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:    cfg.ProxyListen,
		Handler: handler,
	}

	log.Printf("pop proxy listening on %s (config: %s)", cfg.ProxyListen, *configPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server stopped: %v", err)
	}
}
