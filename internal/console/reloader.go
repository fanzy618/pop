package console

import (
	"github.com/fanzy618/pop/internal/model"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/upstream"
)

// rebuildRuntime is invoked after any persisted change (rules, upstreams,
// config) that affects how the data plane should route. It builds a fresh
// RouteSnapshot from the database and atomically publishes it to the proxy.
func (s *Server) rebuildRuntime() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rebuildRuntimeLocked()
}

func (s *Server) rebuildRuntimeLocked() error {
	upstreamItems, err := s.db.ListUpstreams()
	if err != nil {
		return err
	}
	ruleItems, err := s.db.ListRules()
	if err != nil {
		return err
	}
	if err := model.ValidateRuntime(upstreamItems, ruleItems); err != nil {
		return err
	}

	mgr, err := upstream.NewManager(model.BuildUpstreamConfigs(upstreamItems))
	if err != nil {
		return err
	}

	s.proxy.Publish(proxy.NewSnapshot(model.BuildMatcher(ruleItems, s.cfg.DefaultAction), mgr))
	s.proxy.SetTelemetry(s.telemetry)
	return nil
}
