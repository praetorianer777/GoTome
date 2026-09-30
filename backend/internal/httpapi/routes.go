package httpapi

import (
	"net/http"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

// APIPrefix is where every endpoint but the probe lives.
const APIPrefix = "/api/v1"

// HandlerFunc is a handler that reports failure by returning it; the router
// writes the error envelope.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Route is one endpoint. The router and the OpenAPI document are both built
// from the same table of these, so neither can describe a route the other
// does not have.
type Route struct {
	Method string
	// Path is relative to APIPrefix, with parameters in braces: /books/{id}.
	Path string
	// ID is the operationId, which names the function in generated clients.
	ID      string
	Summary string
	Tag     string
	// Request and Response are zero values of the JSON bodies; nil means the
	// route has none.
	Request  any
	Response any
	// Status is what success answers; zero means 200, or 204 without a Response.
	Status int
	// Public routes answer without a session. Every other route refuses a
	// request nobody is signed in for.
	Public  bool
	Handler HandlerFunc
}

func (rt Route) successStatus() int {
	switch {
	case rt.Status != 0:
		return rt.Status
	case rt.Response == nil:
		return http.StatusNoContent
	default:
		return http.StatusOK
	}
}

// routes is the table of every API endpoint.
func (s *Server) routes() []Route {
	return []Route{
		{
			Method: http.MethodGet, Path: "/version", ID: "getVersion",
			Summary: "Which build of GOtome is running", Tag: "system",
			Response: buildInfo{}, Public: true, Handler: s.getVersion,
		},
		{
			Method: http.MethodGet, Path: "/setup", ID: "getSetup",
			Summary: "Whether the first account still has to be created", Tag: "auth",
			Response: setupStatus{}, Public: true, Handler: s.getSetup,
		},
		{
			Method: http.MethodPost, Path: "/setup", ID: "completeSetup",
			Summary: "Create the first account, an administrator, and sign in as it", Tag: "auth",
			Request: setupRequest{}, Response: auth.User{}, Status: http.StatusCreated,
			Public: true, Handler: s.postSetup,
		},
		{
			Method: http.MethodPost, Path: "/auth/login", ID: "login",
			Summary: "Sign in with user name and password", Tag: "auth",
			Request: loginRequest{}, Response: auth.User{}, Public: true, Handler: s.postLogin,
		},
		{
			Method: http.MethodPost, Path: "/auth/logout", ID: "logout",
			Summary: "End the session", Tag: "auth",
			Public: true, Handler: s.postLogout,
		},
		{
			Method: http.MethodGet, Path: "/auth/me", ID: "getCurrentUser",
			Summary: "Who is signed in", Tag: "auth",
			Response: auth.User{}, Handler: s.getMe,
		},
	}
}

// buildInfo is version.Info under the name clients see it by: "Info" alone
// would say nothing in a generated client.
type buildInfo version.Info

func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, http.StatusOK, buildInfo(version.Current()))
	return nil
}
