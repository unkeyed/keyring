package httpapi

import (
	"net/http"
	"net/url"
)

// contentSecurityPolicy allows only Keyring's own scripts, so injected markup
// cannot run code. Company logos load from any https origin, the TOTP QR
// code is a data: URL, and React sets inline style attributes.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' https: data:; style-src 'self' 'unsafe-inline'; " +
	"frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'"

// SecurityHeaders sets the browser hardening headers on every response,
// and HSTS when Keyring is served over https.
//
// Every response defaults to Cache-Control: no-store with Vary: Cookie,
// Authorization, because most of them depend on who is asking and Keyring
// may sit behind a shared cache such as a CDN or Unkey's gateway. The few
// responses that are the same for everyone (the UI's files, openapi.yaml)
// replace both headers.
func SecurityHeaders(publicURL *url.URL, next http.Handler) http.Handler {
	hsts := publicURL.Scheme == "https"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		h.Set("Vary", "Cookie, Authorization")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
