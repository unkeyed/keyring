// Package contextdev implements crm.Enricher over context.dev's two
// synchronous endpoints with net/http. The official SDK is not used: we need
// two POSTs, and its built-in retries would hide 429s from the job state
// machine, which owns retry policy.
package contextdev

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
)

const baseURL = "https://api.context.dev/v1"

// minScore drops weak person matches; context.dev returns them with a 0-100
// score. A guess to tune against real data.
const minScore = 70

type Client struct {
	key  string
	http *http.Client
}

func New(apiKey string) *Client {
	return &Client{key: apiKey, http: &http.Client{Timeout: 90 * time.Second}}
}

// Person calls POST /people/enrich {email}. Maps:
//
//	match.status not_found, or score < minScore      -> Found: false
//	422 FREE_EMAIL_DETECTED / DISPOSABLE_EMAIL_DETECTED -> Found: false (their list is longer than ours)
//	person.name.full, current_role.title, location.display, bio, avatar_url,
//	first social_urls entry on linkedin.com          -> PersonFacts
func (c *Client) Person(ctx context.Context, email crm.Email) (crm.PersonFacts, error) {
	return crm.PersonFacts{}, errors.New("not implemented")
}

// Company calls POST /brand/retrieve {type: by_domain, domain,
// timeoutOpts: {milliseconds: 60000, behavior: fail}}. 60s avoids
// COLD_DOMAIN_TIMEOUT_TOO_LOW on uncached domains. Maps:
//
//	400 NOT_FOUND                     -> Found: false
//	brand.title, description, logos[type=logo|icon][0].url, employees.range,
//	industries.eic[0].industry, address.country, socials[type=linkedin].url
func (c *Client) Company(ctx context.Context, domain crm.Domain) (crm.CompanyFacts, error) {
	return crm.CompanyFacts{}, errors.New("not implemented")
}

// classify turns a non-2xx response into *crm.EnrichError:
//
//	429, 402 USAGE_EXCEEDED -> Throttled, RetryAfter from Retry-After (402: 6h)
//	408, 5xx                -> Retryable
//	other 4xx               -> permanent (never retry-loop 4xx: context.dev
//	                           disables free keys after repeated failures)
func classify(resp *http.Response) error {
	return errors.New("not implemented")
}
