package signup

// The three signature schemes the v1 providers use. A new provider almost
// always reuses one of these by name in its register call.

// standardWebhooks verifies the Standard Webhooks / Svix scheme used by
// Clerk, Stytch, Better Auth (our plugin) and Supabase (when the trigger
// signs). Accepts either header family: webhook-{id,timestamp,signature} or
// svix-{id,timestamp,signature}. Accepts secrets as "whsec_<b64>",
// "v1,whsec_<b64>" (Supabase) or bare base64.
//
// Rejects timestamps more than 5 minutes from d.ReceivedAt.
// Stdlib only: crypto/hmac, crypto/sha256, encoding/base64, crypto/subtle.
func standardWebhooks(secret string, d Delivery) error {
	// TODO:
	//   id, ts, sigs := header("webhook-id"|"svix-id"), ...
	//   check |ReceivedAt - ts| <= 5m
	//   key := base64decode(trimPrefix(trimPrefix(secret, "v1,"), "whsec_"))
	//   want := base64(HMAC-SHA256(key, id + "." + ts + "." + body))
	//   for each space-separated "v1,<sig>": subtle.ConstantTimeCompare
	return ErrBadSignature
}

// workosSignature verifies "WorkOS-Signature: t=<ms>, v1=<hex>":
// hex(HMAC-SHA256([]byte(secret), t + "." + body)), 3 minute tolerance.
func workosSignature(secret string, d Delivery) error {
	return ErrBadSignature
}

// bearerToken verifies "Authorization: Bearer <secret>" in constant time.
// Used by Auth0 Event Streams and the Supabase trigger, which cannot sign.
func bearerToken(secret string, d Delivery) error {
	return ErrBadSignature
}
