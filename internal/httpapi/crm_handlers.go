package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/uncrm/uncrm/internal/crm"
	"github.com/uncrm/uncrm/internal/signup"
)

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

func (s *server) searchContacts(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	p := queryParams(r, "q", "email", "tag", "stage", "provider", "company_id", "signed_up_after", "signed_up_before", "cursor", "limit")
	q := crm.ContactQuery{
		Text:           p.str("q"),
		Email:          p.str("email"),
		Tag:            crm.Tag(p.str("tag")),
		Stage:          crm.Stage(p.str("stage")),
		Provider:       p.str("provider"),
		CompanyID:      crm.CompanyID(p.int("company_id")),
		SignedUpAfter:  p.time("signed_up_after"),
		SignedUpBefore: p.time("signed_up_before"),
		Cursor:         p.str("cursor"),
		Limit:          int(p.int("limit")),
	}
	if p.err != nil {
		return p.err
	}
	page, err := s.svc.SearchContacts(r.Context(), q)
	return respond(w, http.StatusOK, page, err)
}

func (s *server) createContact(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var in crm.NewContact
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := s.svc.CreateContact(r.Context(), a, in)
	return respond(w, http.StatusCreated, c, err)
}

func (s *server) getContact(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.svc.GetContact(r.Context(), crm.ContactID(id))
	return respond(w, http.StatusOK, c, err)
}

// deleteContact erases the contact and everything recorded about it.
func (s *server) deleteContact(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.DeleteContact(r.Context(), crm.ContactID(id)))
}

func (s *server) bulkUpdateContacts(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var b crm.BulkUpdate
	if err := readJSON(r, &b); err != nil {
		return err
	}
	results, err := s.svc.BulkUpdateContacts(r.Context(), a, b)
	return respond(w, http.StatusOK, map[string][]crm.BulkResult{"results": results}, err)
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
	return respond(w, http.StatusOK, c, err)
}

func (s *server) resetContactField(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.svc.ResetContactField(r.Context(), a, crm.ContactID(id), crm.Field(r.PathValue("field")))
	return respond(w, http.StatusOK, c, err)
}

// mergeContacts folds the contact in the body ({"drop": id}) into the one in
// the path and responds with the surviving contact.
func (s *server) mergeContacts(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	keep, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var b struct {
		Drop crm.ContactID `json:"drop"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	c, err := s.svc.MergeContacts(r.Context(), a, crm.ContactID(keep), b.Drop)
	return respond(w, http.StatusOK, c, err)
}

func (s *server) addNote(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var b struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	n, err := s.svc.AddNote(r.Context(), a, crm.ContactID(id), b.Body)
	return respond(w, http.StatusCreated, n, err)
}

func (s *server) deleteNote(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.DeleteNote(r.Context(), a, crm.NoteID(id)))
}

// enrichResult is the body of both enrich routes: the job's state after the
// request, usually "pending" ("skipped" for a free-mail contact).
type enrichResult struct {
	State crm.JobState `json:"state"`
}

// enriched answers 202 when a job was queued and 200 when there is nothing
// to do (skipped), so a client does not wait for work that will never run.
func enriched(w http.ResponseWriter, st crm.JobState, err error) error {
	code := http.StatusAccepted
	if st == crm.JobSkipped {
		code = http.StatusOK
	}
	return respond(w, code, enrichResult{st}, err)
}

func (s *server) enrichContact(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	st, err := s.svc.EnrichContact(r.Context(), a, crm.ContactID(id))
	return enriched(w, st, err)
}

func (s *server) searchCompanies(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	p := queryParams(r, "q", "domain", "cursor", "limit")
	q := crm.CompanyQuery{Text: p.str("q"), Domain: p.str("domain"), Cursor: p.str("cursor"), Limit: int(p.int("limit"))}
	if p.err != nil {
		return p.err
	}
	page, err := s.svc.SearchCompanies(r.Context(), q)
	return respond(w, http.StatusOK, page, err)
}

func (s *server) createCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	var in crm.NewCompany
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := s.svc.CreateCompany(r.Context(), a, in)
	return respond(w, http.StatusCreated, c, err)
}

func (s *server) getCompany(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.svc.GetCompany(r.Context(), crm.CompanyID(id))
	return respond(w, http.StatusOK, c, err)
}

// deleteCompany removes the company; its contacts stay, unlinked.
func (s *server) deleteCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	return noContent(w, s.svc.DeleteCompany(r.Context(), a, crm.CompanyID(id)))
}

func (s *server) updateCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var p crm.CompanyPatch
	if err := readJSON(r, &p); err != nil {
		return err
	}
	c, err := s.svc.UpdateCompany(r.Context(), a, crm.CompanyID(id), p)
	return respond(w, http.StatusOK, c, err)
}

func (s *server) resetCompanyField(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.svc.ResetCompanyField(r.Context(), a, crm.CompanyID(id), crm.Field(r.PathValue("field")))
	return respond(w, http.StatusOK, c, err)
}

func (s *server) enrichCompany(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	st, err := s.svc.EnrichCompany(r.Context(), a, crm.CompanyID(id))
	return enriched(w, st, err)
}

func (s *server) listActivity(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	p := queryParams(r, "kind", "contact_id", "company_id", "since", "before", "cursor", "limit")
	q := crm.ActivityQuery{
		Kind:      crm.ActivityKind(p.str("kind")),
		ContactID: crm.ContactID(p.int("contact_id")),
		CompanyID: crm.CompanyID(p.int("company_id")),
		Since:     p.time("since"),
		Before:    p.time("before"),
		Cursor:    p.str("cursor"),
		Limit:     int(p.int("limit")),
	}
	if p.err != nil {
		return p.err
	}
	a, err := s.svc.ListActivity(r.Context(), q)
	return respond(w, http.StatusOK, a, err)
}

func (s *server) listTags(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	tags, err := s.svc.ListTags(r.Context())
	return respond(w, http.StatusOK, tags, err)
}

// listStages answers the stage names alone, the shape this route has always
// had; listStageCounts adds each stage's contact count.
func (s *server) listStages(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	counts, err := s.svc.ListStages(r.Context())
	names := make([]crm.Stage, len(counts))
	for i, c := range counts {
		names[i] = c.Name
	}
	return respond(w, http.StatusOK, names, err)
}

func (s *server) listStageCounts(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	counts, err := s.svc.ListStages(r.Context())
	return respond(w, http.StatusOK, counts, err)
}

// statusBody adds the key store mode, which auth owns, to crm's summary.
type statusBody struct {
	crm.Status
	KeyStore string `json:"key_store"` // "builtin" or "unkey"
}

func (s *server) status(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	st, err := s.svc.Status(r.Context())
	return respond(w, http.StatusOK, statusBody{st, s.keys.Mode()}, err)
}

// respond writes v with code, or returns err for writeError.
func respond(w http.ResponseWriter, code int, v any, err error) error {
	if err != nil {
		return err
	}
	writeJSON(w, code, v)
	return nil
}

func noContent(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
