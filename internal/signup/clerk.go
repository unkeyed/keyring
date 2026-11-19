package signup

import (
	"encoding/json"
	"time"
)

// Clerk is the exemplar adapter. Copy this file to add a provider.

func init() {
	register(Provider{
		Name:   "clerk",
		Title:  "Clerk",
		Verify: standardWebhooks,
		Parse:  parseClerk,
		Setup: `In the Clerk Dashboard open **Webhooks > Add Endpoint**.
Set the URL to ` + "`{{webhook_url}}`" + ` and subscribe to user.created, user.updated and user.deleted.
Copy the **Signing Secret** (starts with whsec_) and paste it here.`,
	})
}

type clerkEvent struct {
	Type      string    `json:"type"`
	Timestamp int64     `json:"timestamp"` // ms
	Data      clerkUser `json:"data"`
}

type clerkUser struct {
	ID                    string  `json:"id"`
	FirstName             *string `json:"first_name"`
	LastName              *string `json:"last_name"`
	Username              *string `json:"username"`
	ImageURL              string  `json:"image_url"`
	PrimaryEmailAddressID *string `json:"primary_email_address_id"`
	EmailAddresses        []struct {
		ID           string `json:"id"`
		EmailAddress string `json:"email_address"`
		Verification *struct {
			Status string `json:"status"`
		} `json:"verification"`
	} `json:"email_addresses"`
	CreatedAt int64 `json:"created_at"` // ms
}

func parseClerk(d Delivery) (Signup, error) {
	var ev clerkEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	var kind Kind
	switch ev.Type {
	case "user.created":
		kind = Created
	case "user.updated":
		kind = Updated
	case "user.deleted":
		kind = Deleted
	default:
		return Signup{}, ErrIgnored
	}
	s := Signup{
		Kind:       kind,
		ExternalID: ev.Data.ID,
		AvatarURL:  ev.Data.ImageURL,
		Name:       joinName(deref(ev.Data.FirstName), deref(ev.Data.LastName)),
		OccurredAt: time.UnixMilli(ev.Timestamp),
		Raw:        d.Body,
	}
	if s.Name == "" {
		s.Name = deref(ev.Data.Username)
	}
	for _, e := range ev.Data.EmailAddresses {
		if ev.Data.PrimaryEmailAddressID != nil && e.ID == *ev.Data.PrimaryEmailAddressID {
			s.Email = e.EmailAddress
			s.EmailVerified = e.Verification != nil && e.Verification.Status == "verified"
		}
	}
	if s.Email == "" && kind != Deleted {
		return Signup{}, ErrIgnored // phone-only or web3 user
	}
	return s, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func joinName(first, last string) string {
	switch {
	case first == "":
		return last
	case last == "":
		return first
	}
	return first + " " + last
}
