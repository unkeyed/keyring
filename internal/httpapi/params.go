package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
)

// params reads a request's query string. The first bad or unknown parameter
// is kept in err, so a handler parses every field and checks once. Unknown
// names fail instead of being ignored, so ?query= (for ?q=) cannot silently
// return every contact.
type params struct {
	v   url.Values
	err error
}

func queryParams(r *http.Request, allowed ...string) *params {
	p := &params{v: r.URL.Query()}
	for name := range p.v {
		if !slices.Contains(allowed, name) {
			p.fail("unknown query parameter %q; this route accepts %s", name, strings.Join(allowed, ", "))
		}
	}
	return p
}

func (p *params) fail(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf("%w: "+format, append([]any{crm.ErrInvalid}, args...)...)
	}
}

func (p *params) str(name string) string { return p.v.Get(name) }

func (p *params) int(name string) int64 {
	s := p.v.Get(name)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		p.fail("%s must be a non-negative integer, got %q", name, s)
	}
	return n
}

func (p *params) time(name string) time.Time {
	s := p.v.Get(name)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		p.fail("%s must be an RFC 3339 timestamp like 2026-01-02T15:04:05Z, got %q", name, s)
	}
	return t
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive integer, got %q", crm.ErrInvalid, name, r.PathValue(name))
	}
	return id, nil
}
