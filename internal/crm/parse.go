package crm

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// webURL returns field's value s as an absolute http(s) URL, adding https://
// when the scheme is missing, so "linkedin.com/in/ada" is accepted. "" stays
// "", which clears a field. Anything else, such as a javascript: link, is
// ErrInvalid: stored URLs end up in hrefs. contextdev.webURL applies the same
// rule to context.dev's URLs.
func webURL(field Field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	withScheme := s
	if !strings.Contains(s, "://") {
		withScheme = "https://" + s
	}
	u, err := url.Parse(withScheme)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%w: %s must be an http or https URL such as https://example.com/ada, got %q", ErrInvalid, field, s)
	}
	return u.String(), nil
}

// ParseTime reads an RFC 3339 timestamp or a YYYY-MM-DD date, which means
// midnight UTC at the start of that day.
func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: want an RFC 3339 time like 2026-01-02T15:04:05Z or a date like 2026-01-02, got %q", ErrInvalid, s)
}

func joinNames[T ~string](names []T) string {
	s := make([]string, len(names))
	for i, n := range names {
		s[i] = string(n)
	}
	return strings.Join(s, ", ")
}
