package sources

import (
	"bufio"
	"strings"
)

// robots is the part of a robots.txt (RFC 9309) that applies to Byname:
// the rules of the groups naming its product token, or of the * groups
// when none does.
type robots struct {
	rules []robotsRule
}

type robotsRule struct {
	allow   bool
	pattern string
}

var allowAll = robots{}

var disallowAll = robots{rules: []robotsRule{{allow: false, pattern: "/"}}}

// parseRobots reads a robots.txt for the product token agent.
func parseRobots(body, agent string) robots {
	agent = strings.ToLower(agent)
	var mine, star []robotsRule
	var matchesMe, matchesStar, sawMine bool
	inAgents := false
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if !inAgents {
				matchesMe, matchesStar = false, false
			}
			inAgents = true
			v := strings.ToLower(value)
			if v == "*" {
				matchesStar = true
			} else if v == agent {
				matchesMe, sawMine = true, true
			}
		case "allow", "disallow":
			inAgents = false
			// An empty Disallow allows everything; it adds no rule.
			if value == "" {
				continue
			}
			r := robotsRule{allow: key == "allow", pattern: value}
			if matchesMe {
				mine = append(mine, r)
			}
			if matchesStar {
				star = append(star, r)
			}
		default:
			inAgents = false
		}
	}
	if sawMine {
		return robots{rules: mine}
	}
	return robots{rules: star}
}

// allowed applies the most specific matching rule to a path (with its
// query), Allow winning a tie. No matching rule allows.
func (r robots) allowed(path string) bool {
	best, allow := -1, true
	for _, rule := range r.rules {
		if !robotsMatch(rule.pattern, path) {
			continue
		}
		if n := len(rule.pattern); n > best || (n == best && rule.allow) {
			best, allow = n, rule.allow
		}
	}
	return allow
}

// robotsMatch matches a robots.txt path pattern, where * is any run of
// characters and a trailing $ anchors the end, against the start of path.
func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	for i, part := range parts[1:] {
		last := i == len(parts)-2
		if last && anchored {
			return strings.HasSuffix(rest, part)
		}
		j := strings.Index(rest, part)
		if j < 0 {
			return false
		}
		rest = rest[j+len(part):]
	}
	return !anchored || rest == ""
}
