package signup

import "errors"

func init() {
	register(Provider{
		Name:   "workos",
		Title:  "WorkOS AuthKit",
		Verify: workosSignature,
		Parse:  parseWorkOS,
		Setup: `In the WorkOS Dashboard open **Webhooks > Create Webhook**.
Set the URL to ` + "`{{webhook_url}}`" + ` and select user.created, user.updated and user.deleted.
Copy the webhook **Secret** and paste it here.`,
	})
}

// parseWorkOS maps {event, id, data, created_at}.
//
//	event user.created|updated|deleted -> Created|Updated|Deleted, else ErrIgnored
//	data.id -> ExternalID, data.email -> Email, data.email_verified -> EmailVerified
//	data.name, else first_name + last_name -> Name
//	data.profile_picture_url -> AvatarURL
//	created_at (event, ISO 8601) -> OccurredAt
//
// organization_membership.* is ErrIgnored in v1: it carries no org name.
func parseWorkOS(d Delivery) (Signup, error) {
	return Signup{}, errors.New("not implemented")
}
