package crm

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseEmail(t *testing.T) {
	ok := map[string]string{
		"ada@stripe.com":             "ada@stripe.com",
		"  Ada.Lovelace@Stripe.COM ": "ada.lovelace@stripe.com",
		"ada+test@stripe.com":        "ada+test@stripe.com",
		"ada@mail.stripe.co.uk":      "ada@mail.stripe.co.uk",
	}
	for in, want := range ok {
		e, err := ParseEmail(in)
		if err != nil || e.String() != want {
			t.Errorf("ParseEmail(%q) = %q, %v; want %q", in, e, err, want)
		}
	}
	for _, in := range []string{"", "ada", "@stripe.com", "ada@", "ada@localhost", "ada@@stripe.com", "a@b@stripe.com",
		"ada lovelace@stripe.com", "Ada <ada@stripe.com>", "ada@stripe..com", "ada@stripe.com."} {
		if e, err := ParseEmail(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseEmail(%q) = %q, %v; want ErrInvalid", in, e, err)
		}
	}
}

func TestEmailDomainAndFreeMail(t *testing.T) {
	work, _ := ParseEmail("ada@Stripe.com")
	free, _ := ParseEmail("ada@GMAIL.com")
	if work.Domain().String() != "stripe.com" || work.Domain().IsFreeMail() {
		t.Errorf("stripe.com: domain %q free %v", work.Domain(), work.Domain().IsFreeMail())
	}
	if free.Domain().String() != "gmail.com" || !free.Domain().IsFreeMail() {
		t.Errorf("gmail.com: domain %q free %v", free.Domain(), free.Domain().IsFreeMail())
	}
}

func TestParseDomain(t *testing.T) {
	for in, want := range map[string]string{"Stripe.com": "stripe.com", " stripe.com. ": "stripe.com", "xn--bcher-kva.de": "xn--bcher-kva.de"} {
		d, err := ParseDomain(in)
		if err != nil || d.String() != want {
			t.Errorf("ParseDomain(%q) = %q, %v; want %q", in, d, err, want)
		}
	}
	for _, in := range []string{"", "stripe", ".com", "stripe.", "https://stripe.com", "stripe.com/x", "-a.com", "a b.com"} {
		if d, err := ParseDomain(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseDomain(%q) = %q, %v; want ErrInvalid", in, d, err)
		}
	}
}

func TestEmailJSONNormalizesOnDecode(t *testing.T) {
	var v struct {
		E Email  `json:"e"`
		D Domain `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"e":" ADA@Stripe.com ","d":"Stripe.com"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.E.String() != "ada@stripe.com" || v.D.String() != "stripe.com" {
		t.Fatalf("decoded %q %q", v.E, v.D)
	}
	if err := json.Unmarshal([]byte(`{"e":"nope"}`), &v); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad email decoded with err %v, want ErrInvalid", err)
	}
}

func TestParseTag(t *testing.T) {
	if tag, err := ParseTag("  VIP "); err != nil || tag != "vip" {
		t.Errorf("ParseTag = %q, %v", tag, err)
	}
	if _, err := ParseTag("  "); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank tag err = %v, want ErrInvalid", err)
	}
}
