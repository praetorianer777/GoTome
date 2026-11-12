// Package annotations keeps a person's highlights and notes in books
// (#408): made in a reader of the web app, or sent by KOReader from its
// sidecar. They are the person's alone, like user_books, and of books they
// still see.
package annotations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Kinds of locator.
const (
	KindCFI      = "cfi"
	KindPDF      = "pdf"
	KindXPointer = "xpointer"
)

// Kinds lists every kind of locator.
var Kinds = []string{KindCFI, KindPDF, KindXPointer}

// Colors a highlight may have; the first is the default.
var Colors = []string{"yellow", "orange", "red", "green", "blue", "purple", "gray"}

// WebClient is the client ID of annotations made in the web app.
const WebClient = "web"

const (
	maxLocator = 2000
	maxPassage = 20000
	maxNote    = 20000
	maxKey     = 600
	maxClient  = 100
	maxRects   = 200
	// MaxPerBook bounds what one book holds for one person, and what a
	// device may send for it.
	MaxPerBook = 5000
	// DefaultLimit and MaxLimit are the sizes of a page of the list.
	DefaultLimit = 50
	MaxLimit     = 200
)

// ErrNotFound is an annotation or a book the person may not see.
var ErrNotFound = errors.New("no such annotation")

// ErrTooMany is a book with more annotations than MaxPerBook.
var ErrTooMany = errors.New("too many annotations")

// Problems says, per field, what is wrong.
type Problems map[string]string

func (p Problems) Error() string { return "the annotation is not valid" }

// Rect is a rectangle on a PDF page, as fractions of the page from its
// top left.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Annotation is a highlight, with a note if the person wrote one.
type Annotation struct {
	ID         uuid.UUID
	BookID     uuid.UUID
	FileID     *uuid.UUID
	Kind       string
	Locator    string
	LocatorEnd string
	Page       *int32
	Rects      []Rect
	Passage    string
	Note       string
	Color      string
	Chapter    string
	// Fraction is how far into the book it lies, if known.
	Fraction  *float64
	ClientID  string
	CreatedAt time.Time
	UpdatedAt time.Time
	// BookTitle and BookAuthors say which book it is in.
	BookTitle   string
	BookAuthors []string
}

// Input is an annotation to keep; Sent adds the key a device names it by.
type Input struct {
	FileID     *uuid.UUID
	Kind       string
	Locator    string
	LocatorEnd string
	Page       *int32
	Rects      []Rect
	Passage    string
	Note       string
	Color      string
	Chapter    string
	Fraction   *float64
	// Key and CreatedAt are a device's: its stable name for the annotation
	// and when it made it. The web app leaves both out.
	Key       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Service keeps annotations.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// NormalizeColor maps a colour a device names (KOReader has more than
// GOtome) to one of Colors; lenient says an unknown name is the default.
func NormalizeColor(name string, lenient bool) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Colors[0], true
	}
	if slices.Contains(Colors, name) {
		return name, true
	}
	if lenient {
		switch name {
		case "olive":
			return "green", true
		case "cyan":
			return "blue", true
		case "grey":
			return "gray", true
		}
		return Colors[0], true
	}
	return "", false
}

func (in *Input) check(lenient bool) error {
	p := Problems{}
	in.Passage = strings.TrimSpace(in.Passage)
	in.Note = strings.TrimSpace(in.Note)
	if !slices.Contains(Kinds, in.Kind) {
		p["kind"] = "The kind of locator is cfi, pdf or xpointer."
	}
	if strings.TrimSpace(in.Locator) == "" || len(in.Locator) > maxLocator || len(in.LocatorEnd) > maxLocator {
		p["locator"] = fmt.Sprintf("Give where in the book, in at most %d characters.", maxLocator)
	}
	if in.Passage == "" || utf8.RuneCountInString(in.Passage) > maxPassage {
		p["passage"] = fmt.Sprintf("Give the highlighted text, in at most %d characters.", maxPassage)
	}
	if utf8.RuneCountInString(in.Note) > maxNote {
		p["note"] = fmt.Sprintf("A note is at most %d characters.", maxNote)
	}
	color, ok := NormalizeColor(in.Color, lenient)
	if !ok {
		p["color"] = "The colour is one of " + strings.Join(Colors, ", ") + "."
	}
	in.Color = color
	if in.Page != nil && *in.Page < 1 {
		p["page"] = "A page is from 1."
	}
	if in.Kind == KindPDF && in.Page == nil {
		p["page"] = "A PDF's highlight is on a page."
	}
	if len(in.Rects) > maxRects {
		p["rects"] = fmt.Sprintf("At most %d rectangles.", maxRects)
	}
	for _, r := range in.Rects {
		if r.X < 0 || r.Y < 0 || r.W < 0 || r.H < 0 || r.X > 1 || r.Y > 1 || r.W > 1 || r.H > 1 {
			p["rects"] = "A rectangle is in fractions of the page, 0 to 1."
		}
	}
	if in.Fraction != nil && (*in.Fraction < 0 || *in.Fraction > 1) {
		p["fraction"] = "The fraction is between 0 and 1."
	}
	if len(in.Key) > maxKey {
		p["key"] = fmt.Sprintf("A key is at most %d characters.", maxKey)
	}
	if len(p) > 0 {
		return p
	}
	return nil
}

func (in Input) rects() ([]byte, error) {
	if len(in.Rects) == 0 {
		return nil, nil
	}
	return json.Marshal(in.Rects)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fromRow(r sqlc.Annotation) Annotation {
	a := Annotation{
		ID: r.ID, BookID: r.BookID, FileID: r.FileID, Kind: r.LocatorKind, Locator: r.Locator,
		Page: r.Page, Passage: r.Passage, Color: r.Color, Fraction: r.Fraction, ClientID: r.ClientID,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.LocatorEnd != nil {
		a.LocatorEnd = *r.LocatorEnd
	}
	if r.Note != nil {
		a.Note = *r.Note
	}
	if r.Chapter != nil {
		a.Chapter = *r.Chapter
	}
	a.Rects = rectsOf(r.Rects)
	return a
}

func rectsOf(raw []byte) []Rect {
	if len(raw) == 0 {
		return nil
	}
	var out []Rect
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func visible(ctx context.Context, q *sqlc.Queries, scope library.Scope, bookID uuid.UUID) error {
	ids, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{
		Ids: []uuid.UUID{bookID}, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrNotFound
	}
	return nil
}

func checkFile(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID, fileID *uuid.UUID) error {
	if fileID == nil {
		return nil
	}
	ok, err := q.FileOfBook(ctx, sqlc.FileOfBookParams{ID: *fileID, BookID: bookID})
	if err != nil {
		return err
	}
	if !ok {
		return Problems{"fileId": "The file is not one of this book's."}
	}
	return nil
}

// Create keeps a highlight the person made in the web app.
func (s *Service) Create(ctx context.Context, scope library.Scope, bookID uuid.UUID, in Input) (Annotation, error) {
	if err := in.check(false); err != nil {
		return Annotation{}, err
	}
	rects, err := in.rects()
	if err != nil {
		return Annotation{}, err
	}
	var out Annotation
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := visible(ctx, q, scope, bookID); err != nil {
			return err
		}
		if err := checkFile(ctx, q, bookID, in.FileID); err != nil {
			return err
		}
		n, err := q.CountBookAnnotations(ctx, sqlc.CountBookAnnotationsParams{UserID: scope.Viewer, BookID: bookID})
		if err != nil {
			return err
		}
		if n >= MaxPerBook {
			return ErrTooMany
		}
		row, err := q.CreateAnnotation(ctx, sqlc.CreateAnnotationParams{
			UserID: scope.Viewer, BookID: bookID, FileID: in.FileID, LocatorKind: in.Kind, Locator: in.Locator,
			LocatorEnd: optional(in.LocatorEnd), Page: in.Page, Rects: rects, Passage: in.Passage, Note: optional(in.Note),
			Color: in.Color, Chapter: optional(in.Chapter), Fraction: in.Fraction,
			ClientID: WebClient, ClientKey: uuid.NewString(),
		})
		out = fromRow(row)
		return err
	})
	return out, err
}

// Change sets a note and a colour; nil leaves one as it is. An empty note
// takes the note away.
func (s *Service) Change(ctx context.Context, scope library.Scope, id uuid.UUID, note, color *string) (Annotation, error) {
	var out Annotation
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.GetAnnotation(ctx, sqlc.GetAnnotationParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		newNote, newColor := row.Note, row.Color
		p := Problems{}
		if note != nil {
			trimmed := strings.TrimSpace(*note)
			if utf8.RuneCountInString(trimmed) > maxNote {
				p["note"] = fmt.Sprintf("A note is at most %d characters.", maxNote)
			}
			newNote = optional(trimmed)
		}
		if color != nil {
			c, ok := NormalizeColor(*color, false)
			if !ok {
				p["color"] = "The colour is one of " + strings.Join(Colors, ", ") + "."
			}
			newColor = c
		}
		if len(p) > 0 {
			return p
		}
		changed, err := q.ChangeAnnotation(ctx, sqlc.ChangeAnnotationParams{ID: id, Note: newNote, Color: newColor})
		out = fromRow(changed)
		return err
	})
	return out, err
}

// Delete takes an annotation away.
func (s *Service) Delete(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		_, err := q.GetAnnotation(ctx, sqlc.GetAnnotationParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return q.DeleteAnnotation(ctx, id)
	})
}

// Filter says which annotations to list.
type Filter struct {
	BookID *uuid.UUID
	// Words must each occur in the passage, the note, the title or an author.
	Words string
	// After is the cursor a page gave.
	After string
	Limit int
}

// Page is a page of annotations, newest first.
type Page struct {
	Items []Annotation
	// Next is the cursor of the following page; empty at the end.
	Next string
}

// List returns the scope's own annotations, of books they see.
func (s *Service) List(ctx context.Context, scope library.Scope, f Filter) (Page, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	return s.list(ctx, scope, f, limit)
}

// All returns every annotation List would, in pages of its own.
func (s *Service) All(ctx context.Context, scope library.Scope, f Filter) ([]Annotation, error) {
	var out []Annotation
	f.After = ""
	for {
		page, err := s.list(ctx, scope, f, MaxLimit)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if page.Next == "" {
			return out, nil
		}
		f.After = page.Next
	}
}

func (s *Service) list(ctx context.Context, scope library.Scope, f Filter, limit int) (Page, error) {
	arg := sqlc.ListAnnotationsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, BookID: f.BookID, MaxRows: int32(limit + 1)}
	if w := strings.Join(strings.Fields(f.Words), " "); w != "" {
		arg.Words = &w
	}
	if f.After != "" {
		at, id, err := decodeCursor(f.After)
		if err != nil {
			return Page{}, Problems{"after": "That is not a cursor this list gave."}
		}
		arg.BeforeAt, arg.BeforeID = &at, &id
	}
	rows, err := sqlc.New(s.pool).ListAnnotations(ctx, arg)
	if err != nil {
		return Page{}, err
	}
	var page Page
	for i, r := range rows {
		if i == limit {
			last := rows[limit-1]
			page.Next = encodeCursor(last.CreatedAt, last.ID)
			break
		}
		a := fromRow(sqlc.Annotation{
			ID: r.ID, UserID: r.UserID, BookID: r.BookID, FileID: r.FileID, LocatorKind: r.LocatorKind, Locator: r.Locator,
			LocatorEnd: r.LocatorEnd, Page: r.Page, Rects: r.Rects, Passage: r.Passage, Note: r.Note, Color: r.Color,
			Chapter: r.Chapter, Fraction: r.Fraction, ClientID: r.ClientID, ClientKey: r.ClientKey,
			EditedHere: r.EditedHere, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
		a.BookTitle, a.BookAuthors = r.BookTitle, r.BookAuthors
		page.Items = append(page.Items, a)
	}
	return page, nil
}

func encodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, errors.New("no separator")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	u, err := uuid.Parse(id)
	return t, u, err
}

// SyncResult says what a device's send changed.
type SyncResult struct {
	Kept    int
	Removed int
}

// Sync makes a device's annotations of a book what it sent: each is kept
// under its key, the same one again changing nothing, and those of the
// device it no longer sends go. What other clients made is not touched.
func (s *Service) Sync(ctx context.Context, scope library.Scope, bookID uuid.UUID, fileID uuid.UUID, clientID string, items []Input) (SyncResult, error) {
	p := Problems{}
	if strings.TrimSpace(clientID) == "" || len(clientID) > maxClient || clientID == WebClient {
		p["clientId"] = fmt.Sprintf("Name the device in at most %d characters.", maxClient)
	}
	if len(items) > MaxPerBook {
		p["annotations"] = fmt.Sprintf("At most %d annotations of a book.", MaxPerBook)
	}
	keys := make([]string, 0, len(items))
	seen := map[string]bool{}
	for i := range items {
		if err := items[i].check(true); err != nil {
			for k, v := range err.(Problems) {
				p[fmt.Sprintf("annotations[%d].%s", i, k)] = v
			}
			continue
		}
		if items[i].Key == "" {
			p[fmt.Sprintf("annotations[%d].key", i)] = "Name each annotation by a key that stays the same."
			continue
		}
		if seen[items[i].Key] {
			continue
		}
		seen[items[i].Key] = true
		keys = append(keys, items[i].Key)
	}
	if len(p) > 0 {
		return SyncResult{}, p
	}
	var out SyncResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := visible(ctx, q, scope, bookID); err != nil {
			return err
		}
		done := map[string]bool{}
		for _, in := range items {
			if done[in.Key] {
				continue
			}
			done[in.Key] = true
			rects, err := in.rects()
			if err != nil {
				return err
			}
			created, updated := in.CreatedAt, in.UpdatedAt
			if created.IsZero() {
				created = time.Now()
			}
			if updated.IsZero() {
				updated = created
			}
			if err := q.SyncAnnotation(ctx, sqlc.SyncAnnotationParams{
				UserID: scope.Viewer, BookID: bookID, FileID: &fileID, LocatorKind: in.Kind, Locator: in.Locator,
				LocatorEnd: optional(in.LocatorEnd), Page: in.Page, Rects: rects, Passage: in.Passage, Note: optional(in.Note),
				Color: in.Color, Chapter: optional(in.Chapter), Fraction: in.Fraction, ClientID: clientID, ClientKey: in.Key,
				CreatedAt: created, UpdatedAt: updated,
			}); err != nil {
				return err
			}
		}
		removed, err := q.DropAnnotationsNotSent(ctx, sqlc.DropAnnotationsNotSentParams{
			UserID: scope.Viewer, BookID: bookID, ClientID: clientID, Column4: keys,
		})
		out = SyncResult{Kept: len(keys), Removed: int(removed)}
		return err
	})
	return out, err
}
