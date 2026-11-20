package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uncrm/uncrm/internal/crm"
)

// Passkeys are discoverable WebAuthn credentials with user verification
// required, so the authenticator itself checked a PIN or biometric and a
// passkey sign-in is complete on its own. Attestation is not requested: the
// relying party trusts any authenticator that verifies its user.
//
// Every ceremony is a begin request that stores a ceremony challenge and a
// finish request that takes it. The purpose says what the finish may do.

// Passkey is a registered passkey as the account page lists it.
type Passkey struct {
	ID         string     `json:"id"` // base64url credential ID
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type purpose string

const (
	purposeLogin        purpose = "login"         // discoverable sign-in
	purposeSecondFactor purpose = "second_factor" // after a password, for UserID
	purposeReauth       purpose = "reauth"        // re-verify UserID's session
	purposeAdd          purpose = "add"           // register another passkey for UserID
	purposeSignup       purpose = "signup"        // register the first passkey of a new user
)

type ceremony struct {
	Purpose purpose              `json:"purpose"`
	Session webauthn.SessionData `json:"session"`
	UserID  int64                `json:"user_id,omitempty"`
	Signup  *newUser             `json:"signup,omitempty"`
}

// newUser is a person joining through a setup link or an invite.
type newUser struct {
	Via       string `json:"via"` // "setup" or "invite"
	TokenHash []byte `json:"token_hash"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Handle    []byte `json:"handle"`
}

// passkeyUser is a user as go-webauthn sees one.
type passkeyUser struct {
	id          int64
	handle      []byte
	email, name string
	creds       []webauthn.Credential
}

func (p *passkeyUser) WebAuthnID() []byte                         { return p.handle }
func (p *passkeyUser) WebAuthnName() string                       { return p.email }
func (p *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }
func (p *passkeyUser) WebAuthnDisplayName() string {
	if p.name != "" {
		return p.name
	}
	return p.email
}

var errNoPasskeyUser = fmt.Errorf("%w: this passkey is not registered on this uncrm", crm.ErrInvalid)

// loadPasskeyUser finds a user by id, or by WebAuthn handle when id is 0.
func loadPasskeyUser(ctx context.Context, db dbtx, id int64, handle []byte) (*passkeyUser, error) {
	p := &passkeyUser{}
	err := db.QueryRow(ctx, `SELECT id, webauthn_handle, email, name FROM users WHERE id = $1 OR ($1 = 0 AND webauthn_handle = $2)`,
		id, handle).Scan(&p.id, &p.handle, &p.email, &p.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNoPasskeyUser
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT credential FROM passkeys WHERE user_id = $1`, p.id)
	if err != nil {
		return nil, err
	}
	p.creds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (webauthn.Credential, error) {
		var c webauthn.Credential
		var raw []byte
		if err := row.Scan(&raw); err != nil {
			return c, err
		}
		return c, json.Unmarshal(raw, &c)
	})
	return p, err
}

func passkeyErr(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return fmt.Errorf("%w: the passkey was not accepted: %s", crm.ErrInvalid, pe.Details)
	}
	return err
}

var userVerified = webauthn.WithUserVerification(protocol.VerificationRequired)

// beginAssertion starts a sign-in ceremony: discoverable when user is nil,
// limited to user's passkeys otherwise.
func (u *Users) beginAssertion(ctx context.Context, c ceremony, user *passkeyUser) (string, *protocol.CredentialAssertion, error) {
	var opts *protocol.CredentialAssertion
	var sd *webauthn.SessionData
	var err error
	if user == nil {
		opts, sd, err = u.webauthn.BeginDiscoverableLogin(userVerified)
	} else {
		opts, sd, err = u.webauthn.BeginLogin(user, userVerified)
	}
	if err != nil {
		return "", nil, passkeyErr(err)
	}
	c.Session = *sd
	handle, err := putChallenge(ctx, u.db, kindPasskey, c)
	return handle, opts, err
}

// finishAssertion verifies a sign-in ceremony begun for want and returns
// the user it proved, after storing the passkey's new sign count.
func (u *Users) finishAssertion(ctx context.Context, want purpose, handle string, credential json.RawMessage) (ceremony, *passkeyUser, error) {
	var c ceremony
	if err := takeChallenge(ctx, u.db, kindPasskey, handle, &c); err != nil {
		return c, nil, err
	}
	if c.Purpose != want {
		return c, nil, errChallenge
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return c, nil, passkeyErr(err)
	}
	var user *passkeyUser
	var cred *webauthn.Credential
	if c.UserID == 0 {
		var wu webauthn.User
		wu, cred, err = u.webauthn.ValidatePasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
			return loadPasskeyUser(ctx, u.db, 0, userHandle)
		}, c.Session, parsed)
		if err == nil {
			user = wu.(*passkeyUser)
		}
	} else {
		if user, err = loadPasskeyUser(ctx, u.db, c.UserID, nil); err != nil {
			return c, nil, err
		}
		cred, err = u.webauthn.ValidateLogin(user, c.Session, parsed)
	}
	if err != nil {
		return c, nil, passkeyErr(err)
	}
	if cred.Authenticator.CloneWarning {
		return c, nil, fmt.Errorf("%w: this passkey's signature counter went backwards, which suggests a copy of it exists; remove it and register a new one", crm.ErrInvalid)
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return c, nil, err
	}
	if _, err := u.db.Exec(ctx, `UPDATE passkeys SET credential = $2, last_used_at = now() WHERE id = $1`, cred.ID, raw); err != nil {
		return c, nil, err
	}
	return c, user, nil
}

// BeginPasskeyLogin starts a usernameless sign-in.
func (u *Users) BeginPasskeyLogin(ctx context.Context) (string, *protocol.CredentialAssertion, error) {
	return u.beginAssertion(ctx, ceremony{Purpose: purposeLogin}, nil)
}

// FinishPasskeyLogin signs in whoever owns the passkey. No second factor
// follows: the authenticator verified its user.
func (u *Users) FinishPasskeyLogin(ctx context.Context, handle string, credential json.RawMessage, c Client) (SignIn, error) {
	_, user, err := u.finishAssertion(ctx, purposeLogin, handle, credential)
	if err != nil {
		return SignIn{}, err
	}
	return u.signIn(ctx, user.id, MethodPasskey, c)
}

// BeginSecondFactorPasskey starts a passkey check for the pending login.
func (u *Users) BeginSecondFactorPasskey(ctx context.Context, pending string) (string, *protocol.CredentialAssertion, error) {
	var p pendingLogin
	if err := peekChallenge(ctx, u.db, kindSecondFactor, pending, &p); err != nil {
		return "", nil, err
	}
	user, err := loadPasskeyUser(ctx, u.db, p.UserID, nil)
	if err != nil {
		return "", nil, err
	}
	return u.beginAssertion(ctx, ceremony{Purpose: purposeSecondFactor, UserID: p.UserID}, user)
}

// FinishSecondFactorPasskey completes the pending login with a passkey.
func (u *Users) FinishSecondFactorPasskey(ctx context.Context, pending, handle string, credential json.RawMessage, c Client) (SignIn, error) {
	var p pendingLogin
	if err := peekChallenge(ctx, u.db, kindSecondFactor, pending, &p); err != nil {
		return SignIn{}, err
	}
	cer, _, err := u.finishAssertion(ctx, purposeSecondFactor, handle, credential)
	if err != nil {
		return SignIn{}, err
	}
	if cer.UserID != p.UserID {
		return SignIn{}, errChallenge
	}
	return u.completePending(ctx, pending, p, MethodPasskey, c)
}

// BeginReauthPasskey starts re-verifying the current session.
func (u *Users) BeginReauthPasskey(ctx context.Context, cur Current) (string, *protocol.CredentialAssertion, error) {
	user, err := loadPasskeyUser(ctx, u.db, cur.UserID, nil)
	if err != nil {
		return "", nil, err
	}
	return u.beginAssertion(ctx, ceremony{Purpose: purposeReauth, UserID: cur.UserID}, user)
}

func (u *Users) FinishReauthPasskey(ctx context.Context, cur Current, handle string, credential json.RawMessage) error {
	cer, _, err := u.finishAssertion(ctx, purposeReauth, handle, credential)
	if err != nil {
		return err
	}
	if cer.UserID != cur.UserID {
		return errChallenge
	}
	return u.reauthenticated(ctx, cur)
}

func (u *Users) beginRegistration(ctx context.Context, c ceremony, user *passkeyUser) (string, *protocol.CredentialCreation, error) {
	required := true
	opts, sd, err := u.webauthn.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: &required,
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(webauthn.Credentials(user.creds).CredentialDescriptors()),
	)
	if err != nil {
		return "", nil, passkeyErr(err)
	}
	c.Session = *sd
	handle, err := putChallenge(ctx, u.db, kindPasskey, c)
	return handle, opts, err
}

func (u *Users) finishRegistration(ctx context.Context, want purpose, handle string, credential json.RawMessage) (ceremony, *webauthn.Credential, error) {
	var c ceremony
	if err := takeChallenge(ctx, u.db, kindPasskey, handle, &c); err != nil {
		return c, nil, err
	}
	if c.Purpose != want {
		return c, nil, errChallenge
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return c, nil, passkeyErr(err)
	}
	cred, err := u.webauthn.CreateCredential(&passkeyUser{handle: c.Session.UserID}, c.Session, parsed)
	if err != nil {
		return c, nil, passkeyErr(err)
	}
	return c, cred, nil
}

func insertPasskey(ctx context.Context, tx pgx.Tx, userID int64, name string, cred *webauthn.Credential) (Passkey, error) {
	raw, err := json.Marshal(cred)
	if err != nil {
		return Passkey{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	p := Passkey{ID: base64.RawURLEncoding.EncodeToString(cred.ID), Name: name}
	err = tx.QueryRow(ctx, `INSERT INTO passkeys (id, user_id, name, credential) VALUES ($1, $2, $3, $4) RETURNING created_at`,
		cred.ID, userID, name, raw).Scan(&p.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Passkey{}, fmt.Errorf("%w: this passkey is already registered", crm.ErrConflict)
	}
	return p, err
}

// BeginAddPasskey starts registering another passkey for the current user.
func (u *Users) BeginAddPasskey(ctx context.Context, cur Current) (string, *protocol.CredentialCreation, error) {
	if !cur.Recent {
		return "", nil, ErrReauth
	}
	user, err := loadPasskeyUser(ctx, u.db, cur.UserID, nil)
	if err != nil {
		return "", nil, err
	}
	return u.beginRegistration(ctx, ceremony{Purpose: purposeAdd, UserID: cur.UserID}, user)
}

func (u *Users) FinishAddPasskey(ctx context.Context, cur Current, handle string, credential json.RawMessage, name string) (Passkey, error) {
	if !cur.Recent {
		return Passkey{}, ErrReauth
	}
	c, cred, err := u.finishRegistration(ctx, purposeAdd, handle, credential)
	if err != nil {
		return Passkey{}, err
	}
	if c.UserID != cur.UserID {
		return Passkey{}, errChallenge
	}
	var p Passkey
	err = pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		p, err = insertPasskey(ctx, tx, cur.UserID, name, cred)
		return err
	})
	return p, err
}

// Signup is a person joining with a passkey: through a setup link, giving
// their email and name, or through an invite, which fixes the email.
type Signup struct {
	SetupToken  string `json:"setup_token"`
	InviteToken string `json:"invite_token"`
	Email       string `json:"email"`
	Name        string `json:"name"`
}

func (u *Users) BeginPasskeySignup(ctx context.Context, s Signup) (string, *protocol.CredentialCreation, error) {
	n := newUser{Name: strings.TrimSpace(s.Name), Handle: make([]byte, 32)}
	_, _ = rand.Read(n.Handle)
	switch {
	case s.SetupToken != "" && s.InviteToken == "":
		n.Via, n.TokenHash, n.Email = "setup", hashToken(s.SetupToken), normalizeEmail(s.Email)
		if err := checkEmail(n.Email); err != nil {
			return "", nil, err
		}
		if err := checkSetup(ctx, u.db, n.TokenHash); err != nil {
			return "", nil, err
		}
	case s.InviteToken != "" && s.SetupToken == "":
		email, err := u.InviteEmail(ctx, s.InviteToken)
		if err != nil {
			return "", nil, err
		}
		n.Via, n.TokenHash, n.Email = "invite", hashToken(s.InviteToken), email
	default:
		return "", nil, fmt.Errorf("%w: give exactly one of setup_token and invite_token", crm.ErrInvalid)
	}
	user := &passkeyUser{handle: n.Handle, email: n.Email, name: n.Name}
	return u.beginRegistration(ctx, ceremony{Purpose: purposeSignup, Signup: &n}, user)
}

// FinishPasskeySignup creates the user with their first passkey and signs
// them in. Using up the setup link or invite, creating the user and storing
// the passkey commit together.
func (u *Users) FinishPasskeySignup(ctx context.Context, handle string, credential json.RawMessage, c Client) (SignIn, error) {
	cer, cred, err := u.finishRegistration(ctx, purposeSignup, handle, credential)
	if err != nil {
		return SignIn{}, err
	}
	n := cer.Signup
	var token string
	err = pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		var id int64
		var err error
		if n.Via == "setup" {
			id, err = claimSetup(ctx, tx, n.TokenHash, n.Email, n.Name, n.Handle)
		} else {
			if _, err = claimInvite(ctx, tx, n.TokenHash); err == nil {
				id, err = createUser(ctx, tx, n.Email, n.Name, n.Handle)
			}
		}
		if err != nil {
			return err
		}
		if _, err := insertPasskey(ctx, tx, id, "", cred); err != nil {
			return err
		}
		token, err = u.newSession(ctx, tx, id, MethodPasskey, c)
		return err
	})
	return SignIn{Session: token}, err
}
