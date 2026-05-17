// Package abp parses an Adblock Plus filter list into a deduplicated, sorted
// list of bare hostnames. The subset accepted is intentionally narrow: blank
// lines and comments are skipped; element hiding (##, #@#, #?#), exception
// (@@), and regex (/.../) rules are reported as skipped; everything else is
// reduced to its hostname via the conventions of ||host^ rules.
package abp

import (
	"bufio"
	"net"
	"regexp"
	"sort"
	"strings"
)

var domainPattern = regexp.MustCompile(`^[a-z0-9.-]+$`)

// ParseDomains returns the deduplicated, lexicographically sorted set of
// hostnames extracted from content, along with the total line count and the
// number of lines skipped as unsupported (regex, exception, element-hiding,
// malformed). It never returns an error — malformed lines are simply
// reported in skippedUnsupported.
func ParseDomains(content string) (domains []string, totalLines int, skippedUnsupported int) {
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		totalLines++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") {
			continue
		}
		if strings.HasPrefix(line, "@@") || strings.Contains(line, "##") || strings.Contains(line, "#@#") || strings.Contains(line, "#?#") {
			skippedUnsupported++
			continue
		}
		if strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/") {
			skippedUnsupported++
			continue
		}

		host, ok := parseLineHost(line)
		if !ok {
			skippedUnsupported++
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		domains = append(domains, host)
	}
	sort.Strings(domains)
	return domains, totalLines, skippedUnsupported
}

func parseLineHost(line string) (string, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "||")
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimPrefix(line, "http://")
	line = strings.TrimPrefix(line, "https://")
	line = strings.TrimPrefix(line, "*.")

	if idx := strings.IndexAny(line, "^/$?|*"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(strings.ToLower(strings.Trim(line, ".")))
	if line == "" {
		return "", false
	}
	if strings.Contains(line, ":") {
		hostOnly, _, err := net.SplitHostPort(line)
		if err == nil {
			line = hostOnly
		} else if strings.Count(line, ":") == 1 {
			parts := strings.Split(line, ":")
			if len(parts) == 2 && parts[1] != "" {
				line = parts[0]
			}
		}
	}
	if !domainPattern.MatchString(line) || !strings.Contains(line, ".") {
		return "", false
	}
	if strings.HasPrefix(line, "-") || strings.HasSuffix(line, "-") || strings.Contains(line, "..") {
		return "", false
	}
	return line, true
}
