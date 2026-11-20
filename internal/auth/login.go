package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/uncrm/uncrm/internal/crm"
)

type pendingLogin struct {
	UserID int64  `json:"user_id"`
	First  string `json:"first"`
	// Link is a provider login matched to the user by email. It is linked
	// only once the second factor passes, so a failed code leaves no link.
	Link *ExternalIdentity `json:"link,omitempty"`
}

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

// secondFactors decides what must follow a first factor: a passkey, a
// password, or a provider ID. A provider login matched to the user by email
// alone also takes the user's passkey, if they have one: the provider vouches
// for the address, not for the person who holds this account.
func secondFactors(p pendingLogin, f factors) ([]string, error) {
	var codes []string
	if f.totp {
		codes = []string{MethodTOTP, MethodRecovery}
	}
	switch {
	case p.First == MethodPasskey:
		return nil, nil
	case p.First == MethodPassword:
		need := codes
		if f.passkey {
			need = append(need, MethodPasskey)
		}
		if len(need) == 0 {
			return nil, errNoSecondFactor
		}
		return need, nil
	case p.Link != nil && f.passkey:
		return append(codes, MethodPasskey), nil
	default:
		return codes, nil
	}
}

func (u *Users) afterFirstFactor(ctx context.Context, userID int64, first string, link *ExternalIdentity, c Client) (SignIn, error) {
	f, err := u.factorsOf(ctx, userID)
	if err != nil {
		return SignIn{}, err
	}
	p := pendingLogin{UserID: userID, First: first, Link: link}
	need, err := secondFactors(p, f)
	if err != nil {
		return SignIn{}, err
	}
	if len(need) == 0 {
		return u.openSession(ctx, p, first, "", c)
	}
	handle, err := putChallenge(ctx, u.db, kindSecondFactor, p)
	if err != nil {
		return SignIn{}, err
	}
	return SignIn{Pending: handle, SecondFactors: need}, nil
}

func (u *Users) PendingFactors(ctx context.Context, handle string) ([]string, error) {
	var p pendingLogin
	if err := peekChallenge(ctx, u.db, kindSecondFactor, handle, &p); err != nil {
		return nil, err
	}
	f, err := u.factorsOf(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return secondFactors(p, f)
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
	return u.openSession(ctx, p, joinMethods(p.First, second), handle, c)
}

// openSession uses up the pending login behind handle, if any, links its
// provider login, and opens the session, all in one transaction.
func (u *Users) openSession(ctx context.Context, p pendingLogin, method, handle string, c Client) (SignIn, error) {
	var token string
	err := pgx.BeginFunc(ctx, u.db, func(tx pgx.Tx) error {
		if handle != "" {
			if ok, err := dropChallenge(ctx, tx, handle); err != nil || !ok {
				return errOr(err, errChallenge)
			}
		}
		if p.Link != nil {
			if err := u.insertIdentity(ctx, tx, p.UserID, *p.Link); err != nil {
				return err
			}
		}
		var err error
		token, err = u.newSession(ctx, tx, p.UserID, method, c)
		return err
	})
	return SignIn{Session: token}, err
}

func (u *Users) ReauthCode(ctx context.Context, cur Current, code string, c Client) error {
	if _, err := u.useCode(ctx, cur.UserID, code, c); err != nil {
		return err
	}
	return u.reauthenticated(ctx, cur)
}

func (u *Users) Options(ctx context.Context) (Options, error) {
	open, err := u.SetupOpen(ctx)
	return Options{Providers: u.providerInfo(), PasswordLogin: u.cfg.PasswordLogin, SetupRequired: open}, err
}
