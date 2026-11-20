package auth

import (
	"net/http"
	"strings"

	"github.com/unkeyed/keyring/internal/crm"
)

// Via says how a request authenticated, which decides the routes it may use.
type Via int

const (
	ViaAPIKey Via = iota + 1
	ViaSession
)

// Authenticator resolves an inbound request to an actor. OAuth issues the
// tokens /mcp accepts; Caller never accepts them.
type Authenticator struct {
	Keys  KeyStore
	Users *Users
	OAuth *OAuthServer
}

// Caller reads "Authorization: Bearer <key>" first, then the session cookie.
// A present but invalid credential never falls through to the other one.
// Neither present is ErrUnauthorized, an invalid key ErrInvalidKey, an
// invalid session ErrUnauthorized. Any other error means the key store or
// database failed, and the request must fail with it.
func (a *Authenticator) Caller(r *http.Request) (crm.Actor, Via, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return crm.Actor{}, 0, ErrUnauthorized
		}
		actor, err := a.Keys.Verify(r.Context(), strings.TrimSpace(token))
		if err != nil {
			return crm.Actor{}, 0, err
		}
		return actor, ViaAPIKey, nil
	}
	actor, err := a.Users.Session(r.Context(), a.Users.Cookie(r, SessionCookie))
	if err != nil {
		return crm.Actor{}, 0, err
	}
	return actor, ViaSession, nil
}
