package httpapi

import (
	"bufio"
	"bytes"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/unkeyed/byname/internal/core"
)

// TestEveryRouteHasAnAccessClass fails when a route is neither public nor
// for a session, an admin or one scope, so no route can be added without
// saying who may call it.
func TestEveryRouteHasAnAccessClass(t *testing.T) {
	for _, rt := range routes {
		switch {
		case rt.access == public, rt.access == session, rt.access == admin:
		case rt.access == anyone:
			if rt.pattern != "DELETE /v1/keys/current" {
				t.Errorf("%s: anyone is only for DELETE /v1/keys/current; name the scope it needs", rt.pattern)
			}
		case slices.Contains(core.Scopes, core.Scope(rt.access)):
		default:
			t.Errorf("%s: access %q is not public, session, admin or a scope in core.Scopes", rt.pattern, rt.access)
		}
		if rt.access == public && strings.Contains(rt.pattern, " /v1/") && rt.pattern != "GET /v1/openapi.yaml" {
			t.Errorf("%s: a public /v1 route; give it a scope or a session class", rt.pattern)
		}
	}
}

// TestRoutesMatchOpenAPI fails when a pattern in routes has no matching
// method+path in openapi.yaml, or the reverse, or when the spec's security
// for an operation disagrees with the route's access.
func TestRoutesMatchOpenAPI(t *testing.T) {
	spec := specOperations(t, openapiYAML)
	table := map[string]access{}
	for _, rt := range routes {
		table[strings.ToLower(rt.pattern)] = rt.access
	}
	for _, op := range slices.Sorted(maps.Keys(table)) {
		got, ok := spec[op]
		switch {
		case !ok:
			t.Errorf("route %q is missing from openapi.yaml", op)
		case got != table[op]:
			t.Errorf("%q: openapi.yaml security says %s, the route table says %s", op, got, table[op])
		}
	}
	for _, op := range slices.Sorted(maps.Keys(spec)) {
		if _, ok := table[op]; !ok {
			t.Errorf("openapi.yaml documents %q, which no route serves", op)
		}
	}
}

var requirement = regexp.MustCompile(`^        - (apiKey|session): \[(.*)\]$`)

// specOperations reads "method /path" -> access from the paths section of
// an OpenAPI document laid out like openapi.yaml: paths at two spaces,
// methods at four, an operation's security at six and its requirements at
// eight. "security: []" is public, "- session: []" alone is session,
// "- session: [admin]" is admin, "- apiKey: [<scope>]" is that scope and
// "- apiKey: []" is anyone. Every operation must state its security.
func specOperations(t *testing.T, doc []byte) map[string]access {
	t.Helper()
	reqs := map[string][]string{}
	var path, op string
	inPaths, inSecurity := false, false
	sc := bufio.NewScanner(bytes.NewReader(doc))
	for sc.Scan() {
		line := sc.Text()
		if inSecurity {
			if m := requirement.FindStringSubmatch(line); m != nil {
				reqs[op] = append(reqs[op], m[1]+":"+m[2])
				continue
			}
			inSecurity = false
		}
		switch {
		case line == "paths:":
			inPaths = true
		case !inPaths || line == "":
		case !strings.HasPrefix(line, " "):
			inPaths = false
		case strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":"):
			path, op = strings.TrimSuffix(strings.TrimSpace(line), ":"), ""
		case slices.Contains([]string{"    get:", "    put:", "    post:", "    patch:", "    delete:"}, line):
			op = strings.TrimSuffix(strings.TrimSpace(line), ":") + " " + path
			reqs[op] = nil
		case op != "" && line == "      security: []":
			reqs[op] = []string{}
		case op != "" && line == "      security:":
			inSecurity = true
		}
	}
	if len(reqs) == 0 {
		t.Fatal("openapi.yaml has no operations under paths:")
	}
	ops := map[string]access{}
	for op, r := range reqs {
		switch {
		case r == nil:
			t.Errorf("%q states no security", op)
		case len(r) == 0:
			ops[op] = public
		case slices.Equal(r, []string{"session:"}):
			ops[op] = session
		case slices.Equal(r, []string{"session:admin"}):
			ops[op] = admin
		case slices.Equal(r, []string{"apiKey:"}):
			ops[op] = anyone
		case len(r) == 2 && strings.HasPrefix(r[0], "apiKey:") && r[1] == "session:"+strings.TrimPrefix(r[0], "apiKey:"):
			ops[op] = access(strings.TrimPrefix(r[0], "apiKey:"))
		default:
			t.Errorf("%q has security %v, which names no access class", op, r)
		}
	}
	return ops
}
