package httpapi

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/format/markup"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/opds"
)

// opdsPath is where the catalogue for e-readers starts.
const opdsPath = APIPrefix + "/opds"

// opdsStarted is when the navigation feeds were last changed: they are
// the same until the server starts again.
var opdsStarted = time.Now()

// opdsPage is how many entries a feed holds: an e-reader loads the next
// page as its list scrolls on.
const opdsPage = 50

type opdsBooksQuery struct {
	Author string `query:"author" doc:"An author's name key; only their books."`
	Series string `query:"series" doc:"A series's name key; only its books, in their order."`
	Status string `query:"status" enum:"reading,wishlist,completed" doc:"Only the books the caller has this status for."`
	After  string `query:"after" doc:"The cursor of the page before."`
}

type opdsNamesQuery struct {
	After string `query:"after" doc:"The cursor of the page before."`
}

type opdsPageQuery struct {
	Page int `query:"page" doc:"The page, from 1."`
}

type opdsSearchQuery struct {
	Q string `query:"q" doc:"Words of a title, an author or a series."`
}

func (s *Server) opdsRoutes() []Route {
	nav := func(method, path, id, summary string, reads Reads, query any, h HandlerFunc) Route {
		return Route{
			Method: method, Path: path, ID: id, Summary: summary, Tag: "opds", Query: query,
			Produces: opds.NavigationType, Status: http.StatusOK, Reads: reads,
			Permission: auth.LibraryRead, Devices: true, Handler: h,
		}
	}
	acq := func(path, id, summary string, query any, h HandlerFunc) Route {
		r := nav(http.MethodGet, path, id, summary, ReadsLibraries, query, h)
		r.Produces = opds.AcquisitionType
		return r
	}
	search := nav(http.MethodGet, "/opds/opensearch.xml", "getOpdsSearchDescription",
		"The OpenSearch description of the catalogue's search", ReadsNoLibrary, nil, s.opdsSearchDescription)
	search.Produces = opds.OpenSearchType
	return []Route{
		nav(http.MethodGet, "/opds", "getOpdsRoot", "The catalogue for e-readers: what it offers to browse", ReadsNoLibrary, nil, s.opdsRoot),
		search,
		acq("/opds/search", "searchOpds", "Books whose title, author or series looks like the words", opdsSearchQuery{}, s.opdsSearch),
		acq("/opds/books", "listOpdsBooks", "Books with an ebook, the latest first, or of an author, a series or a status", opdsBooksQuery{}, s.opdsBooks),
		nav(http.MethodGet, "/opds/authors", "listOpdsAuthors", "Authors of books with an ebook, alphabetically", ReadsLibraries, opdsNamesQuery{}, s.opdsNames(catalog.NamesAuthors)),
		nav(http.MethodGet, "/opds/series", "listOpdsSeries", "Series of books with an ebook, alphabetically", ReadsLibraries, opdsNamesQuery{}, s.opdsNames(catalog.NamesSeries)),
		nav(http.MethodGet, "/opds/shelves", "listOpdsShelves", "The collections and smart shelves the caller may see", ReadsLibraries, nil, s.opdsShelves),
		acq("/opds/collections/{collectionId}", "listOpdsCollection", "A collection's books, in its order", opdsPageQuery{}, s.opdsCollection),
		acq("/opds/smart-shelves/{shelfId}", "listOpdsSmartShelf", "The books a smart shelf holds", opdsNamesQuery{}, s.opdsSmartShelf),
	}
}

func opdsHref(path string, query url.Values) string {
	if len(query) == 0 {
		return opdsPath + path
	}
	return opdsPath + path + "?" + query.Encode()
}

// opdsFeed starts a feed at the path, with the links every feed has. It
// does not repeat what the query asked for, so that a feed about nothing
// reads the same as one about something the caller may not see.
func opdsFeed(r *http.Request, title, kind string, updated time.Time) opds.Feed {
	f := opds.NewFeed("urn:gotome:opds:"+strings.TrimPrefix(r.URL.Path, opdsPath), title, updated)
	f.Links = []opds.Link{
		{Rel: "self", Href: r.URL.Path, Type: kind},
		{Rel: "start", Href: opdsPath, Type: opds.NavigationType},
		{Rel: "search", Href: opdsHref("/opensearch.xml", nil), Type: opds.OpenSearchType},
	}
	return f
}

func writeFeed(w http.ResponseWriter, kind string, f opds.Feed) error {
	// A feed of books changed when the latest of them did.
	for _, e := range f.Entries {
		if time.Time(e.Updated).After(time.Time(f.Updated)) {
			f.Updated = e.Updated
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", kind+";charset=utf-8")
	// Private: what a feed lists depends on who asks.
	h.Set("Cache-Control", "private, no-cache")
	_, err := buf.WriteTo(w)
	return err
}

func navEntry(id, title, text, href, kind string) opds.Entry {
	return opds.Entry{
		ID: "urn:gotome:opds:" + id, Title: title, Updated: opds.Time(opdsStarted),
		Content: opds.TextContent(text), Links: []opds.Link{{Rel: opds.RelSubsection, Href: href, Type: kind}},
	}
}

func (s *Server) opdsRoot(w http.ResponseWriter, r *http.Request) error {
	f := opdsFeed(r, "GOtome", opds.NavigationType, opdsStarted)
	status := func(s string) string { return opdsHref("/books", url.Values{"status": {s}}) }
	f.Entries = []opds.Entry{
		navEntry("books", "Recently added", "The latest books first.", opdsHref("/books", nil), opds.AcquisitionType),
		navEntry("authors", "Authors", "Every author, alphabetically.", opdsHref("/authors", nil), opds.NavigationType),
		navEntry("series", "Series", "Every series, alphabetically.", opdsHref("/series", nil), opds.NavigationType),
		navEntry("shelves", "Shelves", "Collections and smart shelves.", opdsHref("/shelves", nil), opds.NavigationType),
		navEntry("reading", "Reading", "The books you are reading.", status(catalog.StatusReading), opds.AcquisitionType),
		navEntry("wishlist", "Want to read", "The books you want to read.", status(catalog.StatusWishlist), opds.AcquisitionType),
	}
	return writeFeed(w, opds.NavigationType, f)
}

func (s *Server) opdsSearchDescription(w http.ResponseWriter, r *http.Request) error {
	var buf bytes.Buffer
	d := opds.NewSearch("GOtome", "Search the books by title, author or series.", opdsHref("/search", nil)+"?q={searchTerms}")
	if err := d.Write(&buf); err != nil {
		return err
	}
	w.Header().Set("Content-Type", opds.OpenSearchType+";charset=utf-8")
	_, err := buf.WriteTo(w)
	return err
}

func (s *Server) opdsSearch(w http.ResponseWriter, r *http.Request) error {
	var q opdsSearchQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	hits, err := s.Books.Search(r.Context(), scope, q.Q, catalog.MaxHits)
	if err != nil {
		return err
	}
	books := make([]catalog.Summary, len(hits))
	for i, h := range hits {
		books[i] = h.Summary
	}
	f := opdsFeed(r, "Search results", opds.AcquisitionType, opdsStarted)
	if f.Entries, err = s.opdsEntries(r.Context(), scope, books); err != nil {
		return err
	}
	return writeFeed(w, opds.AcquisitionType, f)
}

func (s *Server) opdsBooks(w http.ResponseWriter, r *http.Request) error {
	var q opdsBooksQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	rules := []filter.Node{catalog.HasEbook()}
	title := "Recently added"
	p := catalog.ListParams{Order: catalog.OrderAdded, Desc: true, After: q.After, Limit: opdsPage}
	if q.Author != "" {
		rules = append(rules, filter.Node{Field: catalog.FieldAuthor, Op: catalog.OpIn, Values: []string{q.Author}})
		p.Order, p.Desc = catalog.OrderTitle, false
	}
	if q.Series != "" {
		// A series is read in its order, which no list is kept in: its
		// books, few as they are, are put in order here.
		rules = append(rules, filter.Node{Field: catalog.FieldSeries, Op: catalog.OpIn, Values: []string{q.Series}})
		p.Order, p.Desc, p.Limit = catalog.OrderTitle, false, catalog.MaxPage
	}
	if q.Status != "" {
		rules = append(rules, filter.Node{Field: catalog.FieldStatus, Op: catalog.OpIn, Values: []string{q.Status}})
		title = map[string]string{catalog.StatusReading: "Reading", catalog.StatusWishlist: "Want to read", catalog.StatusCompleted: "Read"}[q.Status]
	}
	p.Filter = filter.Node{All: rules}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	page, err := s.Books.List(r.Context(), scope, p)
	if errors.Is(err, catalog.ErrBadCursor) {
		return ErrValidation(map[string]string{"after": "This cursor belongs to another list; start again from the first page."})
	}
	if err != nil {
		return err
	}
	switch {
	case q.Author != "":
		title = nameOf(page.Books, q.Author, func(b catalog.Summary) []string { return b.Authors })
	case q.Series != "":
		title = nameOf(page.Books, q.Series, func(b catalog.Summary) []string { return []string{b.Series} })
		slices.SortStableFunc(page.Books, func(a, b catalog.Summary) int {
			return cmp.Compare(placeInSeries(a), placeInSeries(b))
		})
	}
	f := opdsFeed(r, title, opds.AcquisitionType, opdsStarted)
	if f.Entries, err = s.opdsEntries(r.Context(), scope, page.Books); err != nil {
		return err
	}
	if page.Next != "" {
		next := r.URL.Query()
		next.Set("after", page.Next)
		f.Links = append(f.Links, opds.Link{Rel: "next", Href: opdsHref("/books", next), Type: opds.AcquisitionType})
	}
	return writeFeed(w, opds.AcquisitionType, f)
}

// nameOf is the name among the books' whose key is the one asked for, as
// the books spell it; without such a book, the feed's general title.
func nameOf(books []catalog.Summary, key string, names func(catalog.Summary) []string) string {
	for _, b := range books {
		for _, n := range names(b) {
			if catalog.Key(n) == catalog.Key(key) {
				return n
			}
		}
	}
	return "Books"
}

// placeInSeries orders a series's books by their position, those without one
// last.
func placeInSeries(b catalog.Summary) float64 {
	if b.SeriesIndex == nil {
		return 1e9
	}
	return *b.SeriesIndex
}

func (s *Server) opdsNames(list string) HandlerFunc {
	title := map[string]string{catalog.NamesAuthors: "Authors", catalog.NamesSeries: "Series"}[list]
	param := map[string]string{catalog.NamesAuthors: "author", catalog.NamesSeries: "series"}[list]
	return func(w http.ResponseWriter, r *http.Request) error {
		var q opdsNamesQuery
		if err := decodeQuery(r, &q); err != nil {
			return err
		}
		page, err := s.Books.NameList(r.Context(), library.ScopeOf(*UserFrom(r.Context())), list, q.After, opdsPage)
		if errors.Is(err, catalog.ErrBadCursor) {
			return ErrValidation(map[string]string{"after": "This cursor belongs to another list; start again from the first page."})
		}
		if err != nil {
			return err
		}
		f := opdsFeed(r, title, opds.NavigationType, opdsStarted)
		for _, n := range page.Names {
			f.Entries = append(f.Entries, navEntry(param+":"+n.Key, n.Name, books(n.Books),
				opdsHref("/books", url.Values{param: {n.Key}}), opds.AcquisitionType))
		}
		if page.Next != "" {
			f.Links = append(f.Links, opds.Link{Rel: "next", Href: opdsHref("/"+list, url.Values{"after": {page.Next}}), Type: opds.NavigationType})
		}
		return writeFeed(w, opds.NavigationType, f)
	}
}

func books(n int) string {
	if n == 1 {
		return "1 book"
	}
	return strconv.Itoa(n) + " books"
}

func (s *Server) opdsShelves(w http.ResponseWriter, r *http.Request) error {
	scope := library.ScopeOf(*UserFrom(r.Context()))
	collections, err := s.Shelves.List(r.Context(), scope, nil)
	if err != nil {
		return err
	}
	smart, err := s.Shelves.SmartShelves(r.Context(), scope)
	if err != nil {
		return err
	}
	f := opdsFeed(r, "Shelves", opds.NavigationType, opdsStarted)
	for _, c := range collections {
		f.Entries = append(f.Entries, navEntry("collection:"+c.ID.String(), c.Name, books(c.Books),
			opdsHref("/collections/"+c.ID.String(), nil), opds.AcquisitionType))
	}
	for _, sh := range smart {
		f.Entries = append(f.Entries, navEntry("smart-shelf:"+sh.ID.String(), sh.Name, books(sh.Books),
			opdsHref("/smart-shelves/"+sh.ID.String(), nil), opds.AcquisitionType))
	}
	return writeFeed(w, opds.NavigationType, f)
}

func (s *Server) opdsCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "collectionId", "collection")
	if err != nil {
		return err
	}
	var q opdsPageQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	c, ids, err := s.Shelves.Get(r.Context(), scope, id)
	if err != nil {
		return collectionError(err)
	}
	// A collection is in the order its owner gave it, so it is paged by
	// position.
	page := max(q.Page, 1)
	from := min((page-1)*opdsPage, len(ids))
	to := min(from+opdsPage, len(ids))
	summaries, err := s.Books.Summaries(r.Context(), scope, ids[from:to])
	if err != nil {
		return err
	}
	f := opdsFeed(r, c.Name, opds.AcquisitionType, c.UpdatedAt)
	if f.Entries, err = s.opdsEntries(r.Context(), scope, summaries); err != nil {
		return err
	}
	if to < len(ids) {
		f.Links = append(f.Links, opds.Link{Rel: "next", Href: opdsHref("/collections/"+id.String(), url.Values{"page": {strconv.Itoa(page + 1)}}), Type: opds.AcquisitionType})
	}
	return writeFeed(w, opds.AcquisitionType, f)
}

func (s *Server) opdsSmartShelf(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "shelfId", "smart shelf")
	if err != nil {
		return err
	}
	var q opdsNamesQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	shelf, err := s.Shelves.SmartShelf(r.Context(), scope, id)
	if err != nil {
		return smartShelfError(err)
	}
	page, err := s.Shelves.SmartBooks(r.Context(), scope, id, catalog.ListParams{Order: catalog.OrderTitle, After: q.After, Limit: opdsPage})
	switch {
	case errors.Is(err, catalog.ErrBadCursor):
		return ErrValidation(map[string]string{"after": "This cursor belongs to another list; start again from the first page."})
	case err != nil:
		return smartShelfError(err)
	}
	f := opdsFeed(r, shelf.Name, opds.AcquisitionType, shelf.UpdatedAt)
	if f.Entries, err = s.opdsEntries(r.Context(), scope, page.Books); err != nil {
		return err
	}
	if page.Next != "" {
		f.Links = append(f.Links, opds.Link{Rel: "next", Href: opdsHref("/smart-shelves/"+id.String(), url.Values{"after": {page.Next}}), Type: opds.AcquisitionType})
	}
	return writeFeed(w, opds.AcquisitionType, f)
}

// opdsEntries are the books as an acquisition feed lists them: a link to
// download each ebook file, the best format first, and the cover.
func (s *Server) opdsEntries(ctx context.Context, scope library.Scope, books []catalog.Summary) ([]opds.Entry, error) {
	ids := make([]uuid.UUID, len(books))
	for i, b := range books {
		ids[i] = b.ID
	}
	details, err := s.Books.FeedBooks(ctx, scope, ids)
	if err != nil {
		return nil, err
	}
	entries := make([]opds.Entry, 0, len(books))
	for _, b := range books {
		d, ok := details[b.ID]
		if !ok {
			continue
		}
		e := opds.Entry{
			ID: "urn:uuid:" + b.ID.String(), Title: b.Title, Updated: opds.Time(d.UpdatedAt), Language: d.Language,
			Content: opds.TextContent(bookText(b, d.Description)),
		}
		if b.Subtitle != "" {
			e.Title += ": " + b.Subtitle
		}
		if b.PublishedYear != nil {
			e.Issued = strconv.Itoa(int(*b.PublishedYear))
		}
		for _, a := range b.Authors {
			e.Authors = append(e.Authors, opds.Person{Name: a})
		}
		for _, file := range d.Files {
			e.Links = append(e.Links, opds.Link{
				Rel: opds.RelAcquisition, Href: APIPrefix + "/files/" + file.ID.String() + "/download",
				Type: cmp.Or(mediaTypes[file.Format], "application/octet-stream"), Title: strings.ToUpper(file.Format), Length: file.Size,
			})
		}
		if b.CoverKey != "" {
			cover := APIPrefix + "/books/" + b.ID.String() + "/covers/"
			v := "?v=" + url.QueryEscape(b.CoverKey)
			e.Links = append(e.Links,
				opds.Link{Rel: opds.RelImage, Href: cover + "large" + v, Type: "image/jpeg"},
				opds.Link{Rel: opds.RelThumbnail, Href: cover + "small" + v, Type: "image/jpeg"})
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// bookText is what an entry says of a book: its series and place in it,
// and its description as plain text.
func bookText(b catalog.Summary, description string) string {
	var parts []string
	if b.Series != "" {
		series := b.Series
		if b.SeriesIndex != nil {
			series += ", " + strconv.FormatFloat(*b.SeriesIndex, 'f', -1, 64)
		}
		parts = append(parts, series)
	}
	if strings.ContainsRune(description, '<') {
		description, _ = markup.Text([]byte(description))
	}
	if description = strings.TrimSpace(description); description != "" {
		parts = append(parts, description)
	}
	return strings.Join(parts, "\n\n")
}
