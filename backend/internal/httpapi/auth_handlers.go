package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

// SessionCookie holds the session token. The name is fixed: a deployment has
// one GOtome per origin.
const SessionCookie = "gotome_session"

// cookieLifetime is as long as browsers keep a cookie. The session's real end
// is decided by the server, which extends it while it is used; a cookie that
// ran out first would sign people out in the middle of that.
const cookieLifetime = 400 * 24 * time.Hour

// Failed logins allowed per user name and address, and per address alone,
// before further attempts are refused for the rest of the window.
const (
	loginWindow         = 5 * time.Minute
	loginFailuresPerKey = 5
	loginFailuresPerIP  = 30
)

// LoginLimits is the pair of limiters the login route consults.
type LoginLimits struct {
	perAccount *auth.Limiter
	perAddress *auth.Limiter
}

// NewLoginLimits returns limiters on the given clock.
func NewLoginLimits(now func() time.Time) *LoginLimits {
	return &LoginLimits{
		perAccount: auth.NewLimiter(loginFailuresPerKey, loginWindow, now),
		perAddress: auth.NewLimiter(loginFailuresPerIP, loginWindow, now),
	}
}

type setupStatus struct {
	// Needed is true while no account exists and the first one may be created.
	Needed bool `json:"needed"`
}

type setupRequest struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	Email    *string `json:"email,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) error {
	needed, err := s.Auth.SetupNeeded(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, setupStatus{Needed: needed})
	return nil
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) error {
	var req setupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	user, err := s.Auth.Setup(r.Context(), req.Username, req.Password, req.Email)
	var invalid *auth.ValidationError
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, auth.ErrSetupDone):
		return ErrConflict("GOtome is already set up. Sign in instead.")
	case errors.Is(err, auth.ErrTaken):
		return ErrConflict("That user name or e-mail address is already in use.")
	case err != nil:
		return err
	}
	token, _, err := s.Auth.OpenSession(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		return err
	}
	setSessionCookie(w, r, token)
	writeJSON(w, r, http.StatusCreated, user)
	return nil
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	address := clientAddress(r)
	account := strings.ToLower(strings.TrimSpace(req.Username)) + "|" + address
	if wait := max(s.Logins.perAccount.RetryAfter(account), s.Logins.perAddress.RetryAfter(address)); wait > 0 {
		return ErrTooManyRequests("Too many failed sign-in attempts. Wait a little and try again.", wait)
	}

	user, token, _, err := s.Auth.Login(r.Context(), req.Username, req.Password, r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.Logins.perAccount.Fail(account)
		s.Logins.perAddress.Fail(address)
		return ErrUnauthorized("The user name or the password is wrong.")
	}
	if err != nil {
		return err
	}
	s.Logins.perAccount.Reset(account)
	setSessionCookie(w, r, token)
	writeJSON(w, r, http.StatusOK, user)
	return nil
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) error {
	if err := s.Auth.Logout(r.Context(), sessionToken(r)); err != nil {
		return err
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// currentUser is the signed-in user and what their role allows, so the web
// app can leave out what the server would refuse anyway.
type currentUser struct {
	auth.User
	Permissions []auth.Permission `json:"permissions"`
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) error {
	user := UserFrom(r.Context())
	writeJSON(w, r, http.StatusOK, currentUser{User: *user, Permissions: auth.Permissions(user.Role)})
	return nil
}

// UserFrom is the signed-in user of the request, or nil.
func UserFrom(ctx context.Context) *auth.User {
	user, _ := ctx.Value(userKey).(*auth.User)
	return user
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(cookieLifetime.Seconds()),
		HttpOnly: true,
		Secure:   overTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   overTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// overTLS reports whether the browser reached the server over HTTPS, directly
// or through a reverse proxy that says so. Home installations often run plain
// HTTP on a local network, where a Secure cookie would never be sent back.
func overTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// clientAddress is the address the connection came from. Behind a reverse
// proxy that is the proxy's, which makes the per-address limit one shared by
// everybody; the per-account limit still holds.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authenticate resolves the session cookie to a user for the handlers. A
// missing or dead session is not an error here: public routes are served
// either way, and requireUser refuses the rest.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := sessionToken(r)
		if token == "" || s.Auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.Auth.Authenticate(r.Context(), token)
		switch {
		case errors.Is(err, auth.ErrNoSession):
			// A cookie for a session that is gone would be sent forever.
			clearSessionCookie(w, r)
			next.ServeHTTP(w, r)
		case err != nil:
			writeError(w, r, err)
		default:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, &user)))
		}
	})
}

// require lets a request through when its caller holds the permission: 401
// when nobody is signed in, 403 when somebody is and their role does not
// reach that far.
func require(permission auth.Permission, next HandlerFunc) HandlerFunc {
	if permission == auth.Public {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) error {
		user := UserFrom(r.Context())
		if user == nil {
			return ErrUnauthorized("")
		}
		if permission != auth.SignedIn && !auth.Allows(user.Role, permission) {
			return ErrForbidden("")
		}
		return next(w, r)
	}
}

// sameOrigin refuses a state-changing request that a page on another site
// made the browser send. The session cookie is SameSite=Lax, which already
// keeps it off cross-site POSTs; this holds when a browser does not honour
// that, and for the routes that need no cookie, such as first-run setup.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				writeError(w, r, ErrForbidden("This request came from another site and was refused."))
				return
			}
		} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			// No Origin but a fetch-metadata header: the browser says where
			// the request came from, and it was not here.
			writeError(w, r, ErrForbidden("This request came from another site and was refused."))
			return
		}
		// Neither header: not a browser, so there is no ambient cookie to abuse.
		next.ServeHTTP(w, r)
	})
}
