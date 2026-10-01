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
	// Consumes is the media type of a request body that is not JSON, such as
	// a multipart form; Request then describes the form's fields. The
	// handler reads the body itself.
	Consumes string
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
			Method: http.MethodPost, Path: "/libraries/{libraryId}/uploads", ID: "uploadFile",
			Summary: "Store one book file in a managed library, or point to the book that already has it", Tag: "libraries",
			Consumes: "multipart/form-data", Request: uploadForm{}, Response: ingest.Uploaded{},
			Permission: auth.BooksUpload, Handler: s.uploadFile,
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
			Method: http.MethodGet, Path: "/auth/sessions", ID: "listOwnSessions",
			Summary: "The caller's own live sessions, the one in use marked", Tag: "auth",
			Response: sessionList{}, Permission: auth.SignedIn, Handler: s.listOwnSessions,
		},
		{
			Method: http.MethodDelete, Path: "/auth/sessions/{sessionId}", ID: "endOwnSession",
			Summary: "End one of the caller's own sessions", Tag: "auth",
			Permission: auth.SignedIn, Handler: s.endOwnSession,
		},
		{
			Method: http.MethodPost, Path: "/auth/password", ID: "changePassword",
			Summary: "Change one's own password; every other session ends", Tag: "auth",
			Request: changePasswordRequest{}, Permission: auth.SignedIn, Handler: s.changePassword,
		},

		{
			Method: http.MethodGet, Path: "/users", ID: "listUsers",
			Summary: "Every account, with when it was last used", Tag: "users",
			Response: accountList{}, Permission: auth.UsersManage, Handler: s.listUsers,
		},
		{
			Method: http.MethodPost, Path: "/users", ID: "createUser",
			Summary: "Add an account with a password", Tag: "users",
			Request: createAccountRequest{}, Response: account{}, Status: http.StatusCreated,
			Permission: auth.UsersManage, Handler: s.createUser,
		},
		{
			Method: http.MethodPatch, Path: "/users/{userId}", ID: "updateUser",
			Summary: "Change an account's role, address or password, or disable it; the last administrator stays one", Tag: "users",
			Request: updateAccountRequest{}, Response: account{}, Permission: auth.UsersManage, Handler: s.updateUser,
		},
		{
			Method: http.MethodDelete, Path: "/users/{userId}/sessions", ID: "endUserSessions",
			Summary: "Sign an account out everywhere", Tag: "users",
			Permission: auth.UsersManage, Handler: s.endUserSessions,
		},

		{
			Method: http.MethodGet, Path: "/settings", ID: "listSettings",
			Summary: "Every setting; a secret says only whether it is set", Tag: "settings",
			Response: settingList{}, Permission: auth.SettingsManage, Handler: s.listSettings,
		},
		{
			Method: http.MethodPatch, Path: "/settings", ID: "updateSettings",
			Summary: "Change settings, all or none; an empty or null value unsets one", Tag: "settings",
			Request: updateSettingsRequest{}, Response: settingList{}, Permission: auth.SettingsManage, Handler: s.updateSettings,
		},

		{
			Method: http.MethodGet, Path: "/books", ID: "listBooks",
			Summary: "One page of the books the caller may see, in one library or all", Tag: "books",
			Query: listBooksQuery{}, Response: bookList{}, Permission: auth.LibraryRead, Handler: s.listBooks,
		},
		{
			Method: http.MethodGet, Path: "/books/search", ID: "searchBooks",
			Summary: "Books whose title, author or series looks like the words, the best first", Tag: "books",
			Query: searchQuery{}, Response: searchResult{}, Permission: auth.LibraryRead, Handler: s.searchBooks,
		},
		{
			Method: http.MethodGet, Path: "/books/facets", ID: "listBookFacets",
			Summary: "How many books of a list have each author, series, tag, language, decade and format", Tag: "books",
			Query: facetsQuery{}, Response: facetList{}, Permission: auth.LibraryRead, Handler: s.listFacets,
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
