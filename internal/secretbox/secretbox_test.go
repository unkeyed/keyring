package secretbox

import (
	"bytes"
	"strings"
	"testing"
)

func mustBox(t *testing.T, cur []byte, prev ...[]byte) *Box {
	t.Helper()
	b, err := New(cur, prev...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpensOnlyInItsContext(t *testing.T) {
	b := mustBox(t, bytes.Repeat([]byte{1}, 32))
	s := b.Seal("whsec_abc", "provider_connections:clerk")
	if strings.Contains(s, "whsec_abc") {
		t.Fatalf("sealed value contains the plaintext: %s", s)
	}
	if got, err := b.Open(s, "provider_connections:clerk"); err != nil || got != "whsec_abc" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if _, err := b.Open(s, "provider_connections:workos"); err == nil {
		t.Fatal("a value moved to another row opened")
	}
	if _, err := b.Open("whsec_abc", "provider_connections:clerk"); err == nil {
		t.Fatal("plaintext opened as if sealed")
	}
}

func TestRotation(t *testing.T) {
	oldKey, newKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	sealedOld := mustBox(t, oldKey).Seal("seed", "totp:1")

	rotated := mustBox(t, newKey, oldKey)
	if got, err := rotated.Open(sealedOld, "totp:1"); err != nil || got != "seed" {
		t.Fatalf("previous key no longer opens: %q, %v", got, err)
	}
	resealed, changed, err := rotated.Reseal(sealedOld, "totp:1")
	if err != nil || !changed || !rotated.Current(resealed) {
		t.Fatalf("Reseal = %q, %v, %v", resealed, changed, err)
	}
	if _, err := mustBox(t, newKey).Open(resealed, "totp:1"); err != nil {
		t.Fatalf("resealed value needs the previous key: %v", err)
	}
	if _, err := mustBox(t, newKey).Open(sealedOld, "totp:1"); err == nil {
		t.Fatal("dropping the previous key still opened its values")
	}
	if _, changed, _ := rotated.Reseal(resealed, "totp:1"); changed {
		t.Fatal("Reseal changed a value already on the current key")
	}
	plain, changed, err := rotated.Reseal("legacy-plaintext", "totp:1")
	if err != nil || !changed {
		t.Fatalf("plaintext not sealed: %v %v", changed, err)
	}
	if got, _ := rotated.Open(plain, "totp:1"); got != "legacy-plaintext" {
		t.Fatalf("sealed plaintext opens to %q", got)
	}
}

func TestParseKey(t *testing.T) {
	for _, s := range []string{
		"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
		"0101010101010101010101010101010101010101010101010101010101010101",
	} {
		if k, err := ParseKey(s); err != nil || !bytes.Equal(k, bytes.Repeat([]byte{1}, 32)) {
			t.Errorf("ParseKey(%q) = %x, %v", s, k, err)
		}
	}
	for _, s := range []string{"", "short", "AQEBAQ=="} {
		if _, err := ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q) accepted", s)
		}
	}
}
