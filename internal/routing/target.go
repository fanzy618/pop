// Package routing holds small pure helpers shared by callers that need to
// translate user-facing routing targets (e.g. console form fields) into
// concrete (Action, upstream id) tuples consumed by the rule engine.
package routing

import (
	"errors"
	"strconv"
	"strings"

	"github.com/fanzy618/pop/internal/rules"
)

// Target is a destination for bulk rule creation. Either DIRECT (no upstream)
// or PROXY referencing a known upstream id.
type Target struct {
	Action     rules.Action
	UpstreamID int64
}

// ParseTarget accepts the wire format used by the console's bulk-import form:
//
//	""             → DIRECT
//	"DIRECT"       → DIRECT
//	"UPSTREAM:<n>" → PROXY pointing at upstream id n (n > 0)
//
// Any other input returns an error.
func ParseTarget(raw string) (Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "DIRECT" {
		return Target{Action: rules.ActionDirect}, nil
	}
	if strings.HasPrefix(raw, "UPSTREAM:") {
		idRaw := strings.TrimSpace(strings.TrimPrefix(raw, "UPSTREAM:"))
		id, err := strconv.ParseInt(idRaw, 10, 64)
		if err != nil || id <= 0 {
			return Target{}, errors.New("invalid upstream route_target")
		}
		return Target{Action: rules.ActionProxy, UpstreamID: id}, nil
	}
	return Target{}, errors.New("unsupported route_target")
}
