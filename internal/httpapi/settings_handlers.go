package httpapi

import (
	"net/http"

	"github.com/uncrm/uncrm/internal/auth"
	"github.com/uncrm/uncrm/internal/crm"
)

// Human-only routes: Settings and login. Agents cannot mint keys, read
// provider secrets or invite users, because an agent holding one leaked key
// should not be able to turn it into more access.

func (s *server) setStages(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b struct {
		Stages  []crm.Stage             `json:"stages"`
		Renames map[crm.Stage]crm.Stage `json:"renames"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return noContent(w, s.svc.SetStages(r.Context(), a, b.Stages, b.Renames))
}

func (s *server) listProviders(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	ps, err := s.svc.ListProviders(r.Context())
	return respond(w, http.StatusOK, ps, err)
}

// connectProvider body {"secret": "..."}; an empty secret asks crm to issue
// one (IssueSecret providers). Responds {"secret": "..."} once.
func (s *server) connectProvider(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b struct {
		Secret string `json:"secret"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		secret, err := s.svc.ConnectProvider(r.Context(), a, r.PathValue("name"), b.Secret)
		return respond(w, http.StatusOK, map[string]string{"secret": secret}, err)
	})
}

func (s *server) disconnectProvider(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return s.recentOnly(r, func() error {
		return noContent(w, s.svc.DisconnectProvider(r.Context(), a, r.PathValue("name")))
	})
}

func (s *server) listKeys(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	keys, err := s.keys.List(r.Context())
	return respond(w, http.StatusOK, keys, keyStoreErr(err))
}

// createdKey is the one response that carries a key's plaintext.
type createdKey struct {
	Plaintext string `json:"key"`
	auth.Key
}

func (s *server) createKey(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		plaintext, k, err := s.keys.Create(r.Context(), a, b.Name)
		return respond(w, http.StatusCreated, createdKey{plaintext, k}, keyStoreErr(err))
	})
}

func (s *server) revokeKey(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return noContent(w, keyStoreErr(s.keys.Revoke(r.Context(), r.PathValue("id"))))
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	users, err := s.users.List(r.Context())
	return respond(w, http.StatusOK, users, err)
}

func (s *server) createInvite(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.recentOnly(r, func() error {
		token, err := s.users.Invite(r.Context(), a, b.Email)
		return respond(w, http.StatusCreated, map[string]string{"token": token}, err)
	})
}

func (s *server) me(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	writeJSON(w, http.StatusOK, a)
	return nil
}
