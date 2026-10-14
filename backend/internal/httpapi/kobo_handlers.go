package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

// koboPrefix is where a Kobo reader's api_endpoint points, with its device
// token after it: the reader sends no credentials to a store it was given.
const koboPrefix = APIPrefix + "/kobo/"

// koboPage is how many changes one library sync answer carries; the reader
// asks again while the answer says more follow.
const koboPage = 100

// koboTime is how Kobo's store writes a moment.
const koboTime = "2006-01-02T15:04:05Z"

// koboToken is the device token in a Kobo reader's request path.
func koboToken(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, koboPrefix)
	if !ok {
		return "", false
	}
	token, _, _ := strings.Cut(rest, "/")
	return token, token != ""
}

// loggedPath is the path as the log may keep it: a Kobo reader's token
// signs it in, so it is left out.
func loggedPath(path string) string {
	token, ok := koboToken(path)
	if !ok {
		return path
	}
	return strings.Replace(path, koboPrefix+token, koboPrefix+"…", 1)
}

// koboBase is the address the reader's requests go to, its token in it.
func koboBase(r *http.Request) string {
	scheme := "http"
	if overTLS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + koboPrefix + chi.URLParam(r, "token")
}

func koboClient(device uuid.UUID) string { return "kobo:" + device.String() }

// koboResources is the resource map the reader keeps under OneStoreServices:
// where it goes for each thing. The library, its details, reading places
// and covers are GOtome's; the store's pages and searches stay Kobo's.
type koboResources struct {
	Resources map[string]string `json:"Resources"`
}

// koboStore are the reader's resources as Kobo's own store sets them, for
// what GOtome does not answer.
var koboStore = map[string]string{
	"account_page":                          "https://www.kobo.com/account/settings",
	"autocomplete":                          "https://storeapi.kobo.com/v1/products/autocomplete",
	"book":                                  "https://storeapi.kobo.com/v1/products/books/{ProductId}",
	"book_detail_page":                      "https://www.kobo.com/{region}/{language}/ebook/{slug}",
	"book_landing_page":                     "https://www.kobo.com/ebooks",
	"categories":                            "https://storeapi.kobo.com/v1/categories",
	"category":                              "https://storeapi.kobo.com/v1/categories/{CategoryId}",
	"category_products":                     "https://storeapi.kobo.com/v1/categories/{CategoryId}/products",
	"dictionary_host":                       "https://ereaderfiles.kobo.com",
	"discovery_host":                        "https://discovery.kobobooks.com",
	"featured_list":                         "https://storeapi.kobo.com/v1/products/featured/{FeaturedListId}",
	"featured_lists":                        "https://storeapi.kobo.com/v1/products/featured",
	"oauth_host":                            "https://oauth.kobo.com",
	"product_recommendations":               "https://storeapi.kobo.com/v1/products/{ProductId}/recommendations",
	"product_reviews":                       "https://storeapi.kobo.com/v1/products/{ProductIds}/reviews",
	"purchase_buy_templated":                "https://www.kobo.com/{region}/{language}/checkoutoption/{ProductId}",
	"reading_services_host":                 "https://readingservices.kobo.com",
	"review":                                "https://storeapi.kobo.com/v1/products/reviews/{ReviewId}",
	"sign_in_page":                          "https://auth.kobobooks.com/ActivateOnWeb",
	"social_host":                           "https://social.kobobooks.com",
	"store_home":                            "www.kobo.com/{region}/{language}",
	"store_host":                            "www.kobo.com",
	"store_search":                          "https://www.kobo.com/{region}/{language}/Search?Query={query}",
	"user_ratings":                          "https://storeapi.kobo.com/v1/user/ratings",
	"user_recommendations":                  "https://storeapi.kobo.com/v1/user/recommendations",
	"user_reviews":                          "https://storeapi.kobo.com/v1/user/reviews",
	"userguide_host":                        "https://ereaderfiles.kobo.com",
	"kobo_audiobooks_enabled":               "False",
	"kobo_nativeborrow_enabled":             "False",
	"kobo_onestorelibrary_enabled":          "False",
	"kobo_redeem_enabled":                   "False",
	"kobo_subscriptions_enabled":            "False",
	"kobo_superpoints_enabled":              "False",
	"kobo_wishlist_enabled":                 "False",
	"kobo_dropbox_link_account_enabled":     "False",
	"kobo_googledrive_link_account_enabled": "False",
}

func (s *Server) koboInitialization(w http.ResponseWriter, r *http.Request) error {
	base := koboBase(r)
	res := make(map[string]string, len(koboStore)+20)
	for k, v := range koboStore {
		res[k] = v
	}
	for k, path := range map[string]string{
		"library_sync":               "/v1/library/sync",
		"library_metadata":           "/v1/library/{Ids}/metadata",
		"library_items":              "/v1/user/library",
		"library_book":               "/v1/user/library/books/{LibraryItemId}",
		"library_prices":             "/v1/user/library/previews/prices",
		"library_stack":              "/v1/user/library/stacks/{LibraryItemId}",
		"reading_state":              "/v1/library/{Ids}/state",
		"delete_entitlement":         "/v1/library/{Ids}",
		"tags":                       "/v1/library/tags",
		"user_profile":               "/v1/user/profile",
		"user_loyalty_benefits":      "/v1/user/loyalty/benefits",
		"user_wishlist":              "/v1/user/wishlist",
		"deals":                      "/v1/deals",
		"get_tests_request":          "/v1/analytics/gettests",
		"post_analytics_event":       "/v1/analytics/event",
		"device_auth":                "/v1/auth/device",
		"device_refresh":             "/v1/auth/refresh",
		"image_url_template":         "/{ImageId}/{Width}/{Height}/false/image.jpg",
		"image_url_quality_template": "/{ImageId}/{Width}/{Height}/{Quality}/{IsGreyscale}/image.jpg",
	} {
		res[k] = base + path
	}
	res["image_host"] = base + "/"
	// What the store would send, read by nothing GOtome knows of: an empty
	// JSON object.
	w.Header().Set("x-kobo-apitoken", "e30=")
	writeJSON(w, r, http.StatusOK, koboResources{Resources: res})
	return nil
}

// koboEntitlement says the reader holds a book, or no longer does.
type koboEntitlement struct {
	Accessibility       string     `json:"Accessibility"`
	ActivePeriod        koboPeriod `json:"ActivePeriod"`
	Created             string     `json:"Created"`
	CrossRevisionID     uuid.UUID  `json:"CrossRevisionId"`
	ID                  uuid.UUID  `json:"Id"`
	IsHiddenFromArchive bool       `json:"IsHiddenFromArchive"`
	IsLocked            bool       `json:"IsLocked"`
	IsRemoved           bool       `json:"IsRemoved"`
	LastModified        string     `json:"LastModified"`
	OriginCategory      string     `json:"OriginCategory"`
	RevisionID          uuid.UUID  `json:"RevisionId"`
	Status              string     `json:"Status"`
}

type koboPeriod struct {
	From string `json:"From"`
}

// koboMetadata is a book's details as the reader shows and downloads it.
type koboMetadata struct {
	Categories             []string          `json:"Categories"`
	Contributors           []string          `json:"Contributors"`
	ContributorRoles       []koboContributor `json:"ContributorRoles"`
	CoverImageID           string            `json:"CoverImageId,omitempty"`
	CrossRevisionID        uuid.UUID         `json:"CrossRevisionId"`
	CurrentDisplayPrice    koboPrice         `json:"CurrentDisplayPrice"`
	Description            string            `json:"Description"`
	DownloadUrls           []koboDownload    `json:"DownloadUrls"`
	EntitlementID          uuid.UUID         `json:"EntitlementId"`
	ExternalIDs            []string          `json:"ExternalIds"`
	Genre                  string            `json:"Genre"`
	IsEligibleForKoboLove  bool              `json:"IsEligibleForKoboLove"`
	IsInternetArchive      bool              `json:"IsInternetArchive"`
	IsPreOrder             bool              `json:"IsPreOrder"`
	IsSocialEnabled        bool              `json:"IsSocialEnabled"`
	Language               string            `json:"Language"`
	PhoneticPronunciations map[string]string `json:"PhoneticPronunciations"`
	PublicationDate        string            `json:"PublicationDate,omitempty"`
	Publisher              koboPublisher     `json:"Publisher"`
	RevisionID             uuid.UUID         `json:"RevisionId"`
	Series                 *koboSeries       `json:"Series,omitempty"`
	Title                  string            `json:"Title"`
	WorkID                 uuid.UUID         `json:"WorkId"`
}

type koboContributor struct {
	Name string `json:"Name"`
}

type koboPrice struct {
	CurrencyCode string  `json:"CurrencyCode"`
	TotalAmount  float64 `json:"TotalAmount"`
}

type koboDownload struct {
	Format   string `json:"Format"`
	Size     int64  `json:"Size"`
	URL      string `json:"Url"`
	Platform string `json:"Platform"`
}

type koboPublisher struct {
	Imprint string `json:"Imprint"`
	Name    string `json:"Name"`
}

type koboSeries struct {
	Name        string    `json:"Name"`
	Number      string    `json:"Number"`
	NumberFloat float64   `json:"NumberFloat"`
	ID          uuid.UUID `json:"Id"`
}

// koboReadingState is where the person is in a book, as the reader keeps it.
type koboReadingState struct {
	EntitlementID     uuid.UUID      `json:"EntitlementId"`
	Created           string         `json:"Created"`
	LastModified      string         `json:"LastModified"`
	PriorityTimestamp string         `json:"PriorityTimestamp"`
	StatusInfo        koboStatusInfo `json:"StatusInfo"`
	Statistics        koboStatistics `json:"Statistics"`
	CurrentBookmark   koboBookmark   `json:"CurrentBookmark"`
}

type koboStatusInfo struct {
	LastModified        string `json:"LastModified"`
	Status              string `json:"Status"`
	TimesStartedReading int    `json:"TimesStartedReading"`
}

type koboStatistics struct {
	LastModified string `json:"LastModified"`
}

type koboBookmark struct {
	LastModified                 string   `json:"LastModified"`
	ProgressPercent              *float64 `json:"ProgressPercent,omitempty"`
	ContentSourceProgressPercent *float64 `json:"ContentSourceProgressPercent,omitempty"`
}

// koboSyncItem is one change a library sync tells the reader of; one of
// its fields is set.
type koboSyncItem struct {
	NewEntitlement      *koboBookChange  `json:"NewEntitlement,omitempty"`
	ChangedEntitlement  *koboBookChange  `json:"ChangedEntitlement,omitempty"`
	ChangedReadingState *koboStateChange `json:"ChangedReadingState,omitempty"`
}

type koboBookChange struct {
	BookEntitlement koboEntitlement   `json:"BookEntitlement"`
	BookMetadata    *koboMetadata     `json:"BookMetadata,omitempty"`
	ReadingState    *koboReadingState `json:"ReadingState,omitempty"`
}

type koboStateChange struct {
	ReadingState koboReadingState `json:"ReadingState"`
}

// koboStateUpdate is what the reader sends of the places read.
type koboStateUpdate struct {
	ReadingStates []koboStateIn `json:"ReadingStates"`
}

type koboStateIn struct {
	EntitlementID   string `json:"EntitlementId"`
	CurrentBookmark *struct {
		ProgressPercent *float64 `json:"ProgressPercent"`
	} `json:"CurrentBookmark"`
	StatusInfo *struct {
		Status string `json:"Status"`
	} `json:"StatusInfo"`
}

type koboStateResult struct {
	RequestResult string             `json:"RequestResult"`
	UpdateResults []koboUpdateResult `json:"UpdateResults"`
}

type koboUpdateResult struct {
	EntitlementID         string     `json:"EntitlementId"`
	CurrentBookmarkResult koboResult `json:"CurrentBookmarkResult"`
	StatisticsResult      koboResult `json:"StatisticsResult"`
	StatusInfoResult      koboResult `json:"StatusInfoResult"`
}

type koboResult struct {
	Result string `json:"Result"`
}

// koboTokens answer a reader that asks for store tokens: GOtome signs it
// in by its address, so they are only kept and sent back.
type koboTokens struct {
	AccessToken  string `json:"AccessToken"`
	RefreshToken string `json:"RefreshToken"`
	TokenType    string `json:"TokenType"`
	TrackingID   string `json:"TrackingId"`
	UserKey      string `json:"UserKey"`
}

func (s *Server) koboSync(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	// A reader without the token GOtome gave it starts from nothing.
	fresh := r.Header.Get("x-kobo-synctoken") == ""
	changes, more, err := s.Kobo.Sync(r.Context(), scope, device, koboClient(device), fresh, koboPage)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for _, c := range changes {
		if !c.Removed {
			ids = append(ids, c.BookID)
		}
	}
	books, err := s.koboBooks(r, scope, ids)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(koboTime)
	out := []koboSyncItem{}
	for _, c := range changes {
		if c.Removed {
			out = append(out, koboSyncItem{ChangedEntitlement: &koboBookChange{BookEntitlement: entitlement(c.BookID, now, now, true)}})
			continue
		}
		b, ok := books[c.BookID]
		if !ok {
			continue
		}
		var state *koboReadingState
		if c.State || c.New {
			st, err := s.koboState(r, scope, c.BookID)
			if err != nil {
				return err
			}
			state = &st
		}
		switch {
		case c.New:
			out = append(out, koboSyncItem{NewEntitlement: &koboBookChange{BookEntitlement: b.entitlement, BookMetadata: &b.metadata, ReadingState: state}})
		case c.Metadata:
			out = append(out, koboSyncItem{ChangedEntitlement: &koboBookChange{BookEntitlement: b.entitlement, BookMetadata: &b.metadata}})
			if c.State {
				out = append(out, koboSyncItem{ChangedReadingState: &koboStateChange{ReadingState: *state}})
			}
		case c.State:
			out = append(out, koboSyncItem{ChangedReadingState: &koboStateChange{ReadingState: *state}})
		}
	}
	w.Header().Set("x-kobo-synctoken", "Z290b21l")
	if more {
		w.Header().Set("x-kobo-sync", "continue")
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

type koboBook struct {
	entitlement koboEntitlement
	metadata    koboMetadata
}

// koboBooks are the entitlements and details of the books among ids the
// scope sees that have an EPUB on disk.
func (s *Server) koboBooks(r *http.Request, scope library.Scope, ids []uuid.UUID) (map[uuid.UUID]koboBook, error) {
	out := map[uuid.UUID]koboBook{}
	if len(ids) == 0 {
		return out, nil
	}
	summaries, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return nil, err
	}
	feed, err := s.Books.FeedBooks(r.Context(), scope, ids)
	if err != nil {
		return nil, err
	}
	base := koboBase(r)
	for _, b := range summaries {
		f, ok := feed[b.ID]
		if !ok {
			continue
		}
		var epub *catalog.FeedFile
		for i := range f.Files {
			if f.Files[i].Format == "epub" {
				epub = &f.Files[i]
				break
			}
		}
		if epub == nil {
			continue
		}
		changed := f.UpdatedAt.UTC().Format(koboTime)
		added := b.AddedAt.UTC().Format(koboTime)
		m := koboMetadata{
			Categories:             []string{"00000000-0000-0000-0000-000000000001"},
			Contributors:           b.Authors,
			ContributorRoles:       []koboContributor{},
			CrossRevisionID:        b.ID,
			CurrentDisplayPrice:    koboPrice{CurrencyCode: "EUR"},
			Description:            f.Description,
			DownloadUrls:           []koboDownload{{Format: "EPUB", Size: epub.Size, URL: base + "/download/" + epub.ID.String(), Platform: "Generic"}},
			EntitlementID:          b.ID,
			ExternalIDs:            []string{},
			Genre:                  "00000000-0000-0000-0000-000000000001",
			IsSocialEnabled:        true,
			Language:               f.Language,
			PhoneticPronunciations: map[string]string{},
			RevisionID:             b.ID,
			Title:                  b.Title,
			WorkID:                 b.ID,
		}
		for _, a := range b.Authors {
			m.ContributorRoles = append(m.ContributorRoles, koboContributor{Name: a})
		}
		if m.Contributors == nil {
			m.Contributors = []string{}
		}
		if b.CoverKey != "" {
			m.CoverImageID = b.ID.String()
		}
		if b.PublishedYear != nil {
			m.PublicationDate = fmt.Sprintf("%04d-01-01T00:00:00Z", *b.PublishedYear)
		}
		if b.Series != "" {
			sr := &koboSeries{Name: b.Series, ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("gotome:series:"+b.Series))}
			if b.SeriesIndex != nil {
				sr.NumberFloat = *b.SeriesIndex
				sr.Number = strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", *b.SeriesIndex), "0"), ".")
			}
			m.Series = sr
		}
		out[b.ID] = koboBook{entitlement: entitlement(b.ID, added, changed, false), metadata: m}
	}
	return out, nil
}

func entitlement(id uuid.UUID, created, changed string, removed bool) koboEntitlement {
	return koboEntitlement{
		Accessibility: "Full", ActivePeriod: koboPeriod{From: created}, Created: created,
		CrossRevisionID: id, ID: id, IsRemoved: removed, LastModified: changed,
		OriginCategory: "Imported", RevisionID: id, Status: "Active",
	}
}

// koboState is where the person is in the book, for the reader.
func (s *Server) koboState(r *http.Request, scope library.Scope, bookID uuid.UUID) (koboReadingState, error) {
	state, err := s.Reading.Get(r.Context(), scope, bookID)
	if err != nil {
		return koboReadingState{}, err
	}
	at := time.Unix(0, 0).UTC().Format(koboTime)
	st := koboReadingState{
		EntitlementID: bookID, Created: at, LastModified: at, PriorityTimestamp: at,
		StatusInfo:      koboStatusInfo{LastModified: at, Status: "ReadyToRead"},
		Statistics:      koboStatistics{LastModified: at},
		CurrentBookmark: koboBookmark{LastModified: at},
	}
	for _, p := range state.Progress {
		if p.Medium != reading.MediumEbook {
			continue
		}
		at = p.UpdatedAt.UTC().Format(koboTime)
		percent := p.Fraction * 100
		st.LastModified, st.PriorityTimestamp = at, at
		st.Statistics.LastModified = at
		st.StatusInfo = koboStatusInfo{LastModified: at, Status: "Reading", TimesStartedReading: 1}
		if p.Fraction >= reading.End {
			st.StatusInfo.Status = "Finished"
		}
		st.CurrentBookmark = koboBookmark{LastModified: at, ProgressPercent: &percent, ContentSourceProgressPercent: &percent}
	}
	return st, nil
}

func (s *Server) koboMetadata(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	books, err := s.koboBooks(r, library.ScopeOf(*UserFrom(r.Context())), []uuid.UUID{id})
	if err != nil {
		return err
	}
	b, ok := books[id]
	if !ok {
		return ErrNotFound("There is no such book.")
	}
	writeJSON(w, r, http.StatusOK, []koboMetadata{b.metadata})
	return nil
}

func (s *Server) koboGetState(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	st, err := s.koboState(r, library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, reading.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, []koboReadingState{st})
	return nil
}

// koboPutState keeps the places the reader sends as the book's ebook
// progress, under the rule every client's are: one behind a place another
// device wrote since the reader was told of its last is not written, and
// the next sync tells the reader of the further one.
func (s *Server) koboPutState(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	var req koboStateUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	out := koboStateResult{RequestResult: "Success", UpdateResults: []koboUpdateResult{}}
	for _, st := range req.ReadingStates {
		res := koboUpdateResult{
			EntitlementID: st.EntitlementID, CurrentBookmarkResult: koboResult{"Ignored"},
			StatisticsResult: koboResult{"Ignored"}, StatusInfoResult: koboResult{"Ignored"},
		}
		fraction, ok := koboFraction(st)
		if ok {
			seen, err := s.Kobo.ProgressSeen(r.Context(), device, id)
			if err != nil {
				return err
			}
			p, saved, err := s.Reading.Save(r.Context(), scope, id, reading.Update{
				Progress: reading.Progress{
					Medium: reading.MediumEbook, Locator: fmt.Sprintf("fraction:%g", fraction),
					Fraction: fraction, ClientID: koboClient(device),
				},
				BasedOn: seen,
			})
			if errors.Is(err, reading.ErrNotFound) {
				return ErrNotFound("There is no such book.")
			}
			if err != nil {
				return err
			}
			if saved {
				if err := s.Kobo.SawProgress(r.Context(), device, id, p.UpdatedAt); err != nil {
					return err
				}
				res.CurrentBookmarkResult, res.StatusInfoResult = koboResult{"Success"}, koboResult{"Success"}
			}
		}
		out.UpdateResults = append(out.UpdateResults, res)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// koboFraction is how far into the book a reading state says the person
// is: its bookmark's percentage, or the end for a book marked finished.
func koboFraction(st koboStateIn) (float64, bool) {
	if st.StatusInfo != nil && st.StatusInfo.Status == "Finished" {
		return 1, true
	}
	if st.CurrentBookmark == nil || st.CurrentBookmark.ProgressPercent == nil {
		return 0, false
	}
	return min(max(*st.CurrentBookmark.ProgressPercent/100, 0), 1), true
}

// koboArchive takes a book the reader archived off the device: it leaves the
// device's deliveries and what the reader was told.
func (s *Server) koboArchive(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	if err := s.Deliveries.OffDevice(r.Context(), device, id); err != nil {
		return err
	}
	if err := s.Kobo.Forget(r.Context(), device, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// koboCover sends a book's cover in the size nearest what the reader asks.
func (s *Server) koboCover(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "cover")
	if err != nil {
		return err
	}
	size := covers.Small
	var width int
	if _, err := fmt.Sscanf(chi.URLParam(r, "width"), "%d", &width); err == nil && width > 300 {
		size = covers.Large
	}
	return s.serveCover(w, r, id, size)
}

func (s *Server) koboAuthDevice(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		UserKey string `json:"UserKey"`
	}
	_ = decodeJSON(w, r, &req)
	writeJSON(w, r, http.StatusOK, koboTokens{
		AccessToken: randomToken(), RefreshToken: randomToken(), TokenType: "Bearer",
		TrackingID: uuid.NewString(), UserKey: req.UserKey,
	})
	return nil
}

func randomToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// koboNothing answers what GOtome has nothing for, the store's offers and
// the reader's analytics, with an empty object, which the reader takes as
// nothing to show.
func (s *Server) koboNothing(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, http.StatusOK, map[string]any{})
	return nil
}

func (s *Server) koboEmptyList(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, http.StatusOK, []any{})
	return nil
}

func (s *Server) koboRoutes() []Route {
	const tag = "kobo"
	read := func(path, id, summary string, reads Reads, h HandlerFunc) Route {
		return Route{
			Method: http.MethodGet, Path: "/kobo/{token}" + path, ID: id, Summary: summary, Tag: tag,
			Reads: reads, Permission: auth.LibraryRead, Devices: true, Handler: h,
		}
	}
	routes := []Route{
		read("/v1/initialization", "koboInitialization", "Where a Kobo reader goes for its library, its details, reading places and covers: GOtome, with its device token in the address", ReadsNoLibrary, s.koboInitialization),
		read("/v1/library/sync", "koboSync", "The changes to a Kobo reader's library since its last sync: books sent to it, their details and reading places, and books taken off it", ReadsLibraries, s.koboSync),
		read("/v1/library/{bookId}/metadata", "koboMetadata", "A book's details as a Kobo reader shows them", ReadsLibraries, s.koboMetadata),
		read("/v1/library/{bookId}/state", "koboReadingState", "Where the person is in a book, as a Kobo reader keeps it", ReadsLibraries, s.koboGetState),
		read("/v1/library/tags", "koboTags", "A Kobo reader's collections: none yet", ReadsNoLibrary, s.koboEmptyList),
		read("/v1/user/profile", "koboProfile", "The store profile a Kobo reader asks for: nothing", ReadsNoLibrary, s.koboNothing),
		read("/v1/user/loyalty/benefits", "koboBenefits", "The store's loyalty benefits: none", ReadsNoLibrary, s.koboNothing),
		read("/v1/user/wishlist", "koboWishlist", "The store's wishlist: empty", ReadsNoLibrary, s.koboEmptyList),
		read("/v1/deals", "koboDeals", "The store's deals: none", ReadsNoLibrary, s.koboNothing),
		read("/{bookId}/{width}/{height}/false/image.jpg", "koboCover", "A book's cover for a Kobo reader", ReadsLibraries, s.koboCover),
		read("/{bookId}/{width}/{height}/{quality}/{greyscale}/image.jpg", "koboCoverQuality", "A book's cover for a Kobo reader, at a quality it names", ReadsLibraries, s.koboCover),
		{
			Method: http.MethodGet, Path: "/kobo/{token}/download/{fileId}", ID: "koboDownload",
			Summary: "A book's EPUB for a Kobo reader; the whole of it marks the book delivered", Tag: tag,
			Produces: "application/epub+zip", Status: http.StatusOK, Reads: ReadsLibraries,
			Permission: auth.LibraryRead, Devices: true, Handler: s.opdsDeviceFile,
		},
		{
			Method: http.MethodPut, Path: "/kobo/{token}/v1/library/{bookId}/state", ID: "koboPutReadingState",
			Summary: "Where a Kobo reader says the person is in a book", Tag: tag,
			Request: koboStateUpdate{}, Response: koboStateResult{},
			Permission: auth.PersonalManage, Devices: true, Handler: s.koboPutState,
		},
		{
			Method: http.MethodDelete, Path: "/kobo/{token}/v1/library/{bookId}", ID: "koboArchive",
			Summary: "A Kobo reader archives a book: it leaves the device", Tag: tag,
			Status: http.StatusNoContent, Permission: auth.PersonalManage, Devices: true, Handler: s.koboArchive,
		},
	}
	for _, p := range []struct{ path, id, summary string }{
		{"/v1/auth/device", "koboAuthDevice", "Store tokens a Kobo reader asks for; GOtome signs it in by its address"},
		{"/v1/auth/refresh", "koboAuthRefresh", "Store tokens a Kobo reader renews"},
	} {
		routes = append(routes, Route{
			Method: http.MethodPost, Path: "/kobo/{token}" + p.path, ID: p.id, Summary: p.summary, Tag: tag,
			Response: koboTokens{}, Permission: auth.LibraryRead, Devices: true, Handler: s.koboAuthDevice,
		})
	}
	for _, p := range []struct{ path, id, summary string }{
		{"/v1/analytics/gettests", "koboAnalyticsTests", "The store's tests a Kobo reader asks for: none"},
		{"/v1/analytics/event", "koboAnalyticsEvent", "What a Kobo reader reports for the store's analytics: taken, kept nowhere"},
	} {
		routes = append(routes, Route{
			Method: http.MethodPost, Path: "/kobo/{token}" + p.path, ID: p.id, Summary: p.summary, Tag: tag,
			Permission: auth.LibraryRead, Devices: true, Handler: s.koboNothing,
		})
	}
	return routes
}
