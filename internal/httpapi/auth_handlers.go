package httpapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/unkeyed/keyring/internal/auth"
	"github.com/unkeyed/keyring/internal/crm"
)

type signInResponse struct {
	Status        string   `json:"status"` // "signed_in" or "second_factor"
	SecondFactors []string `json:"second_factors,omitempty"`
}

// writeSignIn sets the cookie a sign-in step earned. A new session replaces
// the one the browser held, so re-verifying never piles up sessions.
func (s *server) writeSignIn(w http.ResponseWriter, r *http.Request, in auth.SignIn) error {
	if in.Pending != "" {
		s.users.SetPendingCookie(w, in.Pending)
		writeJSON(w, http.StatusOK, signInResponse{Status: "second_factor", SecondFactors: in.SecondFactors})
		return nil
	}
	if err := s.replaceSession(w, r, in.Session); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, signInResponse{Status: "signed_in"})
	return nil
}

func (s *server) replaceSession(w http.ResponseWriter, r *http.Request, session string) error {
	if err := s.users.Logout(r.Context(), s.users.Cookie(r, auth.SessionCookie)); err != nil {
		return err
	}
	s.users.SetSessionCookie(w, session)
	s.users.ClearPendingCookie(w)
	return nil
}

func (s *server) current(r *http.Request) (auth.Current, error) {
	return s.users.Current(r.Context(), s.users.Cookie(r, auth.SessionCookie))
}

func (s *server) optionalCurrent(r *http.Request) (*auth.Current, error) {
	c, err := s.current(r)
	if errors.Is(err, auth.ErrUnauthorized) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type ceremonyStart struct {
	Handle  string `json:"handle"`
	Options any    `json:"options"`
}

type ceremonyFinish struct {
	Handle     string          `json:"handle"`
	Credential json.RawMessage `json:"credential"`
	Name       string          `json:"name,omitempty"`
}

func readFinish(r *http.Request) (ceremonyFinish, error) {
	var b ceremonyFinish
	if err := readJSON(r, &b); err != nil {
		return b, err
	}
	if b.Handle == "" || len(b.Credential) == 0 {
		return b, fmt.Errorf("%w: handle and credential are required", crm.ErrInvalid)
	}
	return b, nil
}

func (s *server) authOptions(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	o, err := s.users.Options(r.Context())
	return respond(w, http.StatusOK, o, err)
}

func (s *server) passkeyBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	h, opts, err := s.users.BeginPasskeyLogin(r.Context())
	return respond(w, http.StatusOK, ceremonyStart{h, opts}, err)
}

func (s *server) passkeyFinish(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	b, err := readFinish(r)
	if err != nil {
		return err
	}
	in, err := s.users.FinishPasskeyLogin(r.Context(), b.Handle, b.Credential, s.users.ClientOf(r))
	if err != nil {
		return err
	}
	return s.writeSignIn(w, r, in)
}

func (s *server) passwordLogin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	in, err := s.users.PasswordLogin(r.Context(), b.Email, b.Password, s.users.ClientOf(r))
	if err != nil {
		return err
	}
	return s.writeSignIn(w, r, in)
}

func (s *server) secondFactor(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	f, err := s.users.PendingFactors(r.Context(), s.users.Cookie(r, auth.PendingCookie))
	return respond(w, http.StatusOK, signInResponse{Status: "second_factor", SecondFactors: f}, err)
}

func (s *server) secondFactorCode(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	in, err := s.users.SecondFactorCode(r.Context(), s.users.Cookie(r, auth.PendingCookie), b.Code, s.users.ClientOf(r))
	if err != nil {
		return err
	}
	return s.writeSignIn(w, r, in)
}

func (s *server) secondFactorPasskeyBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	h, opts, err := s.users.BeginSecondFactorPasskey(r.Context(), s.users.Cookie(r, auth.PendingCookie))
	return respond(w, http.StatusOK, ceremonyStart{h, opts}, err)
}

func (s *server) secondFactorPasskeyFinish(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	b, err := readFinish(r)
	if err != nil {
		return err
	}
	in, err := s.users.FinishSecondFactorPasskey(r.Context(), s.users.Cookie(r, auth.PendingCookie), b.Handle, b.Credential, s.users.ClientOf(r))
	if err != nil {
		return err
	}
	return s.writeSignIn(w, r, in)
}

func withError(path string, err error) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "error=" + url.QueryEscape(publicMessage(err))
}

// publicMessage is what a redirect may show: the message of a 4xx error,
// or a generic line for anything the server should log instead.
func publicMessage(err error) string {
	if code := statusOf(err); code < 500 {
		return err.Error()
	}
	return "sign-in failed on the server; try again"
}

// errCrossSiteStart refuses a link or reauth flow that another site
// started. The provider may answer without asking anything, so a page that
// sends a signed-in browser here could otherwise mark a stolen session as
// recently signed in, or link the wrong account.
var errCrossSiteStart = fmt.Errorf("%w: start this from your account settings", crm.ErrInvalid)

func (s *server) oauthStart(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	q := r.URL.Query()
	start := auth.OAuthStart{Provider: r.PathValue("provider"), Intent: cmp.Or(q.Get("intent"), auth.IntentLogin), Token: q.Get("token"), Next: q.Get("next")}
	back, returnTo := "/login", ""
	switch start.Intent {
	case auth.IntentLink, auth.IntentReauth:
		back = "/settings/account"
		if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			http.Redirect(w, r, withError(back, errCrossSiteStart), http.StatusSeeOther)
			return nil
		}
	case auth.IntentSetup:
		back = "/setup/" + url.PathEscape(start.Token)
		returnTo = back
	case auth.IntentInvite:
		back = "/invite/" + url.PathEscape(start.Token)
		returnTo = back
	}
	cur, err := s.optionalCurrent(r)
	if err != nil {
		return err
	}
	to, state, err := s.users.StartOAuth(r.Context(), start, cur)
	if err != nil {
		s.logIfServer(r, err)
		http.Redirect(w, r, withError(back, err), http.StatusSeeOther)
		return nil
	}
	s.users.SetOAuthCookie(w, state, returnTo)
	http.Redirect(w, r, to, http.StatusSeeOther)
	return nil
}

func (s *server) oauthCallback(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	q := r.URL.Query()
	cur, err := s.optionalCurrent(r)
	if err != nil {
		return err
	}
	s.users.ClearOAuthCookie(w)
	res, err := s.users.FinishOAuth(r.Context(), auth.OAuthCallback{
		Provider: r.PathValue("provider"), State: q.Get("state"), CookieState: s.users.Cookie(r, auth.OAuthCookie),
		Code: q.Get("code"), Error: q.Get("error"),
	}, cur, s.users.ClientOf(r))
	if err != nil {
		s.logIfServer(r, err)
		if back := s.users.Cookie(r, auth.OAuthBackCookie); res.Redirect == "/login" && back != "" && auth.SafeNext(back) == back {
			res.Redirect = back
		}
		http.Redirect(w, r, withError(res.Redirect, err), http.StatusSeeOther)
		return nil
	}
	switch {
	case res.SignIn.Session != "":
		if err := s.replaceSession(w, r, res.SignIn.Session); err != nil {
			return err
		}
	case res.SignIn.Pending != "":
		s.users.SetPendingCookie(w, res.SignIn.Pending)
	}
	http.Redirect(w, r, res.Redirect, http.StatusSeeOther)
	return nil
}

func (s *server) logIfServer(r *http.Request, err error) {
	if statusOf(err) >= 500 {
		s.log.Error("sign-in redirect failed", "path", r.URL.Path, "err", err)
	}
}

func (s *server) checkSetup(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	recovery, err := s.users.CheckSetupToken(r.Context(), r.PathValue("token"))
	return respond(w, http.StatusOK, map[string]bool{"recovery": recovery}, err)
}

func (s *server) inviteInfo(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	email, err := s.users.InviteEmail(r.Context(), r.PathValue("token"))
	return respond(w, http.StatusOK, map[string]string{"email": email}, err)
}

func (s *server) signupPasskeyBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b auth.Signup
	if err := readJSON(r, &b); err != nil {
		return err
	}
	h, opts, err := s.users.BeginPasskeySignup(r.Context(), b)
	return respond(w, http.StatusOK, ceremonyStart{h, opts}, err)
}

func (s *server) signupPasskeyFinish(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	b, err := readFinish(r)
	if err != nil {
		return err
	}
	in, err := s.users.FinishPasskeySignup(r.Context(), b.Handle, b.Credential, s.users.ClientOf(r))
	if err != nil {
		return err
	}
	return s.writeSignIn(w, r, in)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	if err := s.users.Logout(r.Context(), s.users.Cookie(r, auth.SessionCookie)); err != nil {
		return err
	}
	s.users.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Account routes. Each resolves the current session first, since changes
// to sign-in methods depend on how recently it proved a factor.

func (s *server) withCurrent(r *http.Request, f func(auth.Current) error) error {
	cur, err := s.current(r)
	if err != nil {
		return err
	}
	return f(cur)
}

// recentOnly runs f only for a session that proved a factor within the
// last 15 minutes, so a stolen session cookie cannot mint keys, invite
// people, swap provider secrets or remove teammates.
func (s *server) recentOnly(r *http.Request, f func() error) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		if !cur.Recent {
			return auth.ErrReauth
		}
		return f()
	})
}

func (s *server) account(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		a, err := s.users.Account(r.Context(), cur)
		return respond(w, http.StatusOK, a, err)
	})
}

func (s *server) addPasskeyBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		h, opts, err := s.users.BeginAddPasskey(r.Context(), cur)
		return respond(w, http.StatusOK, ceremonyStart{h, opts}, err)
	})
}

func (s *server) addPasskey(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	b, err := readFinish(r)
	if err != nil {
		return err
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		p, err := s.users.FinishAddPasskey(r.Context(), cur, b.Handle, b.Credential, b.Name)
		return respond(w, http.StatusCreated, p, err)
	})
}

func (s *server) removePasskey(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.RemovePasskey(r.Context(), cur, r.PathValue("id")))
	})
}

func (s *server) unlinkIdentity(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.Unlink(r.Context(), cur, r.PathValue("provider")))
	})
}

func (s *server) totpBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		e, err := s.users.BeginTOTP(r.Context(), cur)
		return respond(w, http.StatusOK, e, err)
	})
}

type recoveryCodes struct {
	Codes []string `json:"recovery_codes"`
}

func (s *server) totpFinish(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b struct {
		Handle string `json:"handle"`
		Code   string `json:"code"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		codes, err := s.users.FinishTOTP(r.Context(), cur, b.Handle, b.Code)
		return respond(w, http.StatusCreated, recoveryCodes{codes}, err)
	})
}

func (s *server) totpDisable(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.DisableTOTP(r.Context(), cur))
	})
}

func (s *server) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		codes, err := s.users.RegenerateRecoveryCodes(r.Context(), cur)
		return respond(w, http.StatusOK, recoveryCodes{codes}, err)
	})
}

func (s *server) setPassword(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.SetPassword(r.Context(), cur, b.Password))
	})
}

func (s *server) removePassword(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.RemovePassword(r.Context(), cur))
	})
}

func (s *server) revokeSession(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: no such session", crm.ErrNotFound)
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.RevokeSession(r.Context(), cur, id))
	})
}

func (s *server) reauthPasskeyBegin(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	return s.withCurrent(r, func(cur auth.Current) error {
		h, opts, err := s.users.BeginReauthPasskey(r.Context(), cur)
		return respond(w, http.StatusOK, ceremonyStart{h, opts}, err)
	})
}

func (s *server) reauthPasskey(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	b, err := readFinish(r)
	if err != nil {
		return err
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.FinishReauthPasskey(r.Context(), cur, b.Handle, b.Credential))
	})
}

func (s *server) reauthCode(w http.ResponseWriter, r *http.Request, _ crm.Actor) error {
	var b struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &b); err != nil {
		return err
	}
	return s.withCurrent(r, func(cur auth.Current) error {
		return noContent(w, s.users.ReauthCode(r.Context(), cur, b.Code, s.users.ClientOf(r)))
	})
}

func (s *server) removeUser(w http.ResponseWriter, r *http.Request, a crm.Actor) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: no such user", crm.ErrNotFound)
	}
	return s.recentOnly(r, func() error {
		return noContent(w, s.users.Remove(r.Context(), a, id, s.keys))
	})
}
