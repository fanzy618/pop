package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/proxy"
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

	handler := proxy.NewServerWithDeps(cfg.BuildMatcher(), upstreams)

	srv := &http.Server{
		Addr:    cfg.ProxyListen,
		Handler: handler,
	}

	log.Printf("pop proxy listening on %s (config: %s)", cfg.ProxyListen, *configPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server stopped: %v", err)
	}
}
