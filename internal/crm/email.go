package crm

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// Email is a normalized address: trimmed, lowercased, exactly one "@" with a
// non-empty local part and domain. Its field is unexported, so ParseEmail,
// UnmarshalText (JSON bodies) and Scan (database rows) are the only ways to
// build one, and two Emails name the same mailbox exactly when they are
// equal. That equality is the whole identity-resolution rule across
// providers.
//
// Plus-addressing and Gmail dots are kept: ada+test@x.com is a different
// contact from ada@x.com, because teams test sign-ups that way on purpose.
//
// The zero Email means "no email". It marshals as "".
type Email struct{ addr string }

func ParseEmail(s string) (Email, error) {
	addr := strings.ToLower(strings.TrimSpace(s))
	local, host, ok := strings.Cut(addr, "@")
	if !ok || local == "" || strings.ContainsAny(local, " \t\r\n<>,;\"") || len(addr) > 320 {
		return Email{}, fmt.Errorf("%w: email %q", ErrInvalid, s)
	}
	if _, err := ParseDomain(host); err != nil || strings.HasSuffix(host, ".") {
		return Email{}, fmt.Errorf("%w: email %q", ErrInvalid, s)
	}
	return Email{addr: addr}, nil
}

func (e Email) String() string { return e.addr }
func (e Email) IsZero() bool   { return e.addr == "" }

// Domain is the part after "@", already normalized by ParseEmail.
func (e Email) Domain() Domain {
	_, host, _ := strings.Cut(e.addr, "@")
	return Domain{host: host}
}

func (e Email) MarshalText() ([]byte, error) { return []byte(e.addr), nil }

// UnmarshalText parses, so request bodies normalize on decode. An empty
// string decodes to the zero Email.
func (e *Email) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*e = Email{}
		return nil
	}
	v, err := ParseEmail(string(b))
	if err != nil {
		return err
	}
	*e = v
	return nil
}

func (e Email) Value() (driver.Value, error) { return e.addr, nil }

// Scan parses a text column, so even a database row cannot produce an
// unnormalized Email. NULL scans to the zero Email.
func (e *Email) Scan(src any) error {
	if src == nil {
		*e = Email{}
		return nil
	}
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("crm.Email: cannot scan %T", src)
	}
	return e.UnmarshalText([]byte(s))
}

// Domain is a normalized host name: trimmed, lowercased, no trailing dot,
// at least one inner dot. Like Email, it can only be built by parsing.
// The zero Domain means "no domain"; in a ContactPatch it detaches the
// contact from its company.
type Domain struct{ host string }

func ParseDomain(s string) (Domain, error) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	labels := strings.Split(host, ".")
	if len(labels) < 2 || len(host) > 253 {
		return Domain{}, fmt.Errorf("%w: domain %q", ErrInvalid, s)
	}
	for _, l := range labels {
		if !validLabel(l) {
			return Domain{}, fmt.Errorf("%w: domain %q", ErrInvalid, s)
		}
	}
	return Domain{host: host}, nil
}

// validLabel allows ASCII letters, digits and inner hyphens, plus any
// non-ASCII rune so internationalized domains pass unconverted.
func validLabel(l string) bool {
	if l == "" || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, r := range l {
		if r < 0x80 && r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (d Domain) String() string { return d.host }
func (d Domain) IsZero() bool   { return d.host == "" }

// IsFreeMail reports whether the domain is a consumer mailbox provider
// (gmail.com, outlook.com, ...), read from the embedded freemail.txt.
// A free-mail domain never becomes a company and its addresses never get
// person enrichment.
func (d Domain) IsFreeMail() bool {
	return freemail()[d.host]
}

func (d Domain) MarshalText() ([]byte, error) { return []byte(d.host), nil }

// UnmarshalText parses. An empty string decodes to the zero Domain.
func (d *Domain) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Domain{}
		return nil
	}
	v, err := ParseDomain(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

func (d Domain) Value() (driver.Value, error) { return d.host, nil }

func (d *Domain) Scan(src any) error {
	if src == nil {
		*d = Domain{}
		return nil
	}
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("crm.Domain: cannot scan %T", src)
	}
	return d.UnmarshalText([]byte(s))
}
