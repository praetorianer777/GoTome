package httpapi

import (
	"net/http"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
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
	// Query is the zero value of a struct whose fields, tagged query:"name",
	// are the route's query parameters; enum:"a,b" lists the values one may
	// take. Handlers read them with decodeQuery into the same type.
	Query any
	// Request and Response are zero values of the JSON bodies; nil means the
	// route has none.
	Request  any
	Response any
	// Produces is the media type of a success that is not JSON, such as an
	// image. The handler writes it itself.
	Produces string
	// Status is what success answers; zero means 200, or 204 without a Response.
	Status int
	// Permission is what the caller must hold: auth.Public, auth.SignedIn, or
	// one of the permissions a role is made of. Every route declares one; the
	// router refuses to start with a route that does not.
	Permission auth.Permission
	Handler    HandlerFunc
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
			Response: buildInfo{}, Permission: auth.Public, Handler: s.getVersion,
		},
		{
			Method: http.MethodGet, Path: "/setup", ID: "getSetup",
			Summary: "Whether the first account still has to be created", Tag: "auth",
			Response: setupStatus{}, Permission: auth.Public, Handler: s.getSetup,
		},
		{
			Method: http.MethodPost, Path: "/setup", ID: "completeSetup",
			Summary: "Create the first account, an administrator, and sign in as it", Tag: "auth",
			Request: setupRequest{}, Response: auth.User{}, Status: http.StatusCreated,
			Permission: auth.Public, Handler: s.postSetup,
		},
		{
			Method: http.MethodPost, Path: "/auth/login", ID: "login",
			Summary: "Sign in with user name and password", Tag: "auth",
			Request: loginRequest{}, Response: auth.User{}, Permission: auth.Public, Handler: s.postLogin,
		},
		{
			Method: http.MethodPost, Path: "/auth/logout", ID: "logout",
			Summary: "End the session", Tag: "auth",
			Permission: auth.Public, Handler: s.postLogout,
		},
		{
			Method: http.MethodGet, Path: "/auth/me", ID: "getCurrentUser",
			Summary: "Who is signed in", Tag: "auth",
			Response: currentUser{}, Permission: auth.SignedIn, Handler: s.getMe,
		},

		{
			Method: http.MethodGet, Path: "/libraries", ID: "listLibraries",
			Summary: "The libraries the caller may see", Tag: "libraries",
			Response: libraryList{}, Permission: auth.LibraryRead, Handler: s.listLibraries,
		},
		{
			Method: http.MethodPost, Path: "/libraries", ID: "createLibrary",
			Summary: "Add a library", Tag: "libraries",
			Request: createLibraryRequest{}, Response: libraryResponse{}, Status: http.StatusCreated,
			Permission: auth.StorageManage, Handler: s.createLibrary,
		},
		{
			Method: http.MethodGet, Path: "/libraries/{libraryId}", ID: "getLibrary",
			Summary: "One library, if the caller may see it", Tag: "libraries",
			Response: libraryResponse{}, Permission: auth.LibraryRead, Handler: s.getLibrary,
		},
		{
			Method: http.MethodPatch, Path: "/libraries/{libraryId}", ID: "updateLibrary",
			Summary: "Change a library's name, visibility or whether GOtome may write to it", Tag: "libraries",
			Request: updateLibraryRequest{}, Response: libraryResponse{},
			Permission: auth.StorageManage, Handler: s.updateLibrary,
		},
		{
			Method: http.MethodDelete, Path: "/libraries/{libraryId}", ID: "deleteLibrary",
			Summary: "Remove a library from GOtome; its folder and files stay", Tag: "libraries",
			Permission: auth.StorageManage, Handler: s.deleteLibrary,
		},
		{
			Method: http.MethodPost, Path: "/libraries/{libraryId}/scans", ID: "scanLibrary",
			Summary: "Look through a library's folder for new, changed and missing files", Tag: "libraries",
			Response: ingest.Scan{}, Status: http.StatusAccepted,
			Permission: auth.IndexRebuild, Handler: s.scanLibrary,
		},
		{
			Method: http.MethodGet, Path: "/libraries/{libraryId}/members", ID: "listLibraryMembers",
			Summary: "Who may see a private library besides its owner", Tag: "libraries",
			Response: memberList{}, Permission: auth.StorageManage, Handler: s.listLibraryMembers,
		},
		{
			Method: http.MethodPut, Path: "/libraries/{libraryId}/members/{userId}", ID: "addLibraryMember",
			Summary: "Let a user see a private library", Tag: "libraries",
			Permission: auth.StorageManage, Handler: s.putLibraryMember,
		},
		{
			Method: http.MethodDelete, Path: "/libraries/{libraryId}/members/{userId}", ID: "removeLibraryMember",
			Summary: "Take a private library away from a user again", Tag: "libraries",
			Permission: auth.StorageManage, Handler: s.deleteLibraryMember,
		},

		{
			Method: http.MethodGet, Path: "/books", ID: "listBooks",
			Summary: "One page of the books the caller may see, in one library or all", Tag: "books",
			Query: listBooksQuery{}, Response: bookList{}, Permission: auth.LibraryRead, Handler: s.listBooks,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}", ID: "getBook",
			Summary: "One book with everything that describes it and its files", Tag: "books",
			Response: bookDetail{}, Permission: auth.LibraryRead, Handler: s.getBook,
		},
		{
			Method: http.MethodGet, Path: "/files/{fileId}/download", ID: "downloadFile",
			Summary: "A book's file as it lies on disk; answers range requests", Tag: "books",
			Produces: "application/octet-stream", Status: http.StatusOK,
			Permission: auth.LibraryRead, Handler: s.downloadFile,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/covers/{size}", ID: "getBookCover",
			Summary: "A book's cover as a JPEG; size is small or large", Tag: "books",
			Produces: "image/jpeg", Status: http.StatusOK,
			Permission: auth.LibraryRead, Handler: s.getBookCover,
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
