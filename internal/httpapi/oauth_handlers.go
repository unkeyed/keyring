package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
)

// mountOAuth adds the OAuth endpoints MCP clients discover and call. Their
// parameters and errors follow the RFCs rather than the REST conventions,
// so they live outside routes and openapi.yaml; the metadata documents
// describe them. None reads a cookie, so any origin may call them.
func (s *server) mountOAuth(mux *http.ServeMux) {
	prm := mcpauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               s.oauth.Resource(),
		AuthorizationServers:   []string{s.oauth.Issuer()},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "uncrm",
	})
	mux.Handle("/.well-known/oauth-protected-resource", prm)
	mux.Handle("/.well-known/oauth-protected-resource/mcp", prm)
	mux.Handle("/.well-known/oauth-authorization-server", anyOrigin(http.MethodGet, s.authServerMetadata))
	mux.Handle("/oauth/register", anyOrigin(http.MethodPost, s.registerClient))
	mux.Handle("/oauth/token", anyOrigin(http.MethodPost, s.token))
	mux.Handle("/oauth/revoke", anyOrigin(http.MethodPost, s.revokeToken))
	mux.HandleFunc("GET /oauth/authorize", s.authorize)
}

// anyOrigin serves method with CORS open to every origin, for MCP clients
// that run in a browser.
func anyOrigin(method string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		switch r.Method {
		case method:
			h(w, r)
		case http.MethodOptions:
			w.Header().Set("Access-Control-Allow-Methods", method)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", method)
			writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use "+method)
		}
	})
}

// authServerMetadata is RFC 8414 metadata. uncrm issues no scopes, so it
// lists none.
func (s *server) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	iss := s.oauth.Issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                     iss,
		"authorization_endpoint":                     iss + "/oauth/authorize",
		"token_endpoint":                             iss + "/oauth/token",
		"registration_endpoint":                      iss + "/oauth/register",
		"revocation_endpoint":                        iss + "/oauth/revoke",
		"response_types_supported":                   []string{"code"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []string{"S256"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
	})
}

// registerClient is RFC 7591 dynamic registration. Unknown metadata is
// ignored, as section 2 requires.
func (s *server) registerClient(w http.ResponseWriter, r *http.Request) {
	var m oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&m); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be a JSON object of client metadata")
		return
	}
	res, err := s.oauth.Register(r.Context(), s.users.ClientOf(r), &m)
	if err != nil {
		s.oauthFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// authorize is the authorization endpoint. A sound request goes on to the
// consent page, which signs the person in first if it has to. A refused
// one is explained on that page.
func (s *server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	handle, err := s.oauth.Authorize(r.Context(), auth.AuthorizeRequest{
		ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), ResponseType: q.Get("response_type"),
		CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: q.Get("code_challenge_method"),
		State: q.Get("state"), Resource: q.Get("resource"),
	})
	var refused *auth.OAuthError
	switch {
	case err == nil:
		http.Redirect(w, r, "/oauth/consent?request="+url.QueryEscape(handle), http.StatusFound)
	case errors.As(err, &refused):
		http.Redirect(w, r, "/oauth/consent?error="+url.QueryEscape(refused.Code), http.StatusFound)
	default:
		s.writeError(w, r, err)
	}
}

// token is the token endpoint. A public client names itself with
// client_id, in the form or as the user of HTTP Basic with no password.
func (s *server) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the body must be application/x-www-form-urlencoded")
		return
	}
	f := r.PostForm
	client := f.Get("client_id")
	if user, _, ok := r.BasicAuth(); ok && client == "" {
		client, _ = url.QueryUnescape(user)
	}
	res, err := s.oauth.Exchange(r.Context(), s.users.ClientOf(r), auth.TokenRequest{
		GrantType: f.Get("grant_type"), ClientID: client, Code: f.Get("code"), RedirectURI: f.Get("redirect_uri"),
		CodeVerifier: f.Get("code_verifier"), RefreshToken: f.Get("refresh_token"), Resource: f.Get("resource"),
	})
	if err != nil {
		s.oauthFailed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// revokeToken is RFC 7009 revocation. Any token of a connection, access or
// refresh, ends the connection.
func (s *server) revokeToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "send the token to revoke as a form field named token")
		return
	}
	if err := s.oauth.Revoke(r.Context(), r.PostForm.Get("token")); err != nil {
		s.oauthFailed(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) oauthFailed(w http.ResponseWriter, r *http.Request, err error) {
	var refused *auth.OAuthError
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &refused):
		writeOAuthError(w, http.StatusBadRequest, refused.Code, refused.Description)
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.RetryAfter.Seconds())))
		writeOAuthError(w, http.StatusTooManyRequests, "invalid_request", throttled.Error())
	default:
		s.writeError(w, r, err)
	}
}

func writeOAuthError(w http.ResponseWriter, code int, oauthCode, description string) {
	writeJSON(w, code, map[string]string{"error": oauthCode, "error_description": description})
}

// Consent and connected apps: session routes in the table.

func (s *server) consentRequest(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	c, err := s.oauth.Consent(r.Context(), r.PathValue("id"))
	return respond(w, http.StatusOK, c, err)
}

// decideConsent body {"approve": bool}; responds {"redirect": url}, where
// the consent page sends the browser. Approving hands out access the way a
// new API key does, so it wants a recent sign-in; declining does not.
func (s *server) decideConsent(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b struct {
		Approve bool `json:"approve"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	decide := func() error {
		to, err := s.oauth.Decide(r.Context(), r.PathValue("id"), a, b.Approve)
		return respond(w, http.StatusOK, map[string]string{"redirect": to}, err)
	}
	if !b.Approve {
		return decide()
	}
	return s.recentOnly(r, decide)
}

func (s *server) listApps(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	apps, err := s.oauth.Apps(r.Context())
	return respond(w, http.StatusOK, apps, err)
}

func (s *server) revokeApp(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return noContent(w, s.oauth.RevokeApp(r.Context(), r.PathValue("id")))
}
