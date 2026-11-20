package signup

import (
	"encoding/json"
	"strings"
	"time"
)

func init() {
	register(Provider{
		Name:        "auth0",
		Title:       "Auth0",
		IssueSecret: true,
		Verify:      bearerToken,
		Parse:       parseAuth0,
		Setup: "1. In the Auth0 Dashboard open **Event Streams** and click **Create Event Stream**, then choose **Webhook**.\n" +
			"2. Set **Webhook Endpoint** to `{{webhook_url}}`.\n" +
			"3. Set **Authorization Method** to **Bearer** and paste this token: `{{secret}}`\n" +
			"4. Under events, select `user.created`, `user.updated` and `user.deleted`, then save and enable the stream.\n\n" +
			"No **Event Streams** in your Dashboard? Use the [Post-Login Action](https://github.com/unkeyed/keyring/blob/main/docs/providers/auth0.md#setup-with-a-post-login-action-fallback) " +
			"instead, with the same URL and token.",
	})
}

var auth0Kinds = map[string]Kind{
	"user.created": Created,
	"user.updated": Updated,
	"user.deleted": Deleted,
}

// auth0Event is a CloudEvents body. The Post-Login Action fallback in
// docs/providers/auth0.md sends the same shape.
type auth0Event struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data struct {
		Object struct {
			UserID        string `json:"user_id"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			Name          string `json:"name"`
			GivenName     string `json:"given_name"`
			FamilyName    string `json:"family_name"`
			Picture       string `json:"picture"`
		} `json:"object"`
	} `json:"data"`
}

func parseAuth0(d Delivery) (Signup, error) {
	var ev auth0Event
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	// The events catalog documents "com.auth0.user.created" but test
	// deliveries carry "user.created"; accept both.
	kind, ok := auth0Kinds[strings.TrimPrefix(ev.Type, "com.auth0.")]
	if !ok {
		return Signup{}, ErrIgnored
	}
	u := ev.Data.Object
	if u.Email == "" && kind != Deleted {
		return Signup{}, ErrIgnored // phone-only passwordless user
	}
	s := Signup{
		Kind:          kind,
		ExternalID:    u.UserID,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		Name:          u.Name,
		AvatarURL:     u.Picture,
		OccurredAt:    ev.Time,
		Raw:           d.Body,
	}
	// Database users get their email as name unless a name was set.
	if s.Name == u.Email {
		s.Name = joinName(u.GivenName, u.FamilyName)
	}
	return s, nil
}
