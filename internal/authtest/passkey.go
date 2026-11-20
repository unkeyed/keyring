// Package authtest holds test doubles for sign-in: a software passkey
// authenticator and a fake OIDC issuer. Tests and the e2e suite use them to
// drive the real WebAuthn and OAuth code paths without a browser.
package authtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

var b64 = base64.RawURLEncoding

// Passkey is a software authenticator holding one discoverable ES256
// credential. It always reports user presence and user verification.
type Passkey struct {
	Origin     string
	RPID       string
	ID         []byte
	UserHandle []byte
	Key        *ecdsa.PrivateKey
	SignCount  uint32
}

type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		ExcludeCredentials []struct {
			ID string `json:"id"`
		} `json:"excludeCredentials"`
	} `json:"publicKey"`
}

type requestOptions struct {
	PublicKey struct {
		Challenge        string `json:"challenge"`
		RPID             string `json:"rpId"`
		AllowCredentials []struct {
			ID string `json:"id"`
		} `json:"allowCredentials"`
	} `json:"publicKey"`
}

func NewPasskey(origin string, options []byte) (*Passkey, json.RawMessage, error) {
	var o creationOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, nil, err
	}
	handle, err := b64.DecodeString(o.PublicKey.User.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("user.id: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	p := &Passkey{Origin: origin, RPID: o.PublicKey.RP.ID, ID: make([]byte, 16), UserHandle: handle, Key: key}
	_, _ = rand.Read(p.ID)

	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": o.PublicKey.Challenge, "origin": origin, "crossOrigin": false})
	ecdh, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, nil, err
	}
	point := ecdh.Bytes() // 0x04 || X || Y
	coseKey := cborMap(
		cborPair{cborInt(1), cborInt(2)},
		cborPair{cborInt(3), cborInt(-7)},
		cborPair{cborInt(-1), cborInt(1)},
		cborPair{cborInt(-2), cborBytes(point[1:33])},
		cborPair{cborInt(-3), cborBytes(point[33:65])},
	)
	authData := p.authData(0x01 | 0x04 | 0x40)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(p.ID)))
	authData = append(authData, p.ID...)
	authData = append(authData, coseKey...)
	att := cborMap(
		cborPair{cborText("fmt"), cborText("none")},
		cborPair{cborText("attStmt"), cborMap()},
		cborPair{cborText("authData"), cborBytes(authData)},
	)
	cred, err := json.Marshal(map[string]any{
		"id": b64.EncodeToString(p.ID), "rawId": b64.EncodeToString(p.ID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"attestationObject": b64.EncodeToString(att),
			"transports":        []string{"internal"},
		},
	})
	return p, cred, err
}

func (p *Passkey) authData(flags byte) []byte {
	rp := sha256.Sum256([]byte(p.RPID))
	out := append(rp[:], flags)
	return binary.BigEndian.AppendUint32(out, p.SignCount)
}

func (p *Passkey) Sign(options []byte) (json.RawMessage, error) {
	var o requestOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, err
	}
	if allow := o.PublicKey.AllowCredentials; len(allow) > 0 && !slices.ContainsFunc(allow, func(c struct {
		ID string `json:"id"`
	}) bool {
		return c.ID == b64.EncodeToString(p.ID)
	}) {
		return nil, errors.New("authtest: this passkey is not in allowCredentials")
	}
	if o.PublicKey.RPID != "" && o.PublicKey.RPID != p.RPID {
		return nil, fmt.Errorf("authtest: rpId %q, this passkey is for %q", o.PublicKey.RPID, p.RPID)
	}
	p.SignCount++
	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": o.PublicKey.Challenge, "origin": p.Origin, "crossOrigin": false})
	authData := p.authData(0x01 | 0x04)
	cdHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(slices.Clone(authData), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, p.Key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(p.ID), "rawId": b64.EncodeToString(p.ID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"authenticatorData": b64.EncodeToString(authData),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(p.UserHandle),
		},
	})
}

// passkeyFile is a Passkey as JSON, so the e2e suite can keep its passkey
// between runs against the same instance.
type passkeyFile struct {
	Origin, RPID   string
	ID, UserHandle []byte
	Key            []byte
	SignCount      uint32
}

func (p *Passkey) MarshalJSON() ([]byte, error) {
	key, err := x509.MarshalECPrivateKey(p.Key)
	if err != nil {
		return nil, err
	}
	return json.Marshal(passkeyFile{p.Origin, p.RPID, p.ID, p.UserHandle, key, p.SignCount})
}

func (p *Passkey) UnmarshalJSON(data []byte) error {
	var f passkeyFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	key, err := x509.ParseECPrivateKey(f.Key)
	if err != nil {
		return err
	}
	*p = Passkey{Origin: f.Origin, RPID: f.RPID, ID: f.ID, UserHandle: f.UserHandle, Key: key, SignCount: f.SignCount}
	return nil
}

type cborPair struct{ k, v []byte }

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 1<<8:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	}
}

func cborInt(n int) []byte {
	if n < 0 {
		return cborHead(1, uint64(-1-n))
	}
	return cborHead(0, uint64(n))
}

func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }

func cborMap(pairs ...cborPair) []byte {
	out := cborHead(5, uint64(len(pairs)))
	for _, p := range pairs {
		out = append(append(out, p.k...), p.v...)
	}
	return out
}
