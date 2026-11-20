// Package contextdev implements crm.Enricher over context.dev's two
// synchronous endpoints with net/http. The official SDK is not used: we need
// two POSTs, and its built-in retries would hide 429s from the job state
// machine, which owns retry policy.
package contextdev

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/unkeyed/keyring/internal/crm"
)

const baseURL = "https://api.context.dev/v1"

// minScore drops weak person matches; context.dev returns them with a 0-100
// score. A guess to tune against real data.
const minScore = 70

type Client struct {
	key  string
	base string
	http *http.Client
}

func New(apiKey string) *Client {
	return &Client{key: apiKey, base: baseURL, http: &http.Client{Timeout: 90 * time.Second}}
}

// personResponse is the part of the /people/enrich 200 body Keyring reads.
type personResponse struct {
	Match struct {
		Status string   `json:"status"`
		Score  *float64 `json:"score"`
		Person *struct {
			Name struct {
				Full string `json:"full"`
			} `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Bio       string `json:"bio"`
			Location  *struct {
				Display string `json:"display"`
			} `json:"location"`
			SocialURLs  []string `json:"social_urls"`
			WebsiteURLs []string `json:"website_urls"`
			CurrentRole *struct {
				Title string `json:"title"`
			} `json:"current_role"`
		} `json:"person"`
	} `json:"match"`
}

// Person calls POST /people/enrich {email, social_urls: [linkedInURL]}, the
// LinkedIn URL only when known. Maps:
//
//	match.status not_found, or score < minScore      -> Found: false
//	422 FREE_EMAIL_DETECTED / DISPOSABLE_EMAIL_DETECTED -> Found: false (their list is longer than ours)
//	person.name.full, current_role.title, location.display, bio, avatar_url -> PersonFacts
//	social_urls, website_urls[0]                     -> LinkedInURL and Socials (see personSocials)
func (c *Client) Person(ctx context.Context, email crm.Email, linkedInURL string) (crm.PersonFacts, error) {
	in := map[string]any{"email": email.String()}
	if u := webURL(linkedInURL); u != "" {
		in["social_urls"] = []string{u}
	}
	resp, body, err := c.post(ctx, "/people/enrich", in)
	if err != nil {
		return crm.PersonFacts{}, err
	}
	if code := errorCode(body); resp.StatusCode == http.StatusUnprocessableEntity &&
		(code == "FREE_EMAIL_DETECTED" || code == "DISPOSABLE_EMAIL_DETECTED") {
		return crm.PersonFacts{Raw: body}, nil
	}
	if resp.StatusCode/100 != 2 {
		return crm.PersonFacts{}, classify(resp, body)
	}
	var r personResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return crm.PersonFacts{}, fmt.Errorf("context.dev /people/enrich: %w", err)
	}
	p := r.Match.Person
	if r.Match.Status == "not_found" || p == nil || r.Match.Score == nil || *r.Match.Score < minScore {
		return crm.PersonFacts{Raw: body}, nil
	}
	f := crm.PersonFacts{Found: true, Name: p.Name.Full, Bio: p.Bio, AvatarURL: webURL(p.AvatarURL), Raw: body}
	f.LinkedInURL, f.Socials = personSocials(p.SocialURLs, p.WebsiteURLs)
	if p.CurrentRole != nil {
		f.Title = p.CurrentRole.Title
	}
	if p.Location != nil {
		f.Location = p.Location.Display
	}
	return f, nil
}

// socialHosts names the profile sites Keyring shows by name. A profile on any
// other site is keyed by its host.
var socialHosts = map[string]string{
	"x.com": "x", "twitter.com": "x", "github.com": "github", "youtube.com": "youtube",
	"facebook.com": "facebook", "instagram.com": "instagram", "bsky.app": "bluesky",
	"medium.com": "medium", "tiktok.com": "tiktok",
}

// personSocials sorts a person's profile URLs: the first on linkedin.com is
// the LinkedIn URL, every other site keeps its first profile, and the first
// website URL is "website".
func personSocials(socialURLs, websiteURLs []string) (string, crm.Socials) {
	linkedIn, socials := "", crm.Socials{}
	for _, raw := range socialURLs {
		u, host := parseWebURL(raw)
		switch {
		case u == "":
		case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
			if linkedIn == "" {
				linkedIn = u
			}
		default:
			kind := cmp.Or(socialHosts[host], host)
			if socials[kind] == "" {
				socials[kind] = u
			}
		}
	}
	for _, raw := range websiteURLs {
		if u := webURL(raw); u != "" && socials["website"] == "" {
			socials["website"] = u
		}
	}
	return linkedIn, socials
}

// brandResponse is the part of the /brand/retrieve 200 body Keyring reads.
// Its email and phone are left out on purpose: they are often a social
// handle or one employee's own address, not the company's.
type brandResponse struct {
	Brand struct {
		Title       string      `json:"title"`
		Description string      `json:"description"`
		Slogan      string      `json:"slogan"`
		Logos       []brandLogo `json:"logos"`
		Socials     []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"socials"`
		Address *struct {
			City          string `json:"city"`
			StateProvince string `json:"state_province"`
			Country       string `json:"country"`
		} `json:"address"`
		Stock *struct {
			Ticker   string `json:"ticker"`
			Exchange string `json:"exchange"`
		} `json:"stock"`
		Employees *struct {
			Range string `json:"range"`
		} `json:"employees"`
		Industries *struct {
			EIC []struct {
				Industry    string `json:"industry"`
				Subindustry string `json:"subindustry"`
			} `json:"eic"`
		} `json:"industries"`
		Links map[string]*string `json:"links"`
	} `json:"brand"`
}

type brandLogo struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Mode   string `json:"mode"`
	Colors []struct {
		Hex string `json:"hex"`
	} `json:"colors"`
	Resolution struct {
		Width       int     `json:"width"`
		AspectRatio float64 `json:"aspect_ratio"`
	} `json:"resolution"`
}

const (
	// sharpWidth is twice the largest logo tile Keyring draws (52px), so a
	// raster this wide stays crisp on a 2x screen.
	sharpWidth = 104
	// minWidth drops rasters too small for any tile.
	minWidth = 32
)

const (
	// The relative luminance of Keyring's light and dark surfaces
	// (--surface in web/src/index.css: oklch(1 0 0) and oklch(0.205 0 0)).
	lightSurface = 1.0
	darkSurface  = 0.0086
	// minContrast is WCAG's floor for graphics (1.4.11).
	minContrast = 3.0
)

// readsOn reports whether the art stands out on Keyring's light and dark
// surfaces. context.dev's mode says which theme an image suits, but it is
// sometimes wrong (Stripe's purple icon is labelled "dark" and reads fine
// on white), so the logo's own colours decide; the label is the fallback
// when no colour clears the bar.
func readsOn(l brandLogo) (light, dark bool) {
	for _, c := range l.Colors {
		y, ok := luminance(c.Hex)
		if !ok {
			continue
		}
		light = light || contrast(y, lightSurface) >= minContrast
		dark = dark || contrast(y, darkSurface) >= minContrast
	}
	if light || dark {
		return light, dark
	}
	return l.Mode != "dark", l.Mode == "dark"
}

// luminance is the WCAG relative luminance of a #rrggbb colour.
func luminance(hex string) (float64, bool) {
	var r, g, b uint8
	if n, err := fmt.Sscanf(strings.ToLower(hex), "#%02x%02x%02x", &r, &g, &b); n != 3 || err != nil {
		return 0, false
	}
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), true
}

func contrast(a, b float64) float64 {
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}

// logos normalizes context.dev's logos and orders them as crm.Logo
// documents.
func logos(in []brandLogo) []crm.Logo {
	out := []crm.Logo{}
	for _, l := range in {
		u := webURL(l.URL)
		if u == "" || !isSVG(u) && l.Resolution.Width > 0 && l.Resolution.Width < minWidth {
			continue
		}
		lg := crm.Logo{URL: u, Kind: crm.LogoWordmark, Aspect: l.Resolution.AspectRatio, Width: l.Resolution.Width}
		if l.Type == "icon" {
			lg.Kind = crm.LogoIcon
		}
		if l.Mode == "has_opaque_background" {
			lg.OwnBackground, lg.OnLight, lg.OnDark = true, true, true
		} else {
			lg.OnLight, lg.OnDark = readsOn(l)
		}
		out = append(out, lg)
	}
	blurry := func(l crm.Logo) bool { return !isSVG(l.URL) && l.Width < sharpWidth }
	offSquare := func(l crm.Logo) float64 {
		if l.Aspect <= 0 {
			return 1 // unknown shape sorts after any near-square one
		}
		return math.Abs(math.Log(l.Aspect))
	}
	slices.SortStableFunc(out, func(a, b crm.Logo) int {
		return cmp.Or(
			compareBool(a.Kind != crm.LogoIcon, b.Kind != crm.LogoIcon),
			compareBool(!a.OwnBackground, !b.OwnBackground),
			compareBool(!(a.OnLight && a.OnDark), !(b.OnLight && b.OnDark)),
			compareBool(blurry(a), blurry(b)),
			cmp.Compare(offSquare(a), offSquare(b)),
		)
	})
	return out
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func isSVG(u string) bool {
	p, err := url.Parse(u)
	return err == nil && strings.HasSuffix(strings.ToLower(p.Path), ".svg")
}

// Company calls POST /brand/retrieve {type: by_domain, domain,
// timeoutOpts: {milliseconds: 60000, behavior: fail}}. 60s avoids
// COLD_DOMAIN_TIMEOUT_TOO_LOW on uncached domains. Maps:
//
//	400 NOT_FOUND                     -> Found: false
//	brand.title, description, slogan (Tagline), employees.range,
//	industries.eic[0].industry, address.country
//	address.city, state_province      -> Headquarters
//	socials                           -> LinkedInURL and Socials by type
//	links careers, pricing, blog, contact -> Links
//	industries.eic[].subindustry      -> Segments
//	stock                             -> Stock "EXCHANGE: TICKER"
//	logos                             -> Logos (see logos), LogoURL the first
func (c *Client) Company(ctx context.Context, domain crm.Domain) (crm.CompanyFacts, error) {
	resp, body, err := c.post(ctx, "/brand/retrieve", map[string]any{
		"type":        "by_domain",
		"domain":      domain.String(),
		"timeoutOpts": map[string]any{"milliseconds": 60000, "behavior": "fail"},
	})
	if err != nil {
		return crm.CompanyFacts{}, err
	}
	if resp.StatusCode == http.StatusBadRequest && errorCode(body) == "NOT_FOUND" {
		return crm.CompanyFacts{Raw: body}, nil
	}
	if resp.StatusCode/100 != 2 {
		return crm.CompanyFacts{}, classify(resp, body)
	}
	var r brandResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return crm.CompanyFacts{}, fmt.Errorf("context.dev /brand/retrieve: %w", err)
	}
	b := r.Brand
	f := crm.CompanyFacts{
		Found: true, Name: b.Title, Description: b.Description, Tagline: b.Slogan,
		Socials: crm.Socials{}, Links: crm.Links{}, Logos: logos(b.Logos), Raw: body,
	}
	if len(f.Logos) > 0 {
		f.LogoURL = f.Logos[0].URL
	}
	for _, s := range b.Socials {
		kind, u := strings.ToLower(strings.TrimSpace(s.Type)), webURL(s.URL)
		if kind == "twitter" {
			kind = "x"
		}
		switch {
		case u == "" || kind == "":
		case kind == "linkedin":
			f.LinkedInURL = cmp.Or(f.LinkedInURL, u)
		case f.Socials[kind] == "":
			f.Socials[kind] = u
		}
	}
	for _, kind := range []string{"careers", "pricing", "blog", "contact"} {
		if v := b.Links[kind]; v != nil && webURL(*v) != "" {
			f.Links[kind] = webURL(*v)
		}
	}
	if a := b.Address; a != nil {
		f.Country = a.Country
		f.Headquarters = a.City
		if a.StateProvince != "" && a.StateProvince != a.City && a.StateProvince != a.Country {
			f.Headquarters = strings.TrimPrefix(f.Headquarters+", "+a.StateProvince, ", ")
		}
	}
	if st := b.Stock; st != nil && st.Ticker != "" {
		f.Stock = strings.TrimPrefix(st.Exchange+": "+st.Ticker, ": ")
	}
	if b.Employees != nil {
		f.EmployeeRange = b.Employees.Range
	}
	if b.Industries != nil {
		for i, e := range b.Industries.EIC {
			if i == 0 {
				f.Industry = e.Industry
			}
			if e.Subindustry != "" && !slices.Contains(f.Segments, e.Subindustry) {
				f.Segments = append(f.Segments, e.Subindustry)
			}
		}
	}
	return f, nil
}

// post sends one authenticated JSON request and reads the whole response.
// A transport error comes back as a plain error, which the job state
// machine retries.
func (c *Client) post(ctx context.Context, path string, in any) (*http.Response, []byte, error) {
	reqBody, err := json.Marshal(in)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("context.dev %s: reading response: %w", path, err)
	}
	return resp, body, nil
}

// errorBody is context.dev's ErrorResponse.
type errorBody struct {
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
}

func errorCode(body []byte) string {
	var e errorBody
	_ = json.Unmarshal(body, &e)
	return e.ErrorCode
}

// classify turns a non-2xx response into *crm.EnrichError:
//
//	429             -> EnrichThrottled, RetryAfter from Retry-After
//	401, 402, 403   -> EnrichHalted (bad key, USAGE_EXCEEDED, disabled account)
//	408, 5xx        -> EnrichRetryable
//	other 4xx       -> EnrichPermanent (never retry-loop 4xx: context.dev
//	                   disables free keys after repeated failures)
//
// Msg carries the status, error_code and message, because Settings shows a
// halt's Msg as the reason enrichment stopped.
func classify(resp *http.Response, body []byte) error {
	var e errorBody
	_ = json.Unmarshal(body, &e)
	msg := fmt.Sprintf("context.dev %d", resp.StatusCode)
	if e.ErrorCode != "" {
		msg += " " + e.ErrorCode
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	ee := &crm.EnrichError{Kind: crm.EnrichPermanent, Msg: msg}
	switch code := resp.StatusCode; {
	case code == http.StatusTooManyRequests:
		ee.Kind = crm.EnrichThrottled
		ee.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), time.Now())
	case code == http.StatusUnauthorized || code == http.StatusPaymentRequired || code == http.StatusForbidden:
		ee.Kind = crm.EnrichHalted
	case code == http.StatusRequestTimeout || code >= 500:
		ee.Kind = crm.EnrichRetryable
	}
	return ee
}

// retryAfter reads Retry-After as seconds or an HTTP date. Zero means
// absent or unreadable, and the state machine picks its own wait.
func retryAfter(v string, now time.Time) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// webURL returns raw as an absolute http(s) URL, adding https:// when the
// scheme is missing, or "" when it is not a web address. context.dev's URLs
// are untrusted, and a javascript: link must never reach an href.
func webURL(raw string) string {
	u, _ := parseWebURL(raw)
	return u
}

// parseWebURL is webURL plus the lowercased host without "www.".
func parseWebURL(raw string) (string, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ""
	}
	return u.String(), strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
