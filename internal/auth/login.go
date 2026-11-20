package auth

import (
	"context"
	"fmt"

	"github.com/uncrm/uncrm/internal/crm"
)

// pendingLogin is a login whose first factor passed and whose second has not.
type pendingLogin struct {
	UserID int64  `json:"user_id"`
	First  string `json:"first"`
}

// factors is which second factors a user has.
type factors struct {
	totp, passkey bool
}

func (u *Users) factorsOf(ctx context.Context, userID int64) (factors, error) {
	var f factors
	err := u.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM totp WHERE user_id = $1), EXISTS (SELECT 1 FROM passkeys WHERE user_id = $1)`,
		userID).Scan(&f.totp, &f.passkey)
	return f, err
}

var errNoSecondFactor = fmt.Errorf("%w: a password needs a second factor, and this account has none. "+
	"Sign in with a passkey or a connected login, then add an authenticator app under Settings, Account", crm.ErrInvalid)

// secondFactors decides what must follow a first factor. A passkey needs
// nothing. A social identity needs an authenticator code only when the user
// enrolled an app. A password always needs a code or a passkey, so a user
// with neither cannot sign in with one.
func secondFactors(first string, f factors) ([]string, error) {
	var need []string
	if f.totp {
		need = append(need, MethodTOTP, MethodRecovery)
	}
	switch first {
	case MethodPasskey:
		return nil, nil
	case MethodPassword:
		if f.passkey {
			need = append(need, MethodPasskey)
		}
		if len(need) == 0 {
			return nil, errNoSecondFactor
		}
	}
	return need, nil
}

// afterFirstFactor opens a session, or a pending login when the user owes a
// second factor.
func (u *Users) afterFirstFactor(ctx context.Context, userID int64, first string, c Client) (SignIn, error) {
	f, err := u.factorsOf(ctx, userID)
	if err != nil {
		return SignIn{}, err
	}
	need, err := secondFactors(first, f)
	if err != nil {
		return SignIn{}, err
	}
	if len(need) == 0 {
		return u.signIn(ctx, userID, first, c)
	}
	handle, err := putChallenge(ctx, u.db, kindSecondFactor, pendingLogin{UserID: userID, First: first})
	if err != nil {
		return SignIn{}, err
	}
	return SignIn{Pending: handle, SecondFactors: need}, nil
}

// PendingFactors is what the pending login behind handle accepts.
func (u *Users) PendingFactors(ctx context.Context, handle string) ([]string, error) {
	var p pendingLogin
	if err := peekChallenge(ctx, u.db, kindSecondFactor, handle, &p); err != nil {
		return nil, err
	}
	f, err := u.factorsOf(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return secondFactors(p.First, f)
}

// SecondFactorCode completes a pending login with an authenticator or
// recovery code. A wrong code leaves the login pending, so a typo can be
// retried within the throttle.
func (u *Users) SecondFactorCode(ctx context.Context, handle, code string, c Client) (SignIn, error) {
	var p pendingLogin
	if err := peekChallenge(ctx, u.db, kindSecondFactor, handle, &p); err != nil {
		return SignIn{}, err
	}
	second, err := u.useCode(ctx, p.UserID, code, c)
	if err != nil {
		return SignIn{}, err
	}
	return u.completePending(ctx, handle, p, second, c)
}

func (u *Users) completePending(ctx context.Context, handle string, p pendingLogin, second string, c Client) (SignIn, error) {
	ok, err := dropChallenge(ctx, u.db, handle)
	if err != nil {
		return SignIn{}, err
	}
	if !ok {
		return SignIn{}, errChallenge
	}
	return u.signIn(ctx, p.UserID, joinMethods(p.First, second), c)
}

// ReauthCode re-verifies the current session with an authenticator or
// recovery code.
func (u *Users) ReauthCode(ctx context.Context, cur Current, code string, c Client) error {
	if _, err := u.useCode(ctx, cur.UserID, code, c); err != nil {
		return err
	}
	return u.reauthenticated(ctx, cur)
}

// Options describes the sign-in page.
func (u *Users) Options(ctx context.Context) (Options, error) {
	open, err := u.SetupOpen(ctx)
	return Options{Providers: u.providerInfo(), PasswordLogin: u.cfg.PasswordLogin, SetupRequired: open}, err
}
