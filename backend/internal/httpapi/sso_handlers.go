package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/sso"
)

// stateCookie carries a sign-in's state through the identity provider, so
// that only the browser that started it can finish it.
const stateCookie = "gotome_oidc"

// callbackPath is where the identity provider sends the browser back to;
// it must be registered there.
const callbackPath = "/auth/oidc/callback"

type signInMethods struct {
	// Password is whether the sign-in page offers user name and password.
	Password bool `json:"password"`
	// SSO is the identity provider, if one is set up.
	SSO *ssoMethod `json:"sso,omitempty"`
}

type ssoMethod struct {
	// Name is what the button calls the provider.
	Name string `json:"name"`
}

type ssoStartRequest struct {
	// ReturnTo is the path in the app to go to once signed in.
	ReturnTo string `json:"returnTo,omitempty"`
}

type ssoRedirect struct {
	// URL is the identity provider's page to send the browser to.
	URL string `json:"url"`
}

type ssoCallbackQuery struct {
	State string `query:"state" doc:"The state the sign-in was started with"`
	Code  string `query:"code" doc:"The authorization code"`
	Iss   string `query:"iss" doc:"The issuer of the answer (RFC 9207)"`
	Error string `query:"error" doc:"What the identity provider refused, instead of a code"`
}

type linkedIdentity struct {
	ID         uuid.UUID `json:"id"`
	Issuer     string    `json:"issuer"`
	Email      *string   `json:"email,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

type identityList struct {
	Items []linkedIdentity `json:"items"`
}

func (s *Server) getSignInMethods(w http.ResponseWriter, r *http.Request) error {
	password, err := s.Auth.PasswordsAllowed(r.Context())
	if err != nil {
		return err
	}
	out := signInMethods{Password: password}
	if s.SSO != nil {
		name, ok, err := s.SSO.Name(r.Context())
		if err != nil {
			return err
		}
		if ok {
			out.SSO = &ssoMethod{Name: name}
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) postSSOStart(w http.ResponseWriter, r *http.Request) error {
	var req ssoStartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	return s.startSSO(w, r, appPath(req.ReturnTo), nil)
}

func (s *Server) postIdentity(w http.ResponseWriter, r *http.Request) error {
	return s.startSSO(w, r, "/profile", &UserFrom(r.Context()).ID)
}

func (s *Server) startSSO(w http.ResponseWriter, r *http.Request, returnTo string, link *uuid.UUID) error {
	if s.SSO == nil {
		return ErrConflict("Single sign-on is not set up.")
	}
	address := clientAddress(r)
	if wait := s.Logins.ssoStarts.RetryAfter(address); wait > 0 {
		return ErrTooManyRequests("Too many sign-ins were started. Wait a little and try again.", wait)
	}
	s.Logins.ssoStarts.Fail(address)
	target, state, err := s.SSO.Start(r.Context(), callbackURL(r), returnTo, link)
	if errors.Is(err, sso.ErrNotConfigured) {
		return ErrConflict("Single sign-on is not set up.")
	}
	if err != nil {
		s.Log.WarnContext(r.Context(), "the identity provider cannot be reached", "error", err)
		return ErrUnavailable("The identity provider cannot be reached. Try again later.")
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: APIPrefix + "/auth/oidc", MaxAge: int((10 * time.Minute).Seconds()),
		HttpOnly: true, Secure: overTLS(r), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, r, http.StatusOK, ssoRedirect{URL: target})
	return nil
}

// ssoCallback ends at a page of the app either way: an error goes there as
// ?sso=<reason> for the page to put into words.
func (s *Server) ssoCallback(w http.ResponseWriter, r *http.Request) error {
	var q ssoCallbackQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	browserState := ""
	if c, err := r.Cookie(stateCookie); err == nil {
		browserState = c.Value
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Path: APIPrefix + "/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: overTLS(r), SameSite: http.SameSiteLaxMode,
	})
	cb := sso.Callback{
		State: q.State, Code: q.Code, Issuer: q.Iss, Error: q.Error, BrowserState: browserState,
		RedirectURI: callbackURL(r), UserAgent: r.UserAgent(),
	}
	if user := UserFrom(r.Context()); user != nil {
		cb.Viewer = &user.ID
	}
	if s.SSO == nil {
		http.Redirect(w, r, "/login?sso=off", http.StatusFound)
		return nil
	}
	out, err := s.SSO.Finish(r.Context(), cb)
	if err != nil {
		reason := ssoReason(err)
		if reason == "failed" {
			s.Log.WarnContext(r.Context(), "a sign-in through the identity provider failed", "error", err)
		}
		page := "/login"
		if out.Linked {
			page = out.ReturnTo
		}
		http.Redirect(w, r, page+"?sso="+reason, http.StatusFound)
		return nil
	}
	if out.Linked {
		http.Redirect(w, r, out.ReturnTo+"?sso=linked", http.StatusFound)
		return nil
	}
	setSessionCookie(w, r, out.Token)
	http.Redirect(w, r, out.ReturnTo, http.StatusFound)
	return nil
}

// ssoReason names what went wrong for the page the browser lands on.
func ssoReason(err error) string {
	switch {
	case errors.Is(err, sso.ErrNotConfigured):
		return "off"
	case errors.Is(err, sso.ErrBadState):
		return "expired"
	case errors.Is(err, sso.ErrRefused):
		return "cancelled"
	case errors.Is(err, auth.ErrNoAccount):
		return "no-account"
	case errors.Is(err, auth.ErrNoRole):
		return "no-role"
	case errors.Is(err, auth.ErrDisabled):
		return "disabled"
	case errors.Is(err, auth.ErrIdentityTaken):
		return "taken"
	}
	return "failed"
}

func (s *Server) listIdentities(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Auth.Identities(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := identityList{Items: make([]linkedIdentity, len(rows))}
	for i, row := range rows {
		out.Items[i] = linkedIdentity{ID: row.ID, Issuer: row.Issuer, Email: row.Email, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) deleteIdentity(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "identityId", "identity")
	if err != nil {
		return err
	}
	err = s.Auth.UnlinkIdentity(r.Context(), UserFrom(r.Context()).ID, id)
	switch {
	case errors.Is(err, auth.ErrNoIdentity):
		return ErrNotFound("There is no such identity.")
	case errors.Is(err, auth.ErrLastSignIn):
		return ErrConflict("This is the only way left to sign in to your account. Set a password first.")
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// callbackURL is the callback's address as the browser reaches this
// server, the same at the start and the end of a sign-in.
func callbackURL(r *http.Request) string {
	scheme := "http"
	if overTLS(r) {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: r.Host, Path: APIPrefix + callbackPath}).String()
}

// appPath is a path of the web app to send someone to, or "/": never
// another site, which "//host" or "/\host" would be to a browser, nor the
// API.
func appPath(p string) string {
	if len(p) > 512 || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") ||
		strings.HasPrefix(p, APIPrefix) || strings.ContainsAny(p, "\r\n") {
		return "/"
	}
	return p
}
