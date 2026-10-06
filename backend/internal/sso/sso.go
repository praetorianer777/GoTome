// Package sso signs people in through an OpenID Connect identity provider:
// the authorization code flow with PKCE, a state the browser must bring
// back in a cookie as well as in the address, and a nonce the ID token must
// carry. Which account a person gets is internal/auth's to decide.
package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

const (
	// loginTTL is how long a person has at the identity provider.
	loginTTL = 10 * time.Minute
	// requestTimeout bounds every request to the identity provider.
	requestTimeout = 10 * time.Second
)

var (
	// ErrNotConfigured is a sign-in while no identity provider is set up.
	ErrNotConfigured = errors.New("no identity provider is set up")
	// ErrBadState is a callback whose state is missing, unknown, used,
	// expired, not this browser's, or for a link someone else asked for.
	ErrBadState = errors.New("the sign-in was not started here, or has run out")
	// ErrRefused is the identity provider answering with an error, as when
	// the person cancels.
	ErrRefused = errors.New("the identity provider refused the sign-in")
	// ErrInvalidToken is an answer that does not hold: another issuer, a
	// token that does not verify, or another nonce.
	ErrInvalidToken = errors.New("the identity provider's answer is not valid")
)

// Service signs people in through the identity provider the settings name.
type Service struct {
	pool     *pgxpool.Pool
	auth     *auth.Service
	settings *settings.Store
	// Client reaches the identity provider; tests may replace it.
	Client *http.Client
	now    func() time.Time

	mu sync.Mutex
	// providers are the discovered providers by issuer, each with the keys
	// it signs with, fetched again when a token names one not yet seen.
	providers map[string]*oidc.Provider
}

// NewService returns a Service.
func NewService(pool *pgxpool.Pool, accounts *auth.Service, store *settings.Store) *Service {
	return &Service{
		pool: pool, auth: accounts, settings: store,
		Client: &http.Client{Timeout: requestTimeout}, now: time.Now,
		providers: map[string]*oidc.Provider{},
	}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

type config struct {
	issuer, clientID, clientSecret string
	scopes                         []string
}

func (s *Service) config(ctx context.Context) (config, error) {
	var c config
	var err error
	if c.issuer, err = s.settings.Text(ctx, settings.OIDCIssuer); err != nil {
		return c, err
	}
	if c.clientID, err = s.settings.Text(ctx, settings.OIDCClientID); err != nil {
		return c, err
	}
	if c.issuer == "" || c.clientID == "" {
		return c, ErrNotConfigured
	}
	if c.clientSecret, _, err = s.settings.Secret(ctx, settings.OIDCClientSecret); err != nil {
		return c, err
	}
	scopes, err := s.settings.Text(ctx, settings.OIDCScopes)
	c.scopes = strings.Fields(scopes)
	return c, err
}

// Name is what the sign-in page calls the identity provider, and false
// while none is set up.
func (s *Service) Name(ctx context.Context) (string, bool, error) {
	if _, err := s.config(ctx); errors.Is(err, ErrNotConfigured) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	name, err := s.settings.Text(ctx, settings.OIDCName)
	return name, err == nil, err
}

func (s *Service) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.mu.Lock()
	p, ok := s.providers[issuer]
	s.mu.Unlock()
	if ok {
		return p, nil
	}
	// The provider keeps the context for fetching its keys later, long
	// after this request is gone.
	p, err := oidc.NewProvider(oidc.ClientContext(context.WithoutCancel(ctx), s.Client), issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering %s: %w", issuer, err)
	}
	s.mu.Lock()
	s.providers[issuer] = p
	s.mu.Unlock()
	return p, nil
}

func (s *Service) oauth(p *oidc.Provider, c config, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.clientID, ClientSecret: c.clientSecret, Endpoint: p.Endpoint(),
		RedirectURL: redirectURI, Scopes: c.scopes,
	}
}

// Start begins a sign-in, or with link the linking of an identity to that
// account. It returns where to send the browser and the state to keep in
// its cookie. redirectURI is the callback's address as the browser reaches
// it, which the identity provider must know.
func (s *Service) Start(ctx context.Context, redirectURI, returnTo string, link *uuid.UUID) (authURL, state string, err error) {
	c, err := s.config(ctx)
	if err != nil {
		return "", "", err
	}
	p, err := s.provider(ctx, c.issuer)
	if err != nil {
		return "", "", err
	}
	state, nonce := random(), random()
	verifier := oauth2.GenerateVerifier()
	q := sqlc.New(s.pool)
	now := s.now()
	if err := q.DeleteExpiredOidcLogins(ctx, now); err != nil {
		return "", "", err
	}
	err = q.CreateOidcLogin(ctx, sqlc.CreateOidcLoginParams{
		StateHash: hash(state), Nonce: nonce, Verifier: verifier, ReturnTo: returnTo, LinkUserID: link,
		ExpiresAt: now.Add(loginTTL),
	})
	if err != nil {
		return "", "", err
	}
	authURL = s.oauth(p, c, redirectURI).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	return authURL, state, nil
}

// Callback is the browser's return from the identity provider.
type Callback struct {
	// State, Code, Issuer and Error are the query's state, code, iss and
	// error.
	State, Code, Issuer, Error string
	// BrowserState is the state in the browser's cookie.
	BrowserState string
	RedirectURI  string
	UserAgent    string
	// Viewer is who is signed in on the browser, if anyone.
	Viewer *uuid.UUID
}

// Outcome is what a callback came to.
type Outcome struct {
	// ReturnTo is where in the app the person was going; it is set even
	// when the sign-in failed, once the state was known.
	ReturnTo string
	// Linked is a link of an identity to the viewer's account, which a
	// success made; otherwise a success signed User in with the session
	// Token.
	Linked bool
	User   auth.User
	Token  string
}

// Finish completes a sign-in or a link.
func (s *Service) Finish(ctx context.Context, cb Callback) (Outcome, error) {
	var out Outcome
	if cb.State == "" || subtle.ConstantTimeCompare([]byte(cb.State), []byte(cb.BrowserState)) != 1 {
		return out, ErrBadState
	}
	row, err := sqlc.New(s.pool).TakeOidcLogin(ctx, sqlc.TakeOidcLoginParams{StateHash: hash(cb.State), Now: s.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrBadState
	}
	if err != nil {
		return out, err
	}
	out.ReturnTo, out.Linked = row.ReturnTo, row.LinkUserID != nil
	if cb.Error != "" {
		return out, fmt.Errorf("%w: %.100s", ErrRefused, cb.Error)
	}
	if row.LinkUserID != nil && (cb.Viewer == nil || *cb.Viewer != *row.LinkUserID) {
		return out, ErrBadState
	}
	c, err := s.config(ctx)
	if err != nil {
		return out, err
	}
	// A provider that names itself in the answer (RFC 9207) must be the one
	// asked, or another one is answering in its place.
	if cb.Issuer != "" && cb.Issuer != c.issuer {
		return out, fmt.Errorf("%w: answered by %.200s", ErrInvalidToken, cb.Issuer)
	}
	p, err := s.provider(ctx, c.issuer)
	if err != nil {
		return out, err
	}
	token, err := s.oauth(p, c, cb.RedirectURI).Exchange(oidc.ClientContext(ctx, s.Client), cb.Code, oauth2.VerifierOption(row.Verifier))
	if err != nil {
		return out, fmt.Errorf("exchanging the code: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return out, fmt.Errorf("%w: no ID token", ErrInvalidToken)
	}
	idToken, err := p.Verifier(&oidc.Config{ClientID: c.clientID, Now: s.now}).Verify(ctx, raw)
	if err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(row.Nonce)) != 1 {
		return out, fmt.Errorf("%w: another nonce", ErrInvalidToken)
	}
	id, groups, err := s.identity(ctx, idToken)
	if err != nil {
		return out, err
	}

	if out.Linked {
		return out, s.auth.LinkIdentity(ctx, *row.LinkUserID, id)
	}
	policy, err := s.policy(ctx, groups)
	if err != nil {
		return out, err
	}
	out.User, out.Token, err = s.auth.SignInIdentity(ctx, id, policy, cb.UserAgent)
	return out, err
}

// identity reads who the token says the person is, and their groups.
func (s *Service) identity(ctx context.Context, t *oidc.IDToken) (auth.Identity, []string, error) {
	var claims struct {
		Email             string          `json:"email"`
		EmailVerified     json.RawMessage `json:"email_verified"`
		PreferredUsername string          `json:"preferred_username"`
		Nickname          string          `json:"nickname"`
	}
	var all map[string]json.RawMessage
	if err := t.Claims(&claims); err != nil {
		return auth.Identity{}, nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if err := t.Claims(&all); err != nil {
		return auth.Identity{}, nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	id := auth.Identity{Issuer: t.Issuer, Subject: t.Subject, Username: claims.PreferredUsername}
	if id.Username == "" {
		id.Username = claims.Nickname
	}
	if claims.Email != "" {
		id.Email = &claims.Email
		// Some providers send the flag as a string.
		v := strings.Trim(string(claims.EmailVerified), `"`)
		id.EmailVerified = v == "true"
	}
	claim, err := s.settings.Text(ctx, settings.OIDCGroupsClaim)
	if err != nil {
		return auth.Identity{}, nil, err
	}
	return id, groupsOf(all[claim]), nil
}

// groupsOf reads a groups claim: a list of names, or one name.
func groupsOf(raw json.RawMessage) []string {
	var groups []string
	if json.Unmarshal(raw, &groups) == nil {
		return groups
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	return nil
}

// policy is what the settings let a sign-in do, with the role the groups
// give.
func (s *Service) policy(ctx context.Context, groups []string) (auth.IdentityPolicy, error) {
	var p auth.IdentityPolicy
	byRole := map[string][]string{}
	for role, key := range map[string]string{
		auth.RoleAdmin: settings.OIDCAdminGroups, auth.RoleEditor: settings.OIDCEditorGroups, auth.RoleReader: settings.OIDCReaderGroups,
	} {
		names, err := s.settings.Items(ctx, key)
		if err != nil {
			return p, err
		}
		byRole[role] = names
		p.Sync = p.Sync || len(names) > 0
	}
	for _, role := range []string{auth.RoleAdmin, auth.RoleEditor, auth.RoleReader} {
		if slices.ContainsFunc(byRole[role], func(name string) bool {
			return slices.ContainsFunc(groups, func(g string) bool { return strings.EqualFold(g, name) })
		}) {
			p.Role = role
			break
		}
	}
	if p.Role == "" {
		role, err := s.settings.Text(ctx, settings.OIDCDefaultRole)
		if err != nil {
			return p, err
		}
		if role != "none" {
			p.Role = role
		}
	}
	signup, err := s.settings.Text(ctx, settings.OIDCSignup)
	if err != nil {
		return p, err
	}
	link, err := s.settings.Text(ctx, settings.OIDCLinkByEmail)
	if err != nil {
		return p, err
	}
	p.Signup, p.LinkByEmail = signup == "on", link == "on"
	return p, nil
}

func random() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hash(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}
