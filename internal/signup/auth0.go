package signup

import "errors"

func init() {
	register(Provider{
		Name:        "auth0",
		Title:       "Auth0",
		IssueSecret: true,
		Verify:      bearerToken,
		Parse:       parseAuth0,
		Setup: "In the Auth0 Dashboard open **Event Streams > Create Event Stream > Webhook**.\n" +
			"Set the endpoint to `{{webhook_url}}`, authorization to **Bearer** with token `{{secret}}`,\n" +
			"and subscribe to user.created, user.updated and user.deleted.",
	})
}

// parseAuth0 maps a CloudEvents body {id, type, time, data.object}.
//
//	type "user.created" or "com.auth0.user.created" (both seen in docs) -> Created, same for updated/deleted
//	data.object.user_id -> ExternalID, .email, .email_verified, .picture
//	.name unless it equals .email, else given_name + family_name -> Name
//	time -> OccurredAt
func parseAuth0(d Delivery) (Signup, error) {
	return Signup{}, errors.New("not implemented")
}
