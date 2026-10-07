package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// Kinds of event in the libraries. Each is a kind of notification too.
const (
	// KindBooksAdded is a scan that took in new books. Data: library,
	// count.
	KindBooksAdded = "books.added"
	// KindScanFailed is a scan that could not read the library's folder.
	// Data: library, error.
	KindScanFailed = "scan.failed"
	// KindFilesUnreadable is a file that could not be read as its format.
	// Data: library, file, title.
	KindFilesUnreadable = "files.unreadable"
	// KindDuplicatesFound is a check that found a book new duplicates.
	// Data: title, count.
	KindDuplicatesFound = "duplicates.found"
	// KindReviewNeeded is a book whose lookup left matches to review.
	// Data: title.
	KindReviewNeeded = "review.needed"
	// KindWishFulfilled is a wished-for book that arrived. Data: title.
	KindWishFulfilled = "wish.fulfilled"
	// KindReleaseAnnounced is a new book of an author or a series the
	// person follows, still to come. Data: title, authors, subject, and
	// date and precision where known.
	KindReleaseAnnounced = "release.announced"
	// KindReleaseOut is a book the person follows that came out. Data as
	// KindReleaseAnnounced.
	KindReleaseOut = "release.out"
)

// EventKind says who hears of a kind of event and where a summary of
// several leads.
type EventKind struct {
	Kind string
	// Permission is what a person needs to hear of it at all.
	Permission auth.Permission
	// Default is whether a person who never chose hears of it.
	Default bool
	// Summary is where a notification of several such events leads, given
	// the library they share, if any.
	Summary func(library *uuid.UUID) string
}

// EventKinds are the kinds of event, in the order a person chooses them.
var EventKinds = []EventKind{
	{Kind: KindBooksAdded, Permission: auth.LibraryRead, Default: true, Summary: libraryPage},
	{Kind: KindWishFulfilled, Permission: auth.PersonalManage, Default: true, Summary: page("/wishlist")},
	{Kind: KindReleaseAnnounced, Permission: auth.PersonalManage, Default: true, Summary: page("/releases")},
	{Kind: KindReleaseOut, Permission: auth.PersonalManage, Default: true, Summary: page("/releases")},
	{Kind: KindReviewNeeded, Permission: auth.MetadataEdit, Default: true, Summary: page("/review")},
	{Kind: KindDuplicatesFound, Permission: auth.MetadataEdit, Default: true, Summary: page("/duplicates")},
	{Kind: KindFilesUnreadable, Permission: auth.MetadataEdit, Default: true, Summary: page("/jobs")},
	{Kind: KindScanFailed, Permission: auth.StorageManage, Default: true, Summary: page("/admin/libraries")},
}

func libraryPage(library *uuid.UUID) string {
	if library == nil {
		return "/"
	}
	return "/?library=" + library.String()
}

func page(path string) func(*uuid.UUID) string {
	return func(*uuid.UUID) string { return path }
}

func eventKind(kind string) (EventKind, bool) {
	i := slices.IndexFunc(EventKinds, func(k EventKind) bool { return k.Kind == kind })
	if i < 0 {
		return EventKind{}, false
	}
	return EventKinds[i], true
}

var roles = []string{auth.RoleAdmin, auth.RoleEditor, auth.RoleReader}

func rolesAllowed(p auth.Permission) []string {
	var out []string
	for _, r := range roles {
		if auth.Allows(r, p) {
			out = append(out, r)
		}
	}
	return out
}

// Event is something that happened, for the people who are to hear of it.
type Event struct {
	Kind string
	// Libraries are those a person must see to hear of it; the first is
	// the one the notification is about.
	Libraries []uuid.UUID
	BookID    *uuid.UUID
	Data      map[string]any
	Link      string
	// Recipients, when given, are the only people who may hear of it.
	Recipients []uuid.UUID
}

// quiet is how long no event must have come before the waiting ones are
// told, and maxWait how long the oldest waits at most: a large import
// becomes a few notifications rather than one for each book.
const (
	quiet   = time.Minute
	maxWait = 10 * time.Minute
)

// Queue enqueues the job that tells waiting events.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Events queues events and folds them into notifications. A nil Events
// queues nothing.
type Events struct {
	Pool  *pgxpool.Pool
	Queue Queue
}

// QueueTx queues the event, in the transaction of what happened, for each
// person who is to hear of it, and asks for them to be told.
func (e *Events) QueueTx(ctx context.Context, tx pgx.Tx, ev Event) error {
	if e == nil {
		return nil
	}
	k, ok := eventKind(ev.Kind)
	if !ok {
		return fmt.Errorf("no event kind %q", ev.Kind)
	}
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	if ev.Data == nil {
		data = []byte("{}")
	}
	p := sqlc.QueueNotificationEventParams{
		Kind: ev.Kind, BookID: ev.BookID, Data: data, Link: ev.Link,
		Roles: rolesAllowed(k.Permission), Recipients: ev.Recipients, DefaultOn: k.Default,
		Libraries: ev.Libraries, SeesAllRoles: rolesAllowed(auth.StorageManage),
	}
	if p.Libraries == nil {
		p.Libraries = []uuid.UUID{}
	}
	if len(ev.Libraries) > 0 {
		p.LibraryID = &ev.Libraries[0]
	}
	n, err := sqlc.New(tx).QueueNotificationEvent(ctx, p)
	if err != nil || n == 0 {
		return err
	}
	_, err = e.Queue.InsertTx(ctx, tx, FlushArgs{}, jobs.InsertOpts{
		Queue: jobs.QueueNotify, Unique: true, ScheduledAt: time.Now().Add(quiet),
	})
	return err
}

// errWait is Flush's answer while events are still arriving.
var errWait = errors.New("events are still arriving")

// Flush folds the waiting events into notifications, one for each person,
// kind and library: the event itself where it is alone, a count where
// there are several. Unless forced, it waits while events are still coming,
// up to maxWait after the oldest, and says how long with a wait.
func (e *Events) Flush(ctx context.Context, now time.Time, force bool) (time.Duration, error) {
	var wait time.Duration
	err := db.InTx(ctx, e.Pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		span, err := q.NotificationEventSpan(ctx)
		if err != nil || span.Events == 0 {
			return err
		}
		if !force && now.Sub(span.Newest) < quiet && now.Sub(span.Oldest) < maxWait {
			wait = min(quiet-now.Sub(span.Newest), maxWait-now.Sub(span.Oldest))
			return errWait
		}
		events, err := q.TakeNotificationEvents(ctx, now)
		if err != nil {
			return err
		}
		type key struct {
			user, library uuid.UUID
			kind          string
		}
		groups := map[key][]sqlc.NotificationEvent{}
		var order []key
		for _, ev := range events {
			k := key{user: ev.UserID, kind: ev.Kind}
			if ev.LibraryID != nil {
				k.library = *ev.LibraryID
			}
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], ev)
		}
		for _, k := range order {
			n, err := fold(groups[k])
			if err != nil {
				return err
			}
			if _, err := CreateTx(ctx, tx, k.user, n); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errWait) {
		return wait, nil
	}
	return 0, err
}

// fold makes the notification of a group of events of one person, kind and
// library: the event itself if it is alone, otherwise the first's data with
// the count of all, which sums each event's count (one if it has none).
func fold(group []sqlc.NotificationEvent) (New, error) {
	first := group[0]
	if len(group) == 1 {
		return New{Kind: first.Kind, Data: json.RawMessage(first.Data), Link: first.Link, BookID: first.BookID, LibraryID: first.LibraryID}, nil
	}
	data := map[string]any{}
	if err := json.Unmarshal(first.Data, &data); err != nil {
		return New{}, err
	}
	count := 0.0
	for _, ev := range group {
		var d struct {
			Count *float64 `json:"count"`
		}
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return New{}, err
		}
		if d.Count != nil {
			count += *d.Count
		} else {
			count++
		}
	}
	summary := map[string]any{"count": count}
	if lib, ok := data["library"]; ok {
		summary["library"] = lib
	}
	k, _ := eventKind(first.Kind)
	link := "/"
	if k.Summary != nil {
		link = k.Summary(first.LibraryID)
	}
	return New{Kind: first.Kind, Data: summary, Link: link, LibraryID: first.LibraryID}, nil
}

// FlushArgs is the job that tells waiting events.
type FlushArgs struct{}

// flushKind names the job in the queue. It is stored with every job, so it
// stays.
const flushKind = "notify.flush_events"

func (FlushArgs) Kind() string { return flushKind }

// FlushWorker runs FlushArgs.
type FlushWorker struct {
	river.WorkerDefaults[FlushArgs]
	Events *Events
}

func (w *FlushWorker) Work(ctx context.Context, _ *river.Job[FlushArgs]) error {
	wait, err := w.Events.Flush(ctx, time.Now(), false)
	if err != nil {
		return err
	}
	if wait > 0 {
		return river.JobSnooze(wait)
	}
	// Events queued while this ran found it running and asked for no other.
	span, err := sqlc.New(w.Events.Pool).NotificationEventSpan(ctx)
	if err != nil {
		return err
	}
	if span.Events > 0 {
		return river.JobSnooze(quiet)
	}
	return nil
}
