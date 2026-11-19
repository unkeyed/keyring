package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/signup"
)

var errNotImplemented = errors.New("not implemented")

func (s *server) healthz(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	w.WriteHeader(http.StatusOK) // process only; Unkey advises not to check the DB here
	return nil
}

func (s *server) openapi(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	w.Header().Set("Content-Type", "application/yaml")
	_, err := w.Write(openapiYAML)
	return err
}

func (s *server) receiveWebhook(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", crm.ErrInvalid, err)
	}
	d := signup.Delivery{Header: r.Header, Body: body, ReceivedAt: time.Now()}
	if err := s.svc.ReceiveWebhook(r.Context(), r.PathValue("provider"), d); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// signupBody is the generic POST /v1/signups body for any auth system.
// Only email is required; crm.Ingest fills the defaults.
type signupBody struct {
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	AvatarURL     string    `json:"avatar_url"`
	EmailVerified bool      `json:"email_verified"`
	Source        string    `json:"source"`
	ExternalID    string    `json:"external_id"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func (s *server) createSignup(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b signupBody
	if err := readJSON(r, &b); err != nil {
		return err
	}
	su := signup.Signup{
		Source:        b.Source,
		Kind:          signup.Created,
		ExternalID:    b.ExternalID,
		Email:         b.Email,
		EmailVerified: b.EmailVerified,
		Name:          b.Name,
		AvatarURL:     b.AvatarURL,
		OccurredAt:    b.OccurredAt,
	}
	res, err := s.svc.Ingest(r.Context(), a, su)
	if err != nil {
		return err
	}
	code := http.StatusOK
	if res.Created {
		code = http.StatusCreated
	}
	writeJSON(w, code, res)
	return nil
}

// searchContacts maps query params q, tag, stage, provider, company_id,
// signed_up_after, signed_up_before (RFC 3339), cursor, limit onto
// crm.ContactQuery.
func (s *server) searchContacts(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) getContact(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.svc.GetContact(r.Context(), crm.ContactID(id))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (s *server) updateContact(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var p crm.ContactPatch
	if err := readJSON(r, &p); err != nil {
		return err
	}
	c, err := s.svc.UpdateContact(r.Context(), a, crm.ContactID(id), p)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

// resetContactField responds with the contact, like updateContact.
func (s *server) resetContactField(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

// mergeContacts folds the contact in the body ({"drop": id}) into the one in
// the path and responds with the surviving contact.
func (s *server) mergeContacts(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) addNote(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) deleteNote(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) enrichContact(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) searchCompanies(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) getCompany(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) updateCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) resetCompanyField(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) enrichCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	return errNotImplemented
}

func (s *server) listActivity(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) listTags(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) listStages(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func (s *server) status(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return errNotImplemented
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: bad %s", crm.ErrInvalid, name)
	}
	return id, nil
}
