// Package contextdev implements crm.Enricher over context.dev's two
// synchronous endpoints with net/http. The official SDK is not used: we need
// two POSTs, and its built-in retries would hide 429s from the job state
// machine, which owns retry policy.
package contextdev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
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

// personResponse is the part of the /people/enrich 200 body uncrm reads.
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
			CurrentRole *struct {
				Title string `json:"title"`
			} `json:"current_role"`
		} `json:"person"`
	} `json:"match"`
}

// Person calls POST /people/enrich {email}. Maps:
//
//	match.status not_found, or score < minScore      -> Found: false
//	422 FREE_EMAIL_DETECTED / DISPOSABLE_EMAIL_DETECTED -> Found: false (their list is longer than ours)
//	person.name.full, current_role.title, location.display, bio, avatar_url,
//	first social_urls entry on linkedin.com          -> PersonFacts
func (c *Client) Person(ctx context.Context, email crm.Email) (crm.PersonFacts, error) {
	resp, body, err := c.post(ctx, "/people/enrich", map[string]string{"email": email.String()})
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
	f := crm.PersonFacts{
		Found: true, Name: p.Name.Full, Bio: p.Bio, AvatarURL: p.AvatarURL,
		LinkedInURL: firstLinkedIn(p.SocialURLs), Raw: body,
	}
	if p.CurrentRole != nil {
		f.Title = p.CurrentRole.Title
	}
	if p.Location != nil {
		f.Location = p.Location.Display
	}
	return f, nil
}

// brandResponse is the part of the /brand/retrieve 200 body uncrm reads.
type brandResponse struct {
	Brand struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Logos       []struct {
			URL  string `json:"url"`
			Type string `json:"type"`
		} `json:"logos"`
		Socials []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"socials"`
		Address *struct {
			Country string `json:"country"`
		} `json:"address"`
		Employees *struct {
			Range string `json:"range"`
		} `json:"employees"`
		Industries *struct {
			EIC []struct {
				Industry string `json:"industry"`
			} `json:"eic"`
		} `json:"industries"`
	} `json:"brand"`
}

// Company calls POST /brand/retrieve {type: by_domain, domain,
// timeoutOpts: {milliseconds: 60000, behavior: fail}}. 60s avoids
// COLD_DOMAIN_TIMEOUT_TOO_LOW on uncached domains. Maps:
//
//	400 NOT_FOUND                     -> Found: false
//	brand.title, description, logos[type=logo|icon][0].url, employees.range,
//	industries.eic[0].industry, address.country, socials[type=linkedin].url
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
	f := crm.CompanyFacts{Found: true, Name: b.Title, Description: b.Description, Raw: body}
	for _, kind := range []string{"logo", "icon"} {
		for _, l := range b.Logos {
			if f.LogoURL == "" && l.Type == kind {
				f.LogoURL = l.URL
			}
		}
	}
	for _, s := range b.Socials {
		if f.LinkedInURL == "" && s.Type == "linkedin" {
			f.LinkedInURL = s.URL
		}
	}
	if b.Address != nil {
		f.Country = b.Address.Country
	}
	if b.Employees != nil {
		f.EmployeeRange = b.Employees.Range
	}
	if b.Industries != nil && len(b.Industries.EIC) > 0 {
		f.Industry = b.Industries.EIC[0].Industry
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

// firstLinkedIn returns the first URL on linkedin.com or a subdomain of it.
func firstLinkedIn(urls []string) string {
	for _, raw := range urls {
		s := raw
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			continue
		}
		if host := strings.ToLower(u.Hostname()); host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com") {
			return raw
		}
	}
	return ""
}
