package signup

import (
	_ "embed"
	"encoding/json"
	"time"
)

// supabaseSQL is the trigger people paste into the Supabase SQL editor. It
// is also shown verbatim in docs/providers/supabase.md.
//
//go:embed supabase.sql
var supabaseSQL string

func init() {
	register(Provider{
		Name:        "supabase",
		Title:       "Supabase Auth",
		IssueSecret: true,
		Verify:      bearerToken,
		Parse:       parseSupabase,
		Setup: "1. In the Supabase Dashboard open your project's **SQL Editor** and click **New query**.\n" +
			"2. Paste the SQL below and click **Run**. It stores the secret in Vault and adds triggers on `auth.users` " +
			"that send new, changed and deleted users here. Only the columns it lists leave your database.\n" +
			"3. Sign up a test user. This page shows the delivery within a few seconds.\n\n" +
			"```sql\n" + supabaseSQL + "```\n\n" +
			"To send the users you already have, run once:\n\n" +
			"```sql\nselect uncrm.send_user('INSERT', u) from auth.users u where u.email is not null and not u.is_anonymous;\n```",
	})
}

var supabaseKinds = map[string]Kind{
	"INSERT": Created,
	"UPDATE": Updated,
	"DELETE": Deleted,
}

// supabaseEvent is the body supabase.sql builds, which keeps the
// Database Webhook names for type and record.
type supabaseEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Record    struct {
		ID               string     `json:"id"`
		Email            string     `json:"email"`
		EmailConfirmedAt *time.Time `json:"email_confirmed_at"`
		IsAnonymous      bool       `json:"is_anonymous"`
		DeletedAt        *time.Time `json:"deleted_at"`
		Meta             struct {
			FullName  string `json:"full_name"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Picture   string `json:"picture"`
		} `json:"raw_user_meta_data"`
	} `json:"record"`
}

func parseSupabase(d Delivery) (Signup, error) {
	var ev supabaseEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	kind, ok := supabaseKinds[ev.Type]
	if !ok || ev.Record.IsAnonymous {
		return Signup{}, ErrIgnored
	}
	r := ev.Record
	if r.DeletedAt != nil {
		kind = Deleted // a soft delete is an UPDATE that sets deleted_at
	}
	if r.Email == "" && kind != Deleted {
		return Signup{}, ErrIgnored // phone-only user
	}
	s := Signup{
		Kind:          kind,
		ExternalID:    r.ID,
		Email:         r.Email,
		EmailVerified: r.EmailConfirmedAt != nil,
		Name:          r.Meta.FullName,
		AvatarURL:     r.Meta.AvatarURL,
		OccurredAt:    ev.Timestamp,
		Raw:           d.Body,
	}
	if s.Name == "" {
		s.Name = r.Meta.Name
	}
	if s.AvatarURL == "" {
		s.AvatarURL = r.Meta.Picture
	}
	return s, nil
}
