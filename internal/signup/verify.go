package signup

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	standardWebhooksTolerance = 5 * time.Minute
	workosTolerance           = 3 * time.Minute
)

// standardWebhooks verifies the Standard Webhooks / Svix scheme used by
// Clerk, Stytch and Better Auth (our plugin). Accepts either header family:
// webhook-{id,timestamp,signature} or svix-{id,timestamp,signature}. Accepts
// secrets as "whsec_<b64>", "v1,whsec_<b64>" (Supabase) or bare base64.
//
// Rejects timestamps more than 5 minutes from d.ReceivedAt.
func standardWebhooks(secret string, d Delivery) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimPrefix(secret, "v1,"), "whsec_"))
	if err != nil || len(key) == 0 {
		return fmt.Errorf("%w: secret is not a whsec_ base64 key", ErrBadSignature)
	}
	id, ts, sigs := d.Header.Get("webhook-id"), d.Header.Get("webhook-timestamp"), d.Header.Get("webhook-signature")
	if id == "" {
		id, ts, sigs = d.Header.Get("svix-id"), d.Header.Get("svix-timestamp"), d.Header.Get("svix-signature")
	}
	if id == "" || ts == "" || sigs == "" {
		return fmt.Errorf("%w: missing webhook-id, webhook-timestamp or webhook-signature header", ErrBadSignature)
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: bad timestamp", ErrBadSignature)
	}
	if err := fresh(time.Unix(sec, 0), d.ReceivedAt, standardWebhooksTolerance); err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(d.Body)
	want := []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	for _, s := range strings.Fields(sigs) {
		version, sig, _ := strings.Cut(s, ",")
		if version == "v1" && hmac.Equal([]byte(sig), want) {
			return nil
		}
	}
	return ErrBadSignature
}

// workosSignature verifies "WorkOS-Signature: t=<ms>, v1=<hex>":
// hex(HMAC-SHA256([]byte(secret), t + "." + body)), 3 minute tolerance.
func workosSignature(secret string, d Delivery) error {
	if secret == "" {
		return ErrBadSignature
	}
	var ts, sig string
	for _, part := range strings.Split(d.Header.Get("WorkOS-Signature"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return fmt.Errorf("%w: missing or malformed WorkOS-Signature header", ErrBadSignature)
	}
	if err := fresh(time.UnixMilli(ms), d.ReceivedAt, workosTolerance); err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(d.Body)
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return ErrBadSignature
	}
	return nil
}

// bearerToken verifies "Authorization: Bearer <secret>" in constant time.
// Used by Auth0 Event Streams and the Supabase trigger, which cannot sign.
// There is no timestamp, so replays are stopped only by idempotent ingest.
func bearerToken(secret string, d Delivery) error {
	scheme, token, _ := strings.Cut(d.Header.Get("Authorization"), " ")
	if secret == "" || !strings.EqualFold(scheme, "Bearer") ||
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(secret)) != 1 {
		return ErrBadSignature
	}
	return nil
}

func fresh(sent, received time.Time, tolerance time.Duration) error {
	if skew := received.Sub(sent); skew > tolerance || skew < -tolerance {
		return fmt.Errorf("%w: timestamp is %s away from now (allowed %s)", ErrBadSignature, skew.Round(time.Second), tolerance)
	}
	return nil
}
