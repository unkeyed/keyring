package signup

import "errors"

func init() {
	register(Provider{
		Name:   "stytch",
		Title:  "Stytch",
		Verify: standardWebhooks,
		Parse:  parseStytch,
		Setup: "In the Stytch Dashboard open **Webhooks**, add `{{webhook_url}}`, and select the user (or B2B member)\n" +
			"create, update and delete events. Copy the signing secret (starts with whsec_) and paste it here.",
	})
}

// parseStytch maps {action, object_type, id, timestamp, user|member}.
//
//	action CREATE|UPDATE|DELETE -> Kind; object_type not user|member -> ErrIgnored
//	consumer: user.user_id, user.emails[0].email, .verified,
//	          user.name.first_name + last_name, user.providers[0].profile_picture_url
//	b2b:      member.member_id, member.email_address, member.email_address_verified, member.name
//	timestamp -> OccurredAt
func parseStytch(d Delivery) (Signup, error) {
	return Signup{}, errors.New("not implemented")
}
