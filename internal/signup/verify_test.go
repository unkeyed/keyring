package signup

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// The Svix docs vector (docs.svix.com/receiving/verifying-payloads/how-manual).
const (
	svixSecret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	svixID     = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	svixTS     = "1614265330"
	svixBody   = `{"test": 2432232314}`
	svixSig    = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

var svixSent = time.Unix(1614265330, 0)

func headers(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestStandardWebhooks(t *testing.T) {
	svix := headers("svix-id", svixID, "svix-timestamp", svixTS, "svix-signature", svixSig)
	std := headers("webhook-id", svixID, "webhook-timestamp", svixTS, "webhook-signature", svixSig)
	tests := []struct {
		name    string
		secret  string
		header  http.Header
		body    string
		at      time.Time
		wantErr bool
	}{
		{"svix headers", svixSecret, svix, svixBody, svixSent, false},
		{"webhook headers", svixSecret, std, svixBody, svixSent, false},
		{"supabase secret prefix", "v1," + svixSecret, std, svixBody, svixSent, false},
		{"bare base64 secret", svixSecret[len("whsec_"):], std, svixBody, svixSent, false},
		{"one of several signatures", svixSecret, headers("webhook-id", svixID, "webhook-timestamp", svixTS,
			"webhook-signature", "v1,bm90IGl0 v2a,Zm9v "+svixSig), svixBody, svixSent, false},
		{"within tolerance", svixSecret, std, svixBody, svixSent.Add(4 * time.Minute), false},
		{"tampered body", svixSecret, std, `{"test": 2432232315}`, svixSent, true},
		{"wrong secret", "whsec_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", std, svixBody, svixSent, true},
		{"stale timestamp", svixSecret, std, svixBody, svixSent.Add(6 * time.Minute), true},
		{"future timestamp", svixSecret, std, svixBody, svixSent.Add(-6 * time.Minute), true},
		{"signature under another version", svixSecret, headers("webhook-id", svixID, "webhook-timestamp", svixTS,
			"webhook-signature", "v2"+svixSig[2:]), svixBody, svixSent, true},
		{"missing headers", svixSecret, http.Header{}, svixBody, svixSent, true},
		{"empty secret", "", std, svixBody, svixSent, true},
		{"secret not base64", "whsec_!!!", std, svixBody, svixSent, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := standardWebhooks(tt.secret, Delivery{Header: tt.header, Body: []byte(tt.body), ReceivedAt: tt.at})
			checkVerify(t, err, tt.wantErr)
		})
	}
}

func TestWorkOSSignature(t *testing.T) {
	// Expected hex from: printf '%s' '1700000000000.{"event":"user.created"}' | openssl dgst -sha256 -hmac wh_secret_test
	const (
		secret = "wh_secret_test"
		body   = `{"event":"user.created"}`
		sig    = "407a3a4f6b18670d58227187e05fe0edbc2306c86303d8d5f19743aeefe6573b"
	)
	sent := time.UnixMilli(1700000000000)
	good := headers("WorkOS-Signature", "t=1700000000000, v1="+sig)
	tests := []struct {
		name    string
		secret  string
		header  http.Header
		body    string
		at      time.Time
		wantErr bool
	}{
		{"valid", secret, good, body, sent, false},
		{"no space after comma", secret, headers("WorkOS-Signature", "t=1700000000000,v1="+sig), body, sent, false},
		{"within tolerance", secret, good, body, sent.Add(2 * time.Minute), false},
		{"tampered body", secret, good, `{"event":"user.deleted"}`, sent, true},
		{"wrong secret", "other", good, body, sent, true},
		{"stale timestamp", secret, good, body, sent.Add(4 * time.Minute), true},
		{"timestamp swapped", secret, headers("WorkOS-Signature", "t=1700000001000, v1="+sig), body, sent, true},
		{"missing header", secret, http.Header{}, body, sent, true},
		{"empty secret", "", good, body, sent, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := workosSignature(tt.secret, Delivery{Header: tt.header, Body: []byte(tt.body), ReceivedAt: tt.at})
			checkVerify(t, err, tt.wantErr)
		})
	}
}

func TestBearerToken(t *testing.T) {
	const secret = "whsec_c2VjcmV0"
	tests := []struct {
		name    string
		secret  string
		auth    string
		wantErr bool
	}{
		{"valid", secret, "Bearer " + secret, false},
		{"lowercase scheme", secret, "bearer " + secret, false},
		{"wrong token", secret, "Bearer whsec_b3RoZXI=", true},
		{"prefix of token", secret, "Bearer " + secret[:5], true},
		{"basic scheme", secret, "Basic " + secret, true},
		{"token without scheme", secret, secret, true},
		{"missing header", secret, "", true},
		{"empty secret", "", "Bearer ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.auth != "" {
				h.Set("Authorization", tt.auth)
			}
			checkVerify(t, bearerToken(tt.secret, Delivery{Header: h, ReceivedAt: time.Now()}), tt.wantErr)
		})
	}
}

func checkVerify(t *testing.T, err error, wantErr bool) {
	t.Helper()
	if wantErr && !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v, want ErrBadSignature", err)
	}
	if !wantErr && err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}
