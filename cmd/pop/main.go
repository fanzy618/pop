package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
	"github.com/fanzy618/pop/internal/upstream"
)

const (
	envProxyListen   = "POP_PROXY_LISTEN"
	envConsoleListen = "POP_CONSOLE_LISTEN"
	envSQLitePath    = "POP_SQLITE_PATH"
	envDefaultAction = "POP_DEFAULT_ACTION"
)

func main() {
	cfg, err := resolveRuntimeConfig(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatalf("resolve config failed: %v", err)
	}

	db, err := store.OpenSQLite(cfg.SQLitePath)
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
	sysStats := telemetry.NewSysStatsCollector(telStore.Snapshot, 360, 10*time.Second, time.Hour)
	sysStats.Start(context.Background())

	handler := proxy.NewServerWithDeps(cfg.BuildMatcher(ruleItems), upstreams)
	handler.SetTelemetry(telStore)

	consoleHandler, err := console.NewServer(cfg, db, handler, telStore, sysStats)
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

	srv := &http.Server{Addr: cfg.ProxyListen, Handler: handler}
	log.Printf("pop proxy listening on %s", cfg.ProxyListen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("proxy server stopped: %v", err)
	}
}

func resolveRuntimeConfig(args []string, getenv func(string) string) (*config.Config, error) {
	cfg := config.Default()

	if v := strings.TrimSpace(getenv(envProxyListen)); v != "" {
		cfg.ProxyListen = v
	}
	if v := strings.TrimSpace(getenv(envConsoleListen)); v != "" {
		cfg.ConsoleListen = v
	}
	if v := strings.TrimSpace(getenv(envSQLitePath)); v != "" {
		cfg.SQLitePath = v
	}
	if v := strings.TrimSpace(getenv(envDefaultAction)); v != "" {
		cfg.DefaultAction = rules.Action(strings.ToUpper(v))
	}

	fs := flag.NewFlagSet("pop", flag.ContinueOnError)
	proxyListen := cfg.ProxyListen
	consoleListen := cfg.ConsoleListen
	sqlitePath := cfg.SQLitePath
	defaultAction := string(cfg.DefaultAction)

	fs.StringVar(&proxyListen, "proxy-listen", proxyListen, "proxy listen address")
	fs.StringVar(&proxyListen, "p", proxyListen, "proxy listen address (short)")
	fs.StringVar(&consoleListen, "console-listen", consoleListen, "console listen address")
	fs.StringVar(&consoleListen, "c", consoleListen, "console listen address (short)")
	fs.StringVar(&sqlitePath, "sqlite-path", sqlitePath, "sqlite file path")
	fs.StringVar(&sqlitePath, "s", sqlitePath, "sqlite file path (short)")
	fs.StringVar(&defaultAction, "default-action", defaultAction, "default action: DIRECT|PROXY|BLOCK")
	fs.StringVar(&defaultAction, "a", defaultAction, "default action (short)")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: pop [OPTIONS]\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if len(fs.Args()) > 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg.ProxyListen = strings.TrimSpace(proxyListen)
	cfg.ConsoleListen = strings.TrimSpace(consoleListen)
	cfg.SQLitePath = strings.TrimSpace(sqlitePath)
	cfg.DefaultAction = rules.Action(strings.ToUpper(strings.TrimSpace(defaultAction)))

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
