package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	HostedDomain  string // hd, as Google sends for Workspace accounts
}

type GitHubUser struct {
	ID     int64
	Login  string
	Emails []GitHubEmail
}

type GitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type grant struct {
	id        Identity
	gh        GitHubUser
	nonce     string
	challenge string
	clientID  string
}

// Issuer is an OIDC provider and a GitHub stand-in on one httptest server.
// /authorize approves at once as Next (or NextGitHub under
// /login/oauth/authorize) and redirects back with a code. The token
// endpoint checks the PKCE verifier and signs RS256 ID tokens that carry
// the nonce.
type Issuer struct {
	*httptest.Server
	key *rsa.PrivateKey

	mu         sync.Mutex
	Next       Identity
	NextGitHub GitHubUser
	grants     map[string]grant
	tokens     map[string]grant
}

func NewIssuer() *Issuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	iss := &Issuer{key: key, grants: map[string]grant{}, tokens: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", iss.discovery)
	mux.HandleFunc("GET /jwks", iss.jwks)
	mux.HandleFunc("GET /authorize", iss.authorize)
	mux.HandleFunc("POST /token", iss.token)
	mux.HandleFunc("GET /userinfo", iss.userinfo)
	mux.HandleFunc("GET /login/oauth/authorize", iss.authorize)
	mux.HandleFunc("POST /login/oauth/access_token", iss.token)
	mux.HandleFunc("GET /api/user", iss.githubUser)
	mux.HandleFunc("GET /api/user/emails", iss.githubEmails)
	iss.Server = httptest.NewServer(mux)
	return iss
}

func (iss *Issuer) SetNext(id Identity) {
	iss.mu.Lock()
	defer iss.mu.Unlock()
	iss.Next = id
}

func (iss *Issuer) SetNextGitHub(u GitHubUser) {
	iss.mu.Lock()
	defer iss.mu.Unlock()
	iss.NextGitHub = u
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (iss *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                iss.URL,
		"authorization_endpoint":                iss.URL + "/authorize",
		"token_endpoint":                        iss.URL + "/token",
		"jwks_uri":                              iss.URL + "/jwks",
		"userinfo_endpoint":                     iss.URL + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (iss *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := iss.key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test",
		"n": b64.EncodeToString(pub.N.Bytes()), "e": b64.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (iss *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Host == "" {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "PKCE S256 required", http.StatusBadRequest)
		return
	}
	code := b64.EncodeToString(randomBytes(16))
	iss.mu.Lock()
	iss.grants[code] = grant{id: iss.Next, gh: iss.NextGitHub, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), clientID: q.Get("client_id")}
	iss.mu.Unlock()
	back := redirect.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (iss *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	iss.mu.Lock()
	g, ok := iss.grants[r.PostForm.Get("code")]
	delete(iss.grants, r.PostForm.Get("code"))
	iss.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || b64.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	access := b64.EncodeToString(randomBytes(16))
	iss.mu.Lock()
	iss.tokens[access] = g
	iss.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": iss.URL, "aud": g.clientID, "sub": g.id.Subject, "nonce": g.nonce,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"email": g.id.Email, "email_verified": g.id.EmailVerified, "name": g.id.Name,
	}
	if g.id.HostedDomain != "" {
		claims["hd"] = g.id.HostedDomain
	}
	writeJSON(w, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300, "id_token": iss.sign(claims)})
}

func (iss *Issuer) sign(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"})
	payload, _ := json.Marshal(claims)
	unsigned := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, iss.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return unsigned + "." + b64.EncodeToString(sig)
}

func (iss *Issuer) bearer(r *http.Request) (grant, bool) {
	iss.mu.Lock()
	defer iss.mu.Unlock()
	var tok string
	_, _ = fmt.Sscanf(r.Header.Get("Authorization"), "Bearer %s", &tok)
	g, ok := iss.tokens[tok]
	return g, ok
}

func (iss *Issuer) userinfo(w http.ResponseWriter, r *http.Request) {
	g, ok := iss.bearer(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"sub": g.id.Subject, "email": g.id.Email, "email_verified": g.id.EmailVerified, "name": g.id.Name})
}

func (iss *Issuer) githubUser(w http.ResponseWriter, r *http.Request) {
	g, ok := iss.bearer(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"id": g.gh.ID, "login": g.gh.Login, "name": ""})
}

func (iss *Issuer) githubEmails(w http.ResponseWriter, r *http.Request) {
	g, ok := iss.bearer(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, g.gh.Emails)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func CallbackFor(authorizeURL string) (*url.URL, error) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(authorizeURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("authorize answered %s", res.Status)
	}
	return url.Parse(res.Header.Get("Location"))
}
