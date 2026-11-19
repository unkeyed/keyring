package signup

import "errors"

func init() {
	register(Provider{
		Name:        "better-auth",
		Title:       "Better Auth",
		IssueSecret: true,
		Verify:      standardWebhooks,
		Parse:       parseBetterAuth,
		Setup: "Install `@uncrm/better-auth` and add `uncrm({ url: \"{{webhook_url}}\", secret: \"{{secret}}\" })` to your `plugins`.\n" +
			"The plugin signs user create, update and delete hooks with Standard Webhooks.",
	})
}

// parseBetterAuth maps our own plugin's body {type, timestamp, data}.
//
//	type user.created|updated|deleted -> Kind
//	data.id, data.email, data.emailVerified, data.name, data.image
//	timestamp (ISO 8601) -> OccurredAt
func parseBetterAuth(d Delivery) (Signup, error) {
	return Signup{}, errors.New("not implemented")
}
