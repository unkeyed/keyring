package auth

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type Config struct {
	// PublicURL is where people open uncrm. Its host is the WebAuthn relying
	// party ID, its origin the only accepted WebAuthn origin, and OAuth
	// callbacks live under it. Session cookies are Secure when it is https.
	PublicURL     *url.URL
	PasswordLogin bool
	// AllowedEmailDomains lets anyone with a provider-verified address at one
	// of these domains join by social sign-in without an invite. Lower case.
	AllowedEmailDomains []string
	// ClientIPHeader names a header that a trusted proxy in front of uncrm
	// sets to the client's address. Empty means use the TCP peer address.
	ClientIPHeader string
	Providers      []ProviderConfig
}

type ProviderConfig struct {
	ID           string // "google", "github" or "oidc": the URL path segment and identity namespace
	Name         string
	ClientID     string
	ClientSecret string
	Issuer       string
	// GitHubURL and GitHubAPIURL default to github.com; tests point them at
	// a fake.
	GitHubURL, GitHubAPIURL string
}

type Client struct {
	// IP is the throttling key for the address: the address itself for
	// IPv4, its /64 for IPv6, where one host usually owns the whole prefix.
	IP        string
	UserAgent string
}

func (c Client) ipKey() string { return "ip:" + c.IP }

// ClientOf reads the client's address from r.RemoteAddr, or from the
// rightmost entry of ClientIPHeader when one is configured: that entry is
// the one the nearest proxy wrote, so a client cannot forge it by sending
// the header itself.
func (u *Users) ClientOf(r *http.Request) Client {
	raw := r.RemoteAddr
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	if h := u.cfg.ClientIPHeader; h != "" {
		if v := r.Header.Values(h); len(v) > 0 {
			parts := strings.Split(v[len(v)-1], ",")
			raw = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	ip := raw
	if addr, err := netip.ParseAddr(raw); err == nil {
		addr = addr.Unmap()
		ip = addr.String()
		if addr.Is6() {
			ip = netip.PrefixFrom(addr, 64).Masked().String()
		}
	}
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return Client{IP: ip, UserAgent: ua}
}

type ProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CallbackURL string `json:"callback_url"`
}

type Options struct {
	Providers     []ProviderInfo `json:"providers"`
	PasswordLogin bool           `json:"password_login"`
	SetupRequired bool           `json:"setup_required"`
}

func (u *Users) providerInfo() []ProviderInfo {
	out := []ProviderInfo{}
	for _, p := range u.cfg.Providers {
		out = append(out, ProviderInfo{ID: p.ID, Name: p.Name, CallbackURL: u.callbackURL(p.ID)})
	}
	return out
}

func (u *Users) providerIDs() []string {
	ids := []string{}
	for _, p := range u.cfg.Providers {
		ids = append(ids, p.ID)
	}
	return ids
}
