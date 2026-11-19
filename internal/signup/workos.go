package signup

import (
	"encoding/json"
	"time"
)

func init() {
	register(Provider{
		Name:   "workos",
		Title:  "WorkOS AuthKit",
		Verify: workosSignature,
		Parse:  parseWorkOS,
		Setup: `1. In the WorkOS Dashboard, switch to the environment your app uses (Staging or Production) and open **Webhooks > Create Webhook**.
2. Set **Endpoint URL** to ` + "`{{webhook_url}}`" + `.
3. Select the events ` + "`user.created`, `user.updated` and `user.deleted`" + ` and click **Create**.
4. On the webhook's page, reveal the **Secret**, copy it and paste it here.`,
	})
}

var workosKinds = map[string]Kind{
	"user.created": Created,
	"user.updated": Updated,
	"user.deleted": Deleted,
}

type workosEvent struct {
	Event     string    `json:"event"`
	CreatedAt time.Time `json:"created_at"`
	Data      struct {
		ID                string `json:"id"`
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		FirstName         string `json:"first_name"`
		LastName          string `json:"last_name"`
		ProfilePictureURL string `json:"profile_picture_url"`
	} `json:"data"`
}

// parseWorkOS maps {event, id, data, created_at}. organization_membership.*
// is ignored in v1 because it carries no org name.
func parseWorkOS(d Delivery) (Signup, error) {
	var ev workosEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	kind, ok := workosKinds[ev.Event]
	if !ok {
		return Signup{}, ErrIgnored
	}
	u := ev.Data
	s := Signup{
		Kind:          kind,
		ExternalID:    u.ID,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		Name:          u.Name,
		AvatarURL:     u.ProfilePictureURL,
		OccurredAt:    ev.CreatedAt,
		Raw:           d.Body,
	}
	if s.Name == "" {
		s.Name = joinName(u.FirstName, u.LastName)
	}
	return s, nil
}
