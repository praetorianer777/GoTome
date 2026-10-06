package httpapi

import (
	"net/http"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/search"
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
	// Reads says what a GET route answers with; the router refuses to
	// start with one that does not say.
	Reads   Reads
	Handler HandlerFunc
}

// Reads is what a route answers with, as far as libraries are concerned.
type Reads int

const (
	readsUnset Reads = iota
	// ReadsLibraries answers with something that belongs to a library, or
	// tells of it: a book, a file, a count, a shelf. It must filter through
	// visible_library_ids, and an integration test calls every such route as
	// someone outside a private library to prove that it does.
	ReadsLibraries
	// ReadsNoLibrary answers with nothing of any library: the build, the
	// caller's own account, the settings, a provider's answer.
	ReadsNoLibrary
)

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

// Table is every API endpoint, as the router mounts them.
func (s *Server) Table() []Route { return s.routes() }

// routes is the table of every API endpoint.
func (s *Server) routes() []Route {
	return []Route{
		{
			Method: http.MethodGet, Path: "/version", ID: "getVersion",
			Summary: "Which build of GOtome is running", Tag: "system",
			Response: buildInfo{}, Reads: ReadsNoLibrary, Permission: auth.Public, Handler: s.getVersion,
		},
		{
			Method: http.MethodGet, Path: "/setup", ID: "getSetup",
			Summary: "Whether the first account still has to be created", Tag: "auth",
			Response: setupStatus{}, Reads: ReadsNoLibrary, Permission: auth.Public, Handler: s.getSetup,
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
			Response: currentUser{}, Reads: ReadsNoLibrary, Permission: auth.SignedIn, Handler: s.getMe,
		},

		{
			Method: http.MethodGet, Path: "/libraries", ID: "listLibraries",
			Summary: "The libraries the caller may see", Tag: "libraries",
			Response: libraryList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listLibraries,
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
			Response: libraryResponse{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getLibrary,
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
			Response: memberList{}, Reads: ReadsLibraries, Permission: auth.StorageManage, Handler: s.listLibraryMembers,
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
			Response: sessionList{}, Reads: ReadsNoLibrary, Permission: auth.SignedIn, Handler: s.listOwnSessions,
		},
		{
			Method: http.MethodDelete, Path: "/auth/sessions/{sessionId}", ID: "endOwnSession",
			Summary: "End one of the caller's own sessions", Tag: "auth",
			Permission: auth.SignedIn, Handler: s.endOwnSession,
		},
		{
			Method: http.MethodGet, Path: "/auth/storage", ID: "getOwnStorage",
			Summary: "What the caller's uploads take up, and how much they may", Tag: "auth",
			Response: storage{}, Reads: ReadsNoLibrary, Permission: auth.SignedIn, Handler: s.getOwnStorage,
		},
		{
			Method: http.MethodPost, Path: "/auth/password", ID: "changePassword",
			Summary: "Change one's own password; every other session ends", Tag: "auth",
			Request: changePasswordRequest{}, Permission: auth.SignedIn, Handler: s.changePassword,
		},

		{
			Method: http.MethodGet, Path: "/users", ID: "listUsers",
			Summary: "Every account, with when it was last used", Tag: "users",
			Response: accountList{}, Reads: ReadsNoLibrary, Permission: auth.UsersManage, Handler: s.listUsers,
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
			Method: http.MethodPut, Path: "/users/{userId}/quota", ID: "setUserQuota",
			Summary: "Set how much an account may upload; null is no limit", Tag: "users",
			Request: setQuotaRequest{}, Response: account{}, Permission: auth.StorageManage, Handler: s.setQuota,
		},
		{
			Method: http.MethodDelete, Path: "/users/{userId}/sessions", ID: "endUserSessions",
			Summary: "Sign an account out everywhere", Tag: "users",
			Permission: auth.UsersManage, Handler: s.endUserSessions,
		},

		{
			Method: http.MethodGet, Path: "/jobs", ID: "listJobs",
			Summary: "The newest background jobs, with what each works on", Tag: "jobs",
			Query: listJobsQuery{}, Response: jobList{}, Reads: ReadsLibraries, Permission: auth.IndexRebuild, Handler: s.listJobs,
		},
		{
			Method: http.MethodPost, Path: "/jobs/{jobId}/retry", ID: "retryJob",
			Summary: "Run a job that is not running again, as soon as a worker is free", Tag: "jobs",
			Response: jobView{}, Permission: auth.IndexRebuild, Handler: s.retryJob,
		},
		{
			Method: http.MethodPost, Path: "/jobs/{jobId}/cancel", ID: "cancelJob",
			Summary: "Stop a job that waits or runs", Tag: "jobs",
			Response: jobView{}, Permission: auth.IndexRebuild, Handler: s.cancelJob,
		},
		{
			Method: http.MethodPost, Path: "/files/{fileId}/extraction", ID: "rereadFile",
			Summary: "Read a file again for its details, cover and text", Tag: "jobs",
			Response: rereadResult{}, Status: http.StatusAccepted, Permission: auth.IndexRebuild, Handler: s.rereadFile,
		},
		{
			Method: http.MethodGet, Path: "/trash", ID: "listTrash",
			Summary: "The trashed files of the libraries the caller sees, the latest first", Tag: "files",
			Query: trashQuery{}, Response: trashList{}, Reads: ReadsLibraries, Permission: auth.MetadataEdit, Handler: s.listTrash,
		},
		{
			Method: http.MethodPost, Path: "/files/{fileId}/trash", ID: "trashFile",
			Summary: "Move a file of a writable library into its trash; it can be restored until the trash is purged", Tag: "files",
			Status: http.StatusNoContent, Permission: auth.MetadataEdit, Handler: s.trashFile,
		},
		{
			Method: http.MethodPost, Path: "/files/{fileId}/restore", ID: "restoreFile",
			Summary: "Bring a trashed file back to where it was and to its book", Tag: "files",
			Status: http.StatusNoContent, Permission: auth.MetadataEdit, Handler: s.restoreFile,
		},
		{
			Method: http.MethodDelete, Path: "/files/{fileId}", ID: "purgeFile",
			Summary: "Delete a trashed file for good, before its time is up", Tag: "files",
			Status: http.StatusNoContent, Permission: auth.StorageManage, Handler: s.purgeFile,
		},
		{
			Method: http.MethodPost, Path: "/libraries/{libraryId}/extractions", ID: "rereadLibrary",
			Summary: "Read a library's files again: those that failed, or all", Tag: "jobs",
			Request: rereadLibraryRequest{}, Response: rereadResult{}, Status: http.StatusAccepted,
			Permission: auth.IndexRebuild, Handler: s.rereadLibrary,
		},

		{
			Method: http.MethodGet, Path: "/settings", ID: "listSettings",
			Summary: "Every setting; a secret says only whether it is set", Tag: "settings",
			Response: settingList{}, Reads: ReadsNoLibrary, Permission: auth.SettingsManage, Handler: s.listSettings,
		},
		{
			Method: http.MethodPatch, Path: "/settings", ID: "updateSettings",
			Summary: "Change settings, all or none; an empty or null value unsets one", Tag: "settings",
			Request: updateSettingsRequest{}, Response: settingList{}, Permission: auth.SettingsManage, Handler: s.updateSettings,
		},

		{
			Method: http.MethodGet, Path: "/books", ID: "listBooks",
			Summary: "One page of the books the caller may see, in one library or all", Tag: "books",
			Query: listBooksQuery{}, Response: bookList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listBooks,
		},
		{
			Method: http.MethodGet, Path: "/books/search", ID: "searchBooks",
			Summary: "Books whose title, author or series looks like the words, the best first", Tag: "books",
			Query: searchQuery{}, Response: searchResult{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.searchBooks,
		},
		{
			Method: http.MethodGet, Path: "/search", ID: "searchText",
			Summary: "Books whose text holds the words, best first, with the passages that do", Tag: "books",
			Query: fullTextQuery{}, Response: fullTextResult{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.searchText,
		},
		{
			Method: http.MethodGet, Path: "/search/status", ID: "searchStatus",
			Summary: "How much of the books' text search knows, and whether its index is being rebuilt", Tag: "books",
			Query: searchStatusQuery{}, Response: search.IndexStatus{}, Reads: ReadsLibraries, Permission: auth.IndexRebuild, Handler: s.searchStatus,
		},
		{
			Method: http.MethodPost, Path: "/search/reread", ID: "rereadText",
			Summary: "Read the text of a book, a library or every visible book again; search keeps the old text until then", Tag: "books",
			Request: textRereadRequest{}, Response: textRereadResult{}, Status: http.StatusAccepted,
			Permission: auth.IndexRebuild, Handler: s.rereadText,
		},
		{
			Method: http.MethodPost, Path: "/search/rebuild", ID: "rebuildSearchIndex",
			Summary: "Build the search index again from the text already read; searches use the old one until then", Tag: "books",
			Response: rebuildResult{}, Status: http.StatusAccepted,
			Permission: auth.IndexRebuild, Handler: s.rebuildSearchIndex,
		},
		{
			Method: http.MethodGet, Path: "/duplicates", ID: "listDuplicates",
			Summary: "Pairs of books that look like one, both of which the caller sees, with why", Tag: "books",
			Query: duplicatesQuery{}, Response: duplicateList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listDuplicates,
		},
		{
			Method: http.MethodPut, Path: "/duplicates/{pairId}/state", ID: "setDuplicateState",
			Summary: "Keep both books of a pair, or open it again", Tag: "books",
			Request: pairStateRequest{}, Status: http.StatusNoContent,
			Permission: auth.MetadataEdit, Handler: s.setPairState,
		},
		{
			Method: http.MethodPost, Path: "/duplicates/{pairId}/replace", ID: "replaceDuplicate",
			Summary: "Keep one book of a pair: the other's files go to the trash and the book into the one kept", Tag: "books",
			Request: replaceRequest{}, Status: http.StatusNoContent,
			Permission: auth.MetadataEdit, Handler: s.replaceDuplicate,
		},
		{
			Method: http.MethodPost, Path: "/duplicates/checks", ID: "checkDuplicates",
			Summary: "Look for duplicates among the books of a library, or of every library the caller sees", Tag: "books",
			Request: duplicateCheckRequest{}, Response: duplicateCheckResult{}, Status: http.StatusAccepted,
			Permission: auth.MetadataEdit, Handler: s.checkDuplicates,
		},
		{
			Method: http.MethodPost, Path: "/books/{bookId}/merge", ID: "mergeBooks",
			Summary: "Merge another book of the same library into this one, with its files, details and everyone's reading", Tag: "books",
			Request: mergeRequest{}, Response: bookDetail{}, Permission: auth.MetadataEdit, Handler: s.mergeBooks,
		},
		{
			Method: http.MethodGet, Path: "/books/facets", ID: "listBookFacets",
			Summary: "How many books of a list have each author, series, tag, language, decade and format", Tag: "books",
			Query: facetsQuery{}, Response: facetList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listFacets,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}", ID: "getBook",
			Summary: "One book with everything that describes it and its files", Tag: "books",
			Response: bookDetail{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getBook,
		},
		{
			Method: http.MethodPatch, Path: "/books/{bookId}", ID: "editBook",
			Summary: "Change how a book is described, and lock what was changed", Tag: "books",
			Request: editBookRequest{}, Response: bookDetail{}, Permission: auth.MetadataEdit, Handler: s.editBook,
		},
		{
			Method: http.MethodPut, Path: "/books/{bookId}/cover", ID: "putBookCover",
			Summary: "Give a book another cover, and lock it", Tag: "books",
			Request: coverForm{}, Consumes: "multipart/form-data", Response: bookDetail{},
			Permission: auth.MetadataEdit, Handler: s.putBookCover,
		},
		{
			Method: http.MethodDelete, Path: "/books/{bookId}/cover", ID: "deleteBookCover",
			Summary: "Take a book's cover away, and lock it so", Tag: "books",
			Response: bookDetail{}, Permission: auth.MetadataEdit, Handler: s.deleteBookCover,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/candidates", ID: "listCandidates",
			Summary: "What the metadata providers know about a book, the best fit first", Tag: "books",
			Response: candidateList{}, Reads: ReadsLibraries, Permission: auth.MetadataEdit, Handler: s.listCandidates,
		},
		{
			Method: http.MethodPost, Path: "/books/{bookId}/candidates/apply", ID: "applyCandidate",
			Summary: "Take chosen values from a provider's record; locked fields stay", Tag: "books",
			Request: applyCandidateRequest{}, Response: bookDetail{}, Permission: auth.MetadataEdit, Handler: s.applyCandidate,
		},
		{
			Method: http.MethodPut, Path: "/books/{bookId}/reading", ID: "setReading",
			Summary: "Change where the caller stands with a book: status, rating, dates", Tag: "books",
			Request: readingChange{}, Response: readingState{}, Permission: auth.PersonalManage, Handler: s.setReading,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/audio", ID: "getAudio",
			Summary: "A book's audio as one recording: its parts in order and the chapters across them", Tag: "books",
			Response: audioTimeline{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getAudio,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/similar", ID: "listSimilarBooks",
			Summary: "The books most like one by what they are about, the nearest first; its copies and related books left out", Tag: "books",
			Query: similarQuery{}, Response: similarList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listSimilar,
		},
		{
			Method: http.MethodGet, Path: "/metadata/search", ID: "searchMetadata",
			Summary: "Books the metadata providers know by title, author or ISBN, to wish for", Tag: "books",
			Query: metadataSearchQuery{}, Response: candidateList{}, Reads: ReadsNoLibrary, Permission: auth.PersonalManage, Handler: s.searchMetadata,
		},
		{
			Method: http.MethodPost, Path: "/books/wishes", ID: "createWish",
			Summary: "Wish for a book the library does not hold: a placeholder from a provider's record", Tag: "books",
			Request: wishRequest{}, Response: bookDetail{}, Status: http.StatusCreated,
			Permission: auth.PersonalManage, Handler: s.createWish,
		},
		{
			Method: http.MethodGet, Path: "/me/stats", ID: "getStats",
			Summary: "What the caller read and listened to: per day, in total, per year, and when", Tag: "books",
			Query: statsQuery{}, Response: readingStats{}, Reads: ReadsLibraries, Permission: auth.PersonalManage, Handler: s.getStats,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/progress", ID: "getProgress",
			Summary: "Where the caller is in a book, in its text and in its audio", Tag: "books",
			Response: progressState{}, Reads: ReadsLibraries, Permission: auth.PersonalManage, Handler: s.getProgress,
		},
		{
			Method: http.MethodPut, Path: "/books/{bookId}/progress/{medium}", ID: "putProgress",
			Summary: "Record where the caller is in a book; medium is ebook or audio", Tag: "books",
			Request: progressUpdate{}, Response: progressSaved{}, Permission: auth.PersonalManage, Handler: s.putProgress,
		},
		{
			Method: http.MethodGet, Path: "/collections", ID: "listCollections",
			Summary: "The caller's own collections and everyone's shared ones", Tag: "collections",
			Query: collectionsQuery{}, Response: collectionList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listCollections,
		},
		{
			Method: http.MethodPost, Path: "/collections", ID: "createCollection",
			Summary: "Make a collection, private unless it is said to be shared", Tag: "collections",
			Request: collectionRequest{}, Response: collectionDetail{}, Status: http.StatusCreated,
			Permission: auth.PersonalManage, Handler: s.createCollection,
		},
		{
			Method: http.MethodGet, Path: "/collections/{collectionId}", ID: "getCollection",
			Summary: "A collection and, in its order, the books of it the caller may see", Tag: "collections",
			Response: collectionDetail{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getCollection,
		},
		{
			Method: http.MethodPut, Path: "/collections/{collectionId}", ID: "updateCollection",
			Summary: "Rename a collection of the caller's, or change who may look at it", Tag: "collections",
			Request: collectionRequest{}, Response: collectionDetail{}, Permission: auth.PersonalManage, Handler: s.updateCollection,
		},
		{
			Method: http.MethodDelete, Path: "/collections/{collectionId}", ID: "deleteCollection",
			Summary: "Delete a collection of the caller's; its books stay", Tag: "collections",
			Permission: auth.PersonalManage, Handler: s.deleteCollection,
		},
		{
			Method: http.MethodPost, Path: "/collections/{collectionId}/books", ID: "addToCollection",
			Summary: "Put books at the end of a collection of the caller's", Tag: "collections",
			Request: collectionAddRequest{}, Response: collectionAddResult{}, Permission: auth.PersonalManage, Handler: s.addToCollection,
		},
		{
			Method: http.MethodDelete, Path: "/collections/{collectionId}/books/{bookId}", ID: "removeFromCollection",
			Summary: "Take a book out of a collection of the caller's", Tag: "collections",
			Permission: auth.PersonalManage, Handler: s.removeFromCollection,
		},
		{
			Method: http.MethodPut, Path: "/collections/{collectionId}/order", ID: "reorderCollection",
			Summary: "Put a collection's books in another order", Tag: "collections",
			Request: collectionOrder{}, Response: collectionDetail{}, Permission: auth.PersonalManage, Handler: s.reorderCollection,
		},
		{
			Method: http.MethodGet, Path: "/books/count", ID: "countBooks",
			Summary: "How many books the caller may see match a filter", Tag: "books",
			Query: countBooksQuery{}, Response: bookCount{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.countBooks,
		},
		{
			Method: http.MethodGet, Path: "/notifications", ID: "listNotifications",
			Summary: "The caller's notifications, newest first, and how many are unread", Tag: "notifications",
			Query: notificationsQuery{}, Response: notificationList{}, Reads: ReadsLibraries, Permission: auth.SignedIn, Handler: s.listNotifications,
		},
		{
			Method: http.MethodPost, Path: "/notifications/read", ID: "markNotificationsRead",
			Summary: "Mark the caller's notifications read: those named, or all", Tag: "notifications",
			Request: notificationsRead{}, Response: unreadCount{}, Permission: auth.SignedIn, Handler: s.markNotificationsRead,
		},
		{
			Method: http.MethodGet, Path: "/me/notification-settings", ID: "getNotificationSettings",
			Summary: "The kinds of event the caller may be told of, and whether they are", Tag: "notifications",
			Response: notificationSettings{}, Reads: ReadsNoLibrary, Permission: auth.SignedIn, Handler: s.getNotificationSettings,
		},
		{
			Method: http.MethodPut, Path: "/me/notification-settings", ID: "putNotificationSettings",
			Summary: "Choose which kinds of event the caller is told of; kinds left out stay as they are", Tag: "notifications",
			Request: notificationSettings{}, Response: notificationSettings{}, Permission: auth.SignedIn, Handler: s.putNotificationSettings,
		},
		{
			Method: http.MethodGet, Path: "/notifications/stream", ID: "streamNotifications",
			Summary: "Server-sent events: the caller's unread count, now and whenever it may have changed", Tag: "notifications",
			Produces: "text/event-stream", Reads: ReadsLibraries, Permission: auth.SignedIn, Handler: s.streamNotifications,
		},
		{
			Method: http.MethodGet, Path: "/smart-shelves", ID: "listSmartShelves",
			Summary: "The caller's own smart shelves and everyone's shared ones, with what each holds for the caller", Tag: "collections",
			Response: smartShelfList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listSmartShelves,
		},
		{
			Method: http.MethodPost, Path: "/smart-shelves", ID: "createSmartShelf",
			Summary: "Make a smart shelf from a rule tree, private unless it is said to be shared", Tag: "collections",
			Request: smartShelfRequest{}, Response: smartShelf{}, Status: http.StatusCreated,
			Permission: auth.PersonalManage, Handler: s.createSmartShelf,
		},
		{
			Method: http.MethodGet, Path: "/smart-shelves/{shelfId}", ID: "getSmartShelf",
			Summary: "A smart shelf, with how many books it holds for the caller", Tag: "collections",
			Response: smartShelf{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getSmartShelf,
		},
		{
			Method: http.MethodPut, Path: "/smart-shelves/{shelfId}", ID: "updateSmartShelf",
			Summary: "Change a smart shelf of the caller's: its name, rules or who may look at it", Tag: "collections",
			Request: smartShelfRequest{}, Response: smartShelf{}, Permission: auth.PersonalManage, Handler: s.updateSmartShelf,
		},
		{
			Method: http.MethodDelete, Path: "/smart-shelves/{shelfId}", ID: "deleteSmartShelf",
			Summary: "Delete a smart shelf of the caller's", Tag: "collections",
			Permission: auth.PersonalManage, Handler: s.deleteSmartShelf,
		},
		{
			Method: http.MethodGet, Path: "/smart-shelves/{shelfId}/books", ID: "listSmartShelfBooks",
			Summary: "One page of the books on a smart shelf, as they match for the caller now", Tag: "collections",
			Query: smartBooksQuery{}, Response: bookList{}, Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.listSmartShelfBooks,
		},
		{
			Method: http.MethodPost, Path: "/books/reading", ID: "setReadingBulk",
			Summary: "Change where the caller stands with many books at once", Tag: "books",
			Request: readingBulkRequest{}, Response: readingBulkResult{}, Permission: auth.PersonalManage, Handler: s.setReadingBulk,
		},
		{
			Method: http.MethodPost, Path: "/books/bulk", ID: "startBulk",
			Summary: "Edit, look up or write into their files many books at once, as a job", Tag: "books",
			Request: bulkRequest{}, Response: bulkStarted{}, Status: http.StatusAccepted,
			Permission: auth.MetadataEdit, Handler: s.startBulk,
		},
		{
			Method: http.MethodGet, Path: "/bulk/{bulkId}", ID: "getBulk",
			Summary: "A bulk change the caller asked for, and what it came to for each book so far", Tag: "books",
			Response: bulkStatus{}, Reads: ReadsLibraries, Permission: auth.MetadataEdit, Handler: s.getBulk,
		},
		{
			Method: http.MethodGet, Path: "/matches", ID: "listReview",
			Summary: "Books whose doubtful matches wait for a person, those that wait longest first", Tag: "books",
			Query: reviewQuery{}, Response: reviewList{}, Reads: ReadsLibraries, Permission: auth.MetadataEdit, Handler: s.listReview,
		},
		{
			Method: http.MethodPost, Path: "/matches/{matchId}/accept", ID: "acceptMatch",
			Summary: "Take chosen values from a waiting match; the book's other matches are settled", Tag: "books",
			Request: acceptMatchRequest{}, Response: bookDetail{}, Permission: auth.MetadataEdit, Handler: s.acceptMatch,
		},
		{
			Method: http.MethodPost, Path: "/matches/{matchId}/reject", ID: "rejectMatch",
			Summary: "Reject a waiting match; it is not proposed again", Tag: "books",
			Permission: auth.MetadataEdit, Handler: s.rejectMatch,
		},
		{
			Method: http.MethodGet, Path: "/metadata/covers/{token}", ID: "getCandidateCover",
			Summary: "A provider's cover for a candidate, fetched through the server", Tag: "books",
			Produces: "image/*", Status: http.StatusOK, Reads: ReadsNoLibrary, Permission: auth.PersonalManage, Handler: s.getCandidateCover,
		},
		{
			Method: http.MethodGet, Path: "/books/names", ID: "listNames",
			Summary: "Author, series, publisher or tag names in use that begin as typed", Tag: "books",
			Query: namesQuery{}, Response: nameList{}, Reads: ReadsLibraries, Permission: auth.MetadataEdit, Handler: s.listNames,
		},
		{
			Method: http.MethodGet, Path: "/files/{fileId}/download", ID: "downloadFile",
			Summary: "A book's file as it lies on disk; answers range requests", Tag: "books",
			Produces: "application/octet-stream", Status: http.StatusOK,
			Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.downloadFile,
		},
		{
			Method: http.MethodGet, Path: "/books/{bookId}/covers/{size}", ID: "getBookCover",
			Summary: "A book's cover as a JPEG; size is small or large", Tag: "books",
			Produces: "image/jpeg", Status: http.StatusOK,
			Reads: ReadsLibraries, Permission: auth.LibraryRead, Handler: s.getBookCover,
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
