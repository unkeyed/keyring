package signup

import "errors"

func init() {
	register(Provider{
		Name:        "supabase",
		Title:       "Supabase Auth",
		IssueSecret: true,
		Verify:      bearerToken,
		Parse:       parseSupabase,
		Setup: "Run this SQL in the Supabase SQL editor. It adds a trigger on `auth.users` that posts an allowlisted\n" +
			"set of columns to `{{webhook_url}}` with `Authorization: Bearer {{secret}}`.\n\n" +
			"```sql\n-- TODO: trigger function using net.http_post with an explicit column allowlist\n```",
	})
}

// parseSupabase maps a Database Webhook body {type, record, old_record}.
//
//	type INSERT|UPDATE|DELETE -> Created|Updated|Deleted
//	record.id (old_record.id on DELETE) -> ExternalID
//	record.email -> Email; email_confirmed_at != null -> EmailVerified
//	raw_user_meta_data.full_name, else .name -> Name
//	raw_user_meta_data.avatar_url, else .picture -> AvatarURL
//	record.updated_at (created_at on INSERT) -> OccurredAt
//	is_anonymous or empty email -> ErrIgnored
func parseSupabase(d Delivery) (Signup, error) {
	return Signup{}, errors.New("not implemented")
}
