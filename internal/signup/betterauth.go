package signup

import (
	"encoding/json"
	"time"
)

func init() {
	register(Provider{
		Name:        "better-auth",
		Title:       "Better Auth",
		IssueSecret: true,
		Verify:      standardWebhooks,
		Parse:       parseBetterAuth,
		Setup: "1. Install the plugin in your app: `npm install @uncrm/better-auth`.\n" +
			"2. Add it to `plugins` in your `betterAuth({ ... })` config:\n\n" +
			"```ts\n" +
			"import { uncrm } from \"@uncrm/better-auth\";\n\n" +
			"export const auth = betterAuth({\n" +
			"  // ...your existing options\n" +
			"  plugins: [uncrm({ url: \"{{webhook_url}}\", secret: process.env.UNCRM_SECRET! })],\n" +
			"});\n" +
			"```\n\n" +
			"3. Set `UNCRM_SECRET={{secret}}` in your app's environment and redeploy.\n\n" +
			"Every user create, update and delete is then sent here, signed with Standard Webhooks.",
	})
}

var betterAuthKinds = map[string]Kind{
	"user.created": Created,
	"user.updated": Updated,
	"user.deleted": Deleted,
}

// betterAuthEvent is the body integrations/better-auth sends, so its shape
// is ours, not Better Auth's.
type betterAuthEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"emailVerified"`
		Name          string `json:"name"`
		Image         string `json:"image"`
	} `json:"data"`
}

func parseBetterAuth(d Delivery) (Signup, error) {
	var ev betterAuthEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		return Signup{}, err
	}
	kind, ok := betterAuthKinds[ev.Type]
	if !ok {
		return Signup{}, ErrIgnored
	}
	return Signup{
		Kind:          kind,
		ExternalID:    ev.Data.ID,
		Email:         ev.Data.Email,
		EmailVerified: ev.Data.EmailVerified,
		Name:          ev.Data.Name,
		AvatarURL:     ev.Data.Image,
		OccurredAt:    ev.Timestamp,
		Raw:           d.Body,
	}, nil
}
