package signup

import (
	"encoding/json"
	"time"
)

func init() {
	register(Provider{
		Name:   "stytch",
		Title:  "Stytch",
		Verify: standardWebhooks,
		Parse:  parseStytch,
		Setup: "1. In the Stytch Dashboard, pick the project and environment (Test or Live) your app uses, then open **Webhooks** and click **Add Endpoint**.\n" +
			"2. Set **Endpoint URL** to `{{webhook_url}}`.\n" +
			"3. Under **Subscribe to events**, select the user create, update and delete events: " +
			"`direct.user.create`, `direct.user.update`, `direct.user.delete` and the matching `dashboard.user.*` events. " +
			"For a B2B project select the `direct.member.*`, `dashboard.member.*` and `scim.member.*` events instead.\n" +
			"4. Click **Create**, open the endpoint, reveal the **Signing Secret** (starts with whsec_), copy it and paste it here.",
	})
}

var stytchKinds = map[string]Kind{
	"CREATE": Created,
	"UPDATE": Updated,
	"DELETE": Deleted,
}

type stytchEvent struct {
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	User       struct {
		Emails []struct {
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
		} `json:"emails"`
		Name struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"name"`
		Providers []struct {
			ProfilePictureURL string `json:"profile_picture_url"`
		} `json:"providers"`
	} `json:"user"`
	Member struct {
		EmailAddress         string `json:"email_address"`
		EmailAddressVerified bool   `json:"email_address_verified"`
		Name                 string `json:"name"`
	} `json:"member"`
}

// parseStytch maps Consumer user and B2B member events. DELETE carries only
// the top-level id, which equals user.user_id or member.member_id.
func parseStytch(d Delivery) (Signup, error) {
	var ev stytchEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	kind, ok := stytchKinds[ev.Action]
	if !ok {
		return Signup{}, ErrIgnored
	}
	s := Signup{Kind: kind, ExternalID: ev.ID, OccurredAt: ev.Timestamp, Raw: d.Body}
	switch ev.ObjectType {
	case "user":
		u := ev.User
		if len(u.Emails) > 0 {
			s.Email, s.EmailVerified = u.Emails[0].Email, u.Emails[0].Verified
		}
		s.Name = joinName(u.Name.FirstName, u.Name.LastName)
		for _, p := range u.Providers {
			if p.ProfilePictureURL != "" {
				s.AvatarURL = p.ProfilePictureURL
				break
			}
		}
	case "member":
		m := ev.Member
		s.Email, s.EmailVerified, s.Name = m.EmailAddress, m.EmailAddressVerified, m.Name
	default:
		return Signup{}, ErrIgnored
	}
	if s.Email == "" && kind != Deleted {
		return Signup{}, ErrIgnored // phone-only user
	}
	return s, nil
}
