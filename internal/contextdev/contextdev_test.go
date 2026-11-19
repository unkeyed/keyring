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

	f, err := c.Person(context.Background(), ada)
	if err != nil {
		t.Fatal(err)
	}
	want := crm.PersonFacts{
		Found: true, Name: "Ada Lovelace", Title: "Head of Engines", Location: "London, England",
		Bio: "Wrote the first published algorithm.", AvatarURL: "https://cdn.context.dev/p/ada.png",
		LinkedInURL: "https://www.linkedin.com/in/ada-lovelace",
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
		f, err := c.Person(context.Background(), ada)
		if err != nil || f.Found {
			t.Errorf("%s: facts %+v err %v, want not found and no error", name, f, err)
		}
	}
}

func TestCompanyFound(t *testing.T) {
	c, got := fake(t, reply{status: 200, body: fixture(t, "brand_found.json")})
	stripe, _ := crm.ParseDomain("stripe.com")

	f, err := c.Company(context.Background(), stripe)
	if err != nil {
		t.Fatal(err)
	}
	f.Raw = nil
	want := crm.CompanyFacts{
		Found: true, Name: "Stripe", Description: "Financial infrastructure for the internet.",
		LogoURL: "https://cdn.context.dev/b/stripe-logo.svg", EmployeeRange: "5001 to 10000", Industry: "Finance",
		Country: "United States", LinkedInURL: "https://www.linkedin.com/company/stripe",
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("facts = %+v\nwant    %+v", f, want)
	}

	req := <-got
	timeout, _ := req.body["timeoutOpts"].(map[string]any)
	if req.path != "/v1/brand/retrieve" || req.body["type"] != "by_domain" || req.body["domain"] != "stripe.com" ||
		timeout["milliseconds"] != float64(60000) || timeout["behavior"] != "fail" {
		t.Fatalf("request = %+v", req)
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
		_, err := client.Person(context.Background(), ada)
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

	_, err := c.Person(context.Background(), ada)
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
