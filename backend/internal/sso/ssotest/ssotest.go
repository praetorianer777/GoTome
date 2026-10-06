// Package ssotest is an OpenID Connect identity provider for tests: it
// signs in whoever its Claims describe, without asking, and can be made to
// answer the ways a broken or hostile one would.
package ssotest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// ClientID and ClientSecret are the one client the provider knows.
const (
	ClientID     = "gotome"
	ClientSecret = "test-secret"
)

// Provider is a running identity provider.
type Provider struct {
	URL string

	mu sync.Mutex
	// claims are added to every ID token; "sub" is who signs in.
	claims map[string]any
	// tamper changes the next ID token's claims before it is signed.
	tamper func(map[string]any)
	// otherKey signs with a key the provider does not publish.
	otherKey bool
	key      *rsa.PrivateKey
	stranger *rsa.PrivateKey
	grants   map[string]grant
}

type grant struct {
	nonce, challenge, redirectURI string
	claims                        map[string]any
}

// New starts a provider, stopped when the test ends.
func New(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{claims: map[string]any{}, grants: map[string]grant{}}
	var err error
	if p.key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	if p.stranger, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p.URL = srv.URL
	return p
}

// SignIn makes the next sign-ins those of the person the claims describe.
func (p *Provider) SignIn(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = maps.Clone(claims)
}

// Tamper changes the next ID token's claims, once.
func (p *Provider) Tamper(f func(claims map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tamper = f
}

// SignWithUnknownKey signs the next ID tokens with a key the provider does
// not publish.
func (p *Provider) SignWithUnknownKey(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.otherKey = on
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.URL,
		"authorization_endpoint":                p.URL + "/authorize",
		"token_endpoint":                        p.URL + "/token",
		"jwks_uri":                              p.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" || q.Get("redirect_uri") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	p.mu.Lock()
	p.grants[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), claims: maps.Clone(p.claims)}
	p.mu.Unlock()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	v.Set("iss", p.URL)
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	}
	if id != ClientID || secret != ClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	code := r.PostFormValue("code")
	p.mu.Lock()
	g, ok := p.grants[code]
	delete(p.grants, code)
	tamper, signer := p.tamper, p.key
	p.tamper = nil
	if p.otherKey {
		signer = p.stranger
	}
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if !ok || r.PostFormValue("redirect_uri") != g.redirectURI || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.URL, "aud": ClientID, "nonce": g.nonce,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
	maps.Copy(claims, g.claims)
	if tamper != nil {
		tamper(claims)
	}
	idToken, err := sign(signer, claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": rand.Text(), "token_type": "Bearer", "expires_in": 300, "id_token": idToken})
}

func sign(key *rsa.PrivateKey, claims map[string]any) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test"}}, nil)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
