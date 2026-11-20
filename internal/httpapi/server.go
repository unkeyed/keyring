// Package httpapi serves the REST API, which is also the UI's API: the React
// app calls the same /v1 routes with a session cookie that agents call with
// an API key. Handlers decode, call one crm.Service method, encode. Any
// logic beyond that belongs in crm.
package httpapi

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/signup"
)

// access is who may call a route.
type access int

const (
	public access = iota // no caller check: health, login, webhooks (signature checked in crm)
	anyone               // API key or session
	humans               // session only: key management, team, provider secrets
)

type handlerFunc func(s *server, w http.ResponseWriter, r *http.Request, a crm.Actor) error

type route struct {
	pattern string
	access  access
	handle  handlerFunc
}

// routes is the only list of HTTP endpoints. TestRoutesMatchOpenAPI fails
// when openapi.yaml and this table disagree.
var routes = []route{
	{"GET /healthz", public, (*server).healthz},
	{"GET /v1/openapi.yaml", public, (*server).openapi},
	{"POST /webhooks/{provider}", public, (*server).receiveWebhook},

	{"POST /v1/signups", anyone, (*server).createSignup},
	{"GET /v1/contacts", anyone, (*server).searchContacts},
	{"GET /v1/contacts/{id}", anyone, (*server).getContact},
	{"PATCH /v1/contacts/{id}", anyone, (*server).updateContact},
	{"DELETE /v1/contacts/{id}/manual_fields/{field}", anyone, (*server).resetContactField},
	{"POST /v1/contacts/{id}/merge", anyone, (*server).mergeContacts},
	{"POST /v1/contacts/{id}/notes", anyone, (*server).addNote},
	{"DELETE /v1/notes/{id}", anyone, (*server).deleteNote},
	{"POST /v1/contacts/{id}/enrich", anyone, (*server).enrichContact},
	{"GET /v1/companies", anyone, (*server).searchCompanies},
	{"GET /v1/companies/{id}", anyone, (*server).getCompany},
	{"PATCH /v1/companies/{id}", anyone, (*server).updateCompany},
	{"DELETE /v1/companies/{id}/manual_fields/{field}", anyone, (*server).resetCompanyField},
	{"POST /v1/companies/{id}/enrich", anyone, (*server).enrichCompany},
	{"GET /v1/activity", anyone, (*server).listActivity},
	{"GET /v1/tags", anyone, (*server).listTags},
	{"GET /v1/stages", anyone, (*server).listStages},
	{"GET /v1/status", anyone, (*server).status},

	{"PUT /v1/stages", humans, (*server).setStages},
	{"GET /v1/providers", humans, (*server).listProviders},
	{"PUT /v1/providers/{name}", humans, (*server).connectProvider},
	{"DELETE /v1/providers/{name}", humans, (*server).disconnectProvider},
	{"GET /v1/keys", humans, (*server).listKeys},
	{"POST /v1/keys", humans, (*server).createKey},
	{"DELETE /v1/keys/{id}", humans, (*server).revokeKey},
	{"GET /v1/users", humans, (*server).listUsers},
	{"DELETE /v1/users/{id}", humans, (*server).removeUser},
	{"POST /v1/invites", humans, (*server).createInvite},
	{"GET /v1/me", humans, (*server).me},
	{"GET /v1/account", humans, (*server).account},
	{"POST /v1/account/passkeys/begin", humans, (*server).addPasskeyBegin},
	{"POST /v1/account/passkeys", humans, (*server).addPasskey},
	{"DELETE /v1/account/passkeys/{id}", humans, (*server).removePasskey},
	{"DELETE /v1/account/identities/{provider}", humans, (*server).unlinkIdentity},
	{"POST /v1/account/totp/begin", humans, (*server).totpBegin},
	{"POST /v1/account/totp", humans, (*server).totpFinish},
	{"DELETE /v1/account/totp", humans, (*server).totpDisable},
	{"POST /v1/account/recovery-codes", humans, (*server).regenerateRecoveryCodes},
	{"PUT /v1/account/password", humans, (*server).setPassword},
	{"DELETE /v1/account/password", humans, (*server).removePassword},
	{"DELETE /v1/account/sessions/{id}", humans, (*server).revokeSession},
	{"POST /v1/account/reauth/passkey/begin", humans, (*server).reauthPasskeyBegin},
	{"POST /v1/account/reauth/passkey", humans, (*server).reauthPasskey},
	{"POST /v1/account/reauth/code", humans, (*server).reauthCode},

	{"GET /auth/options", public, (*server).authOptions},
	{"POST /auth/passkey/begin", public, (*server).passkeyBegin},
	{"POST /auth/passkey/finish", public, (*server).passkeyFinish},
	{"POST /auth/password", public, (*server).passwordLogin},
	{"GET /auth/second-factor", public, (*server).secondFactor},
	{"POST /auth/second-factor/code", public, (*server).secondFactorCode},
	{"POST /auth/second-factor/passkey/begin", public, (*server).secondFactorPasskeyBegin},
	{"POST /auth/second-factor/passkey/finish", public, (*server).secondFactorPasskeyFinish},
	{"GET /auth/oauth/{provider}/start", public, (*server).oauthStart},
	{"GET /auth/oauth/{provider}/callback", public, (*server).oauthCallback},
	{"GET /auth/setup/{token}", public, (*server).checkSetup},
	{"GET /auth/invites/{token}", public, (*server).inviteInfo},
	{"POST /auth/signup/passkey/begin", public, (*server).signupPasskeyBegin},
	{"POST /auth/signup/passkey/finish", public, (*server).signupPasskeyFinish},
	{"POST /auth/logout", public, (*server).logout},
}

//go:embed openapi.yaml
var openapiYAML []byte

type server struct {
	svc   *crm.Service
	authn *auth.Authenticator
	keys  auth.KeyStore
	users *auth.Users
	log   *slog.Logger
}

// NewMux mounts every route on a new mux; main adds /mcp and the UI's "/"
// to the same mux. Unmatched /v1/ paths get a JSON 404 instead of falling
// through to the UI. Browser-reachable routes are wrapped in
// http.CrossOriginProtection, which rejects cross-site unsafe requests and
// lets non-browser clients (no Sec-Fetch-Site / Origin) through.
func NewMux(svc *crm.Service, authn *auth.Authenticator, log *slog.Logger) *http.ServeMux {
	s := &server{svc: svc, authn: authn, keys: authn.Keys, users: authn.Users, log: log}
	csrf := http.NewCrossOriginProtection()
	mux := http.NewServeMux()
	for _, rt := range routes {
		h := s.wrap(rt)
		if rt.pattern != "POST /webhooks/{provider}" {
			h = csrf.Handler(h)
		}
		mux.Handle(rt.pattern, h)
	}
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such route; the API is described at GET /v1/openapi.yaml"})
	})
	return mux
}

var (
	errForbidden   = errors.New("forbidden: API keys cannot use this route; sign in to the UI")
	errUnavailable = errors.New("unavailable")
)

func (s *server) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var actor crm.Actor
		if rt.access != public {
			a, via, err := s.authn.Caller(r)
			if err != nil {
				s.writeError(w, r, callerErr(err))
				return
			}
			if rt.access == humans && via != auth.ViaSession {
				s.writeError(w, r, errForbidden)
				return
			}
			actor = a
		}
		if err := rt.handle(s, w, r, actor); err != nil {
			s.writeError(w, r, err)
		}
	})
}

// callerErr marks a failure to authenticate that is not the caller's fault:
// Caller returns ErrUnauthorized or ErrInvalidKey for bad credentials, and
// anything else means the key store or database could not decide.
func callerErr(err error) error {
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrInvalidKey) {
		return err
	}
	return fmt.Errorf("%w: %w", errUnavailable, err)
}

// keyStoreErr does the same for key management. A KeyStore reports the
// caller's mistakes as crm.ErrInvalid or crm.ErrNotFound; any other error
// means Postgres or Unkey could not answer.
func keyStoreErr(err error) error {
	if err == nil || errors.Is(err, crm.ErrInvalid) || errors.Is(err, crm.ErrNotFound) {
		return err
	}
	return fmt.Errorf("%w: %w", errUnavailable, err)
}

// writeError is the single error-to-status mapping for REST.
// Body: {"error": "<message>"}, plus "request_id" on a 5xx, whose cause is
// logged under the same id.
func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := statusOf(err)
	var throttled *auth.ThrottledError
	if errors.As(err, &throttled) {
		w.Header().Set("Retry-After", strconv.Itoa(int(throttled.RetryAfter.Seconds())))
	}
	switch {
	case errors.Is(err, auth.ErrReauth):
		writeJSON(w, code, map[string]string{"error": err.Error(), "code": "reauth_required"})
		return
	case code < 500:
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	id := requestID()
	s.log.Error("request failed", "request_id", id, "status", code, "method", r.Method, "path", r.URL.Path, "err", err)
	msg := "internal error"
	if code == http.StatusServiceUnavailable {
		msg = "the database or key store is unavailable; retry shortly"
	}
	writeJSON(w, code, map[string]string{"error": msg, "request_id": id})
}

func statusOf(err error) int {
	var throttled *auth.ThrottledError
	var netErr net.Error
	switch {
	case errors.Is(err, crm.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, auth.ErrUnauthorized), errors.Is(err, auth.ErrInvalidKey), errors.Is(err, signup.ErrBadSignature):
		return http.StatusUnauthorized
	case errors.Is(err, errForbidden), errors.Is(err, auth.ErrReauth):
		return http.StatusForbidden
	case errors.Is(err, crm.ErrNotFound), errors.Is(err, crm.ErrUnknownProvider), errors.Is(err, crm.ErrNotConnected):
		return http.StatusNotFound
	case errors.Is(err, crm.ErrConflict):
		return http.StatusConflict
	case errors.As(err, &throttled):
		return http.StatusTooManyRequests
	case errors.Is(err, errUnavailable), errors.As(err, &netErr):
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON decodes a body of at most 1 MiB, rejecting unknown fields, and
// wraps failures in crm.ErrInvalid.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: request body is empty, want a JSON object", crm.ErrInvalid)
		}
		return fmt.Errorf("%w: bad JSON body: %v", crm.ErrInvalid, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: request body holds more than one JSON value", crm.ErrInvalid)
	}
	return nil
}
