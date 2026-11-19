package httpapi

import (
	"net/http"

	"github.com/uncrm/uncrm/internal/crm"
)

// Human-only routes: Settings and login. Agents cannot mint keys, read
// provider secrets or invite users, because an agent holding one leaked key
// should not be able to turn it into more access.

func (s *server) setStages(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) listProviders(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

// connectProvider body {"secret": "..."}; an empty secret asks crm to issue
// one (IssueSecret providers). Responds {"secret": "..."} once.
func (s *server) connectProvider(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) disconnectProvider(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) listKeys(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

// createKey responds {"key": "<plaintext>", ...Key}; the plaintext is shown once.
func (s *server) createKey(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) revokeKey(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) createInvite(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) me(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	writeJSON(w, http.StatusOK, a)
	return nil
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

// login body {"email", "password"}; sets the session cookie.
func (s *server) login(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) logout(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) acceptInvite(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}
