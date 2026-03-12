package upstream

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Config struct {
	ID      string
	URL     string
	Enabled bool
}

type Target struct {
	ID        string
	URL       *url.URL
	Transport *http.Transport
}

type Manager struct {
	mu      sync.RWMutex
	targets map[string]*Target
}

func NewManager(configs []Config) (*Manager, error) {
	m := &Manager{targets: make(map[string]*Target)}
	if err := m.Replace(configs); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Replace(configs []Config) error {
	next := make(map[string]*Target)
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if cfg.ID == "" {
			return fmt.Errorf("upstream id cannot be empty")
		}

		u, err := url.Parse(cfg.URL)
		if err != nil {
			return fmt.Errorf("parse upstream %s: %w", cfg.ID, err)
		}
		if u.Scheme != "http" {
			return fmt.Errorf("upstream %s scheme %q is not supported", cfg.ID, u.Scheme)
		}
		if u.Host == "" {
			return fmt.Errorf("upstream %s host cannot be empty", cfg.ID)
		}

		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		transport := &http.Transport{
			Proxy:                 http.ProxyURL(u),
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          128,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		}

		next[cfg.ID] = &Target{ID: cfg.ID, URL: u, Transport: transport}
	}

	m.mu.Lock()
	old := m.targets
	m.targets = next
	m.mu.Unlock()

	for id, target := range old {
		if _, ok := next[id]; !ok {
			target.Transport.CloseIdleConnections()
		}
	}

	return nil
}

func (m *Manager) Get(id string) (*Target, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	target, ok := m.targets[id]
	return target, ok
}

func (m *Manager) All() map[string]*Target {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]*Target, len(m.targets))
	for k, v := range m.targets {
		res[k] = v
	}
	return res
}
