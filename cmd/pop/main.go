package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
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

	upstreams, err := upstream.NewManager(cfg.BuildUpstreamConfigs())
	if err != nil {
		log.Fatalf("build upstreams failed: %v", err)
	}
	store := telemetry.NewStore(10000, 30*time.Minute)

	handler := proxy.NewServerWithDeps(cfg.BuildMatcher(), upstreams)
	handler.SetTelemetry(store)

	consoleHandler, err := console.NewServer(cfg, *configPath, handler, store)
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
