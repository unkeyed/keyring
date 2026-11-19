package httpapi

import (
	"bufio"
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"
)

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
			t.Errorf("%q: openapi.yaml security says %s, the route table says %s", op, accessName[got], accessName[table[op]])
		}
	}
	for _, op := range slices.Sorted(maps.Keys(spec)) {
		if _, ok := table[op]; !ok {
			t.Errorf("openapi.yaml documents %q, which no route serves", op)
		}
	}
}

var accessName = map[access]string{public: "public", anyone: "anyone", humans: "humans"}

// specOperations reads "method /path" -> access from the paths section of
// an OpenAPI document laid out like openapi.yaml: paths at two spaces,
// methods at four, an operation's security at six. "security: []" is
// public, "- session: []" alone is humans, and no override is anyone.
func specOperations(t *testing.T, doc []byte) map[string]access {
	t.Helper()
	ops := map[string]access{}
	var path, op string
	inPaths, sessionOnly := false, false
	sc := bufio.NewScanner(bytes.NewReader(doc))
	for sc.Scan() {
		line := sc.Text()
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
			ops[op] = anyone
		case op != "" && line == "      security: []":
			ops[op] = public
		case op != "" && line == "      security:":
			sessionOnly = true
		case sessionOnly:
			sessionOnly = false
			if line == "        - session: []" {
				ops[op] = humans
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("openapi.yaml has no operations under paths:")
	}
	return ops
}
