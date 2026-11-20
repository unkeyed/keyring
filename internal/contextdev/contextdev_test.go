package contextdev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
)

type reply struct {
	status int
	header http.Header
	body   string
}

type request struct {
	method, path, auth string
	body               map[string]any
}

// fake serves one canned reply per call and hands back what it received.
func fake(t *testing.T, r reply) (*Client, <-chan request) {
	t.Helper()
	got := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		in := request{method: req.Method, path: req.URL.Path, auth: req.Header.Get("Authorization")}
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &in.body)
		got <- in
		for k, v := range r.header {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.status)
		_, _ = io.WriteString(w, r.body)
	}))
	t.Cleanup(srv.Close)
	c := New("ctxt_secret_test")
	c.base = srv.URL + "/v1"
	return c, got
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func errBody(code, msg string) string {
	return `{"message":"` + msg + `","error_code":"` + code + `","request_id":"req_err"}`
}

var ada, _ = crm.ParseEmail("ada@stripe.com")

func TestPersonFound(t *testing.T) {
	body := fixture(t, "person_found.json")
	c, got := fake(t, reply{status: 200, body: body})

	f, err := c.Person(context.Background(), ada, "")
	if err != nil {
		t.Fatal(err)
	}
	want := crm.PersonFacts{
		Found: true, Name: "Ada Lovelace", Title: "Head of Engines", Location: "London, England",
		Bio: "Wrote the first published algorithm.", AvatarURL: "https://cdn.context.dev/p/ada.png",
		LinkedInURL: "https://www.linkedin.com/in/ada-lovelace",
		Socials: crm.Socials{
			"x": "https://x.com/ada", "github": "https://github.com/ada", "bluesky": "https://bsky.app/profile/ada.dev",
			"goodreads.com": "https://www.goodreads.com/user/show/1-ada", "website": "https://ada.dev",
		},
	}
	if string(f.Raw) != body {
		t.Fatal("Raw is not the response body")
	}
	f.Raw = nil
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("facts = %+v\nwant    %+v", f, want)
	}

	req := <-got
	if req.method != "POST" || req.path != "/v1/people/enrich" || req.auth != "Bearer ctxt_secret_test" || req.body["email"] != "ada@stripe.com" {
		t.Fatalf("request = %+v", req)
	}
	if _, ok := req.body["social_urls"]; ok {
		t.Fatalf("request sent social_urls %v with no LinkedIn URL known", req.body["social_urls"])
	}
}

func TestPersonSendsKnownLinkedIn(t *testing.T) {
	c, got := fake(t, reply{status: 200, body: `{"match":{"status":"not_found","score":null,"person":null}}`})
	if _, err := c.Person(context.Background(), ada, "linkedin.com/in/ada-lovelace"); err != nil {
		t.Fatal(err)
	}
	req := <-got
	if !reflect.DeepEqual(req.body["social_urls"], []any{"https://linkedin.com/in/ada-lovelace"}) || req.body["email"] != "ada@stripe.com" {
		t.Fatalf("request body = %v, want the email and the LinkedIn URL", req.body)
	}
}

func TestPersonNotFound(t *testing.T) {
	cases := map[string]reply{
		"not_found":         {status: 200, body: `{"match":{"status":"not_found","score":null,"person":null},"request_id":"r"}`},
		"weak match":        {status: 200, body: strings.Replace(fixture(t, "person_found.json"), `"score": 87`, `"score": 40`, 1)},
		"422 free mail":     {status: 422, body: errBody("FREE_EMAIL_DETECTED", "Free email providers are not supported")},
		"422 disposable":    {status: 422, body: errBody("DISPOSABLE_EMAIL_DETECTED", "Disposable email")},
		"200 with no score": {status: 200, body: `{"match":{"status":"candidate","person":{"name":{"full":"X"}}}}`},
	}
	for name, r := range cases {
		c, _ := fake(t, r)
		f, err := c.Person(context.Background(), ada, "")
		if err != nil || f.Found {
			t.Errorf("%s: facts %+v err %v, want not found and no error", name, f, err)
		}
	}
}

const media = "https://media.brand.dev/"

// The brand fixtures are context.dev's real answers for these domains, with
// email and phone blanked.
func TestCompanyFound(t *testing.T) {
	cases := []struct {
		domain, fixture string
		want            crm.CompanyFacts
	}{
		{"stripe.com", "brand_stripe.json", crm.CompanyFacts{
			Name: "Stripe", Tagline: "Financial infrastructure to grow your revenue.",
			EmployeeRange: "5001 to 10000", Industry: "Finance", Country: "United States",
			Headquarters: "South San Francisco, California", LinkedInURL: "https://linkedin.com/company/stripe",
			Socials: crm.Socials{"x": "https://x.com/stripe", "github": "https://github.com/stripe"},
			Links: crm.Links{"careers": "https://stripe.com/careers", "pricing": "https://stripe.com/pricing",
				"blog": "https://stripe.com/blog", "contact": "https://stripe.com/contact/sales"},
			Segments: []string{"Payments & Money Movement", "Financial Infrastructure & APIs", "Developer Tools & APIs",
				"Artificial Intelligence & Machine Learning"},
			LogoURL: media + "2231c684-cfad-4e73-9852-df19475f049e.svg",
			Logos: []crm.Logo{
				{URL: media + "2231c684-cfad-4e73-9852-df19475f049e.svg", Kind: crm.LogoIcon, OnLight: true, OnDark: true, Aspect: 1, Width: 512},
				{URL: media + "45b7b561-171b-4e78-be36-f14de0c85858.png", Kind: crm.LogoIcon, OnLight: true, OnDark: true, Aspect: 1, Width: 180},
				{URL: media + "2b622b22-1758-4fa2-8397-3e218b1c2ca0.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 2.4, Width: 60},
			},
		}},
		{"n8n.io", "brand_n8n.json", crm.CompanyFacts{
			Name: "n8n", Tagline: "No-code speed. Code flexibility. Workflow automation for any app or API.",
			EmployeeRange: "51 to 200", Industry: "Technology", Country: "Germany", Headquarters: "Berlin",
			LinkedInURL: "https://linkedin.com/company/n8n",
			Socials: crm.Socials{"discord": "https://discord.gg/n8n", "x": "https://x.com/n8n_io",
				"facebook": "https://facebook.com/n8nio", "youtube": "https://youtube.com/c/n8n-io"},
			Links:    crm.Links{"careers": "https://n8n.io/careers/", "pricing": "https://n8n.io/pricing/", "contact": "https://n8n.io/contact/"},
			Segments: []string{"Software (B2B)", "Developer Tools & APIs", "Artificial Intelligence & Machine Learning"},
			LogoURL:  media + "86717aa8-1acd-4f14-99d1-c2f1d50b6737.svg",
			Logos: []crm.Logo{
				{URL: media + "86717aa8-1acd-4f14-99d1-c2f1d50b6737.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 2.29, Width: 32},
				{URL: media + "cbf1c081-1258-4ad0-9381-e1a4428a7073.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 2.5, Width: 120},
				{URL: media + "f234c3d6-280d-40ca-974a-a0451f51c552.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 5, Width: 240},
			},
		}},
		{"posthog.com", "brand_posthog.json", crm.CompanyFacts{
			Name: "PostHog", Tagline: "Your product's context layer",
			EmployeeRange: "51 to 200", Industry: "Technology", Country: "United States", Headquarters: "San Francisco, California",
			LinkedInURL: "https://linkedin.com/company/posthog",
			Socials: crm.Socials{"x": "https://x.com/posthog", "github": "https://github.com/posthog/posthog/pull/135310",
				"facebook": "https://facebook.com/PostHog", "youtube": "https://youtube.com/c/posthog/videos"},
			Links: crm.Links{"careers": "https://posthog.com/careers", "pricing": "https://posthog.com/pricing",
				"blog": "https://posthog.com/blog/posthog-marketing"},
			Segments: []string{"Software (B2B)", "Data Infrastructure & Analytics", "Developer Tools & APIs"},
			LogoURL:  media + "ebcba24c-e665-4173-9171-7af286f76075.png",
			Logos: []crm.Logo{
				{URL: media + "ebcba24c-e665-4173-9171-7af286f76075.png", Kind: crm.LogoIcon, OwnBackground: true, OnLight: true, OnDark: true, Aspect: 1, Width: 512},
				{URL: media + "070f9244-6d7d-4d14-a82d-854129afdf3c.svg", Kind: crm.LogoWordmark, OwnBackground: true, OnLight: true, OnDark: true, Aspect: 1.86, Width: 52},
				{URL: media + "a4d4368a-78a0-409b-beef-5f75bbec4db8.png", Kind: crm.LogoWordmark, OnLight: true, OnDark: true, Aspect: 1.12, Width: 480},
				{URL: media + "153c9654-fc4e-43b4-a2f4-4969d9406356.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 1.86, Width: 52},
				{URL: media + "1fad5a9f-8ce1-4c73-9394-937229ab7f43.svg", Kind: crm.LogoWordmark, OnLight: true, Aspect: 5.71, Width: 160},
			},
		}},
	}
	for _, tc := range cases {
		c, got := fake(t, reply{status: 200, body: fixture(t, tc.fixture)})
		d, _ := crm.ParseDomain(tc.domain)
		f, err := c.Company(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		f.Raw, f.Description = nil, ""
		tc.want.Found = true
		if !reflect.DeepEqual(f, tc.want) {
			t.Errorf("%s facts = %+v\nwant %+v", tc.domain, f, tc.want)
		}
		req := <-got
		timeout, _ := req.body["timeoutOpts"].(map[string]any)
		if req.path != "/v1/brand/retrieve" || req.body["type"] != "by_domain" || req.body["domain"] != tc.domain ||
			timeout["milliseconds"] != float64(60000) || timeout["behavior"] != "fail" {
			t.Fatalf("request = %+v", req)
		}
	}
}

func TestCompanyStockAndUnsafeURLs(t *testing.T) {
	body := strings.NewReplacer(
		`"slogan":`, `"stock": {"ticker": "STRP", "exchange": "NASDAQ"}, "slogan":`,
		`"https://x.com/stripe"`, `"javascript:alert(1)"`,
		`"https://stripe.com/blog"`, `"ftp://stripe.com/blog"`,
	).Replace(fixture(t, "brand_stripe.json"))
	c, _ := fake(t, reply{status: 200, body: body})
	d, _ := crm.ParseDomain("stripe.com")
	f, err := c.Company(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if f.Stock != "NASDAQ: STRP" {
		t.Errorf("stock = %q, want NASDAQ: STRP", f.Stock)
	}
	if _, ok := f.Socials["x"]; ok {
		t.Errorf("socials kept a javascript: URL: %v", f.Socials)
	}
	if _, ok := f.Links["blog"]; ok {
		t.Errorf("links kept an ftp: URL: %v", f.Links)
	}
}

func TestCompanyNotFound(t *testing.T) {
	c, _ := fake(t, reply{status: 400, body: errBody("NOT_FOUND", "Brand not found")})
	d, _ := crm.ParseDomain("nobody.example")
	f, err := c.Company(context.Background(), d)
	if err != nil || f.Found {
		t.Fatalf("facts %+v err %v, want not found and no error", f, err)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	cases := []struct {
		name       string
		reply      reply
		kind       crm.EnrichErrorKind
		retryAfter time.Duration
		msg        string
	}{
		{"429", reply{status: 429, header: http.Header{"Retry-After": {"30"}}, body: errBody("RATE_LIMITED", "slow down")},
			crm.EnrichThrottled, 30 * time.Second, "context.dev 429 RATE_LIMITED: slow down"},
		{"401", reply{status: 401, body: errBody("UNAUTHORIZED", "Invalid API key")},
			crm.EnrichHalted, 0, "context.dev 401 UNAUTHORIZED: Invalid API key"},
		{"401 for an unknown key", reply{status: 401, body: errBody("NOT_FOUND", "Endpoint not found")},
			crm.EnrichHalted, 0, "context.dev 401 NOT_FOUND: Endpoint not found"},
		{"402", reply{status: 402, body: errBody("USAGE_EXCEEDED", "Monthly credits used up")},
			crm.EnrichHalted, 0, "context.dev 402 USAGE_EXCEEDED: Monthly credits used up"},
		{"403", reply{status: 403, body: errBody("DISABLED", "Key disabled")}, crm.EnrichHalted, 0, ""},
		{"408", reply{status: 408, body: errBody("REQUEST_TIMEOUT", "timed out")}, crm.EnrichRetryable, 0, ""},
		{"500 without a JSON body", reply{status: 500, body: "upstream exploded"}, crm.EnrichRetryable, 0, "context.dev 500"},
		{"400", reply{status: 400, body: errBody("INPUT_VALIDATION_ERROR", "bad email")}, crm.EnrichPermanent, 0, ""},
		{"422 other than free mail", reply{status: 422, body: errBody("INPUT_VALIDATION_ERROR", "bad")}, crm.EnrichPermanent, 0, ""},
	}
	for _, c := range cases {
		client, _ := fake(t, c.reply)
		_, err := client.Person(context.Background(), ada, "")
		var ee *crm.EnrichError
		if !errors.As(err, &ee) || ee.Kind != c.kind || ee.RetryAfter != c.retryAfter || (c.msg != "" && ee.Msg != c.msg) {
			t.Errorf("%s: err = %#v, want kind %d retry after %v msg %q", c.name, err, c.kind, c.retryAfter, c.msg)
		}
	}

	brand, _ := fake(t, reply{status: 402, body: errBody("USAGE_EXCEEDED", "out of credits")})
	d, _ := crm.ParseDomain("stripe.com")
	_, err := brand.Company(context.Background(), d)
	var ee *crm.EnrichError
	if !errors.As(err, &ee) || ee.Kind != crm.EnrichHalted {
		t.Errorf("brand 402: err = %v, want halted", err)
	}
}

func TestNetworkErrorIsPlain(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := New("k")
	c.base = srv.URL
	srv.Close()

	_, err := c.Person(context.Background(), ada, "")
	var ee *crm.EnrichError
	if err == nil || errors.As(err, &ee) {
		t.Fatalf("err = %v, want a plain transport error, which the job machine retries", err)
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"120":                           2 * time.Minute,
		"Thu, 19 Nov 2026 12:01:30 GMT": 90 * time.Second,
		"":                              0,
		"soon":                          0,
		"-5":                            0,
	}
	for in, want := range cases {
		if got := retryAfter(in, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLogoOrder(t *testing.T) {
	logo := func(url, typ, mode string, width int, aspect float64, colors ...string) brandLogo {
		l := brandLogo{URL: url, Type: typ, Mode: mode}
		l.Resolution.Width, l.Resolution.AspectRatio = width, aspect
		for _, c := range colors {
			l.Colors = append(l.Colors, struct {
				Hex string `json:"hex"`
			}{c})
		}
		return l
	}
	cases := []struct {
		name  string
		logos []brandLogo
		want  []string
	}{
		{"icons before wordmarks; own background, then art that reads on both themes", []brandLogo{
			logo("https://a/wordmark-own.svg", "logo", "has_opaque_background", 0, 1),
			logo("https://a/icon-dark.svg", "icon", "dark", 0, 1),
			logo("https://a/icon-light.svg", "icon", "light", 0, 1),
			logo("https://a/icon-both.svg", "icon", "dark", 0, 1, "#543cfc", "#a494fc"),
			logo("https://a/icon-own.png", "icon", "has_opaque_background", 256, 1),
		}, []string{"https://a/icon-own.png", "https://a/icon-both.svg", "https://a/icon-dark.svg", "https://a/icon-light.svg", "https://a/wordmark-own.svg"}},
		{"in one rank, sharp before blurry, then the squarest", []brandLogo{
			logo("https://a/small.png", "icon", "light", 64, 1),
			logo("https://a/wide.svg", "icon", "light", 20, 1.6),
			logo("https://a/big.png", "icon", "light", 400, 1),
			logo("https://a/square.svg", "icon", "light", 20, 1),
		}, []string{"https://a/big.png", "https://a/square.svg", "https://a/wide.svg", "https://a/small.png"}},
		{"a raster under 32px is dropped, an SVG of any size is kept", []brandLogo{
			logo("https://a/tiny.png", "icon", "light", 16, 1),
			logo("https://a/tiny.svg", "icon", "light", 16, 1),
		}, []string{"https://a/tiny.svg"}},
		{"only web URLs are kept, and a missing scheme becomes https", []brandLogo{
			logo("javascript:alert(1)", "icon", "light", 0, 1),
			logo("data:image/svg+xml,<svg/>", "icon", "light", 0, 1),
			logo("cdn.example/logo.svg", "logo", "light", 0, 2),
		}, []string{"https://cdn.example/logo.svg"}},
		{"ties keep context.dev's order", []brandLogo{
			logo("https://a/one.svg", "icon", "dark", 512, 1),
			logo("https://a/two.svg", "icon", "dark", 512, 1),
		}, []string{"https://a/one.svg", "https://a/two.svg"}},
		{"no logos", nil, []string{}},
	}
	for _, tc := range cases {
		got := []string{}
		for _, l := range logos(tc.logos) {
			got = append(got, l.URL)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Colours are ones context.dev returned for real logos.
func TestReadsOn(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		colors      []string
		light, dark bool
	}{
		{"Stripe's purple icon, labelled dark, reads on both", "dark", []string{"#543cfc", "#a494fc"}, true, true},
		{"black art reads on light only", "light", []string{"#141424"}, true, false},
		{"grey line art too dim for dark", "light", []string{"#4d4d4d", "#313131"}, true, false},
		{"white art reads on dark only", "light", []string{"#ffffff"}, false, true},
		{"no colours: trust a light label", "light", nil, true, false},
		{"no colours: trust a dark label", "dark", nil, false, true},
		{"unparseable colours fall back to the label", "dark", []string{"purple"}, false, true},
	}
	for _, tc := range cases {
		l := brandLogo{Mode: tc.mode}
		for _, c := range tc.colors {
			l.Colors = append(l.Colors, struct {
				Hex string `json:"hex"`
			}{c})
		}
		if light, dark := readsOn(l); light != tc.light || dark != tc.dark {
			t.Errorf("%s: got light=%v dark=%v, want %v %v", tc.name, light, dark, tc.light, tc.dark)
		}
	}
}

// context.dev returned Supabase's state as its country.
func TestHeadquartersSkipsAStateThatIsTheCountry(t *testing.T) {
	body := strings.NewReplacer(
		`"city": "South San Francisco"`, `"city": "Austin"`,
		`"state_province": "California"`, `"state_province": "United States"`,
	).Replace(fixture(t, "brand_stripe.json"))
	c, _ := fake(t, reply{status: 200, body: body})
	d, _ := crm.ParseDomain("stripe.com")
	f, err := c.Company(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if f.Headquarters != "Austin" || f.Country != "United States" {
		t.Errorf("headquarters %q, country %q; want Austin, United States", f.Headquarters, f.Country)
	}
}
