// Package kindle sends a book to a person's Kindle by e-mail (#412): the
// address Amazon gave their device, a mail server an administrator named,
// and a job for each send. Amazon takes the mail only from an address the
// person has approved in their Amazon account, which is the sender the
// administrator set.
package kindle

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// MaxAttachment is the largest file Amazon takes by e-mail: its help page
// for Send to Kindle gives 50 MB per message. Larger books are refused when
// they are asked for. A mail server may allow less (25 MB is common); it
// then refuses the message and the send fails with its answer.
const MaxAttachment = 50 << 20

// Format is the one format that is sent: Amazon takes EPUB and makes it
// Kindle-ready itself. Nothing is converted here, so a book with no EPUB is
// not sent.
const Format = "epub"

const (
	// Reasons a book was not queued.
	NotAvailable = "unavailable"
	TooLarge     = "tooLarge"
	AlreadyWaits = "pending"

	// maxMessage bounds the server's answer kept with a failed send.
	maxMessage  = 300
	maxAttempts = 4
	kind        = "kindle.send"
)

var (
	// ErrNotConfigured is a server on which no mail server is set up.
	ErrNotConfigured = errors.New("sending mail is not set up")
	// ErrNoAddress is a person who has not given a Kindle address.
	ErrNoAddress = errors.New("no Kindle address is set")
	// ErrBadAddress is an address that is not a Kindle's.
	ErrBadAddress = errors.New("not a Kindle address")
)

// Settings are what the service reads of the administrator's settings.
type Settings interface {
	Text(ctx context.Context, key string) (string, error)
	Secret(ctx context.Context, key string) (string, bool, error)
}

// Queue enqueues the jobs; it is jobs.Runner.
type Queue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts jobs.InsertOpts) (jobs.Inserted, error)
}

// Files opens the files of books a person may see; it is catalog.Service.
type Files interface {
	OpenFile(ctx context.Context, scope library.Scope, fileID uuid.UUID) (catalog.VisibleFile, *os.File, error)
}

// Service sends books.
type Service struct {
	pool     *pgxpool.Pool
	settings Settings
	files    Files
	log      *slog.Logger
	Queue    Queue
	// Offline sends nothing, as GOTOME_OFFLINE asks of every outbound
	// connection.
	Offline bool
	// Dial opens the connection to the mail server. Left nil it is the
	// outbound dialer, trusting only the host and port the settings name.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// TLS is the configuration of the connection; tests trust their own
	// certificate through it.
	TLS *tls.Config
}

// NewService returns the Service; Queue is set once the runner exists.
func NewService(pool *pgxpool.Pool, s Settings, files Files, log *slog.Logger) *Service {
	return &Service{pool: pool, settings: s, files: files, log: log}
}

// Status is what a person is told about sending to a Kindle.
type Status struct {
	// Available says that the administrator has set up a mail server.
	Available bool
	// Sender is the address the mail comes from, which the person approves
	// at Amazon.
	Sender string
	// Address is the person's Kindle address, or "".
	Address string
}

// server reads the mail server from the settings; false when it is not set up.
func (s *Service) server(ctx context.Context) (Server, bool, error) {
	if s.Offline {
		return Server{}, false, nil
	}
	var out Server
	var err error
	text := func(key string) string {
		if err != nil {
			return ""
		}
		var v string
		v, err = s.settings.Text(ctx, key)
		return v
	}
	out.Host, out.Sender, out.User, out.Security = text(settings.MailHost), text(settings.MailSender), text(settings.MailUser), text(settings.MailSecurity)
	port := text(settings.MailPort)
	if err != nil {
		return Server{}, false, err
	}
	if out.Host == "" || out.Sender == "" {
		return Server{}, false, nil
	}
	if out.Port, err = strconv.Atoi(port); err != nil {
		return Server{}, false, fmt.Errorf("mail.port: %w", err)
	}
	if out.User != "" {
		pw, _, err := s.settings.Secret(ctx, settings.MailPassword)
		if err != nil {
			return Server{}, false, err
		}
		out.Password = pw
	}
	out.TLS = s.TLS
	out.Dial = s.Dial
	if out.Dial == nil {
		out.Dial = outbound.NewDialer(outbound.Options{Trusted: []string{net.JoinHostPort(out.Host, port)}})
	}
	return out, true, nil
}

// Status says whether sending is set up and the person's address.
func (s *Service) Status(ctx context.Context, user uuid.UUID) (Status, error) {
	srv, ok, err := s.server(ctx)
	if err != nil {
		return Status{}, err
	}
	addr, err := s.Address(ctx, user)
	if err != nil {
		return Status{}, err
	}
	return Status{Available: ok, Sender: srv.Sender, Address: addr}, nil
}

// Address is the person's Kindle address, or "".
func (s *Service) Address(ctx context.Context, user uuid.UUID) (string, error) {
	a, err := sqlc.New(s.pool).GetKindleAddress(ctx, user)
	if err != nil || a == nil {
		return "", err
	}
	return *a, nil
}

// CleanAddress checks an address as typed and returns it as it is kept:
// one plain address at kindle.com. Mail goes to no other address, so the
// feature cannot be used to write to anyone.
func CleanAddress(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	a, err := mail.ParseAddress(v)
	if err != nil || a.Name != "" || a.Address != v {
		return "", ErrBadAddress
	}
	_, domain, _ := strings.Cut(v, "@")
	if domain != "kindle.com" && !strings.HasSuffix(domain, ".kindle.com") {
		return "", ErrBadAddress
	}
	return v, nil
}

// SetAddress keeps the person's Kindle address; "" takes it away.
func (s *Service) SetAddress(ctx context.Context, user uuid.UUID, address string) error {
	var keep *string
	if strings.TrimSpace(address) != "" {
		a, err := CleanAddress(address)
		if err != nil {
			return err
		}
		keep = &a
	}
	return sqlc.New(s.pool).SetKindleAddress(ctx, sqlc.SetKindleAddressParams{ID: user, Address: keep})
}

// Skipped is a book that was not queued, and why.
type Skipped struct {
	BookID uuid.UUID
	Reason string
}

// Result is what a request came to.
type Result struct {
	Queued  int
	Skipped []Skipped
}

// Request queues the EPUB of each book among ids that the scope sees, to be
// sent to the person's Kindle. A book that is not seen is answered like one
// without an EPUB.
func (s *Service) Request(ctx context.Context, scope library.Scope, ids []uuid.UUID) (Result, error) {
	if _, ok, err := s.server(ctx); err != nil || !ok {
		if err == nil {
			err = ErrNotConfigured
		}
		return Result{}, err
	}
	address, err := s.Address(ctx, scope.Viewer)
	if err != nil {
		return Result{}, err
	}
	if address == "" {
		return Result{}, ErrNoAddress
	}
	rows, err := sqlc.New(s.pool).ListKindleFiles(ctx, sqlc.ListKindleFilesParams{Ids: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Result{}, err
	}
	files := map[uuid.UUID]sqlc.ListKindleFilesRow{}
	for _, r := range rows {
		files[r.BookID] = r
	}
	var out Result
	var books []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		switch f, ok := files[id]; {
		case !ok:
			out.Skipped = append(out.Skipped, Skipped{id, NotAvailable})
		case f.SizeBytes > MaxAttachment:
			out.Skipped = append(out.Skipped, Skipped{id, TooLarge})
		default:
			books = append(books, id)
		}
	}
	if len(books) == 0 {
		return out, nil
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, books); err != nil {
			return err
		}
		for _, id := range books {
			f := files[id]
			sendID, err := q.QueueKindleSend(ctx, sqlc.QueueKindleSendParams{
				UserID: scope.Viewer, BookID: id, FileID: f.FileID, Title: f.Title, Address: address,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				out.Skipped = append(out.Skipped, Skipped{id, AlreadyWaits})
				continue
			}
			if err != nil {
				return err
			}
			if _, err := s.Queue.InsertTx(ctx, tx, SendArgs{SendID: sendID}, sendOpts); err != nil {
				return err
			}
			out.Queued++
		}
		return nil
	})
	return out, err
}

// Send is one send as the book's page lists it.
type Send struct {
	ID         uuid.UUID
	State      string
	Message    string
	Address    string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// ForBook lists the person's latest sends of the book, the newest first.
// The caller has checked that the person sees the book.
func (s *Service) ForBook(ctx context.Context, user, book uuid.UUID) ([]Send, error) {
	rows, err := sqlc.New(s.pool).ListBookKindleSends(ctx, sqlc.ListBookKindleSendsParams{BookID: book, UserID: user})
	if err != nil {
		return nil, err
	}
	out := make([]Send, len(rows))
	for i, r := range rows {
		out[i] = Send{ID: r.ID, State: r.State, Message: r.Message, Address: r.Address, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
	}
	return out, nil
}

// SendArgs is the job that sends one book.
type SendArgs struct {
	SendID uuid.UUID `json:"sendId"`
}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (SendArgs) Kind() string { return kind }

var sendOpts = jobs.InsertOpts{Queue: jobs.QueueDefault, MaxAttempts: maxAttempts}

// Worker runs SendArgs.
type Worker struct {
	river.WorkerDefaults[SendArgs]
	Service *Service
}

func (w *Worker) Timeout(*river.Job[SendArgs]) time.Duration { return sessionTimeout + time.Minute }

func (w *Worker) Work(ctx context.Context, job *river.Job[SendArgs]) error {
	return w.Service.Run(ctx, job.Args.SendID, job.Attempt >= job.MaxAttempts)
}

// Run sends the book. The send is taken (state sending) before the mail is
// handed over, so a job that runs again, after a crash or a retry that
// should not have happened, finds it taken and sends nothing. last says that
// an error is not retried.
func (s *Service) Run(ctx context.Context, id uuid.UUID, last bool) error {
	q := sqlc.New(s.pool)
	row, err := q.ClaimKindleSend(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		state, err := q.GetKindleSendState(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil && state == "sending" {
			return s.finish(ctx, id, "failed", "GOtome was stopped while this was being sent. It may have arrived; send it again if it did not.")
		}
		return err
	}
	if err != nil {
		return err
	}
	// The job may be cut off while it sends; what follows must still be
	// written down.
	settle := context.WithoutCancel(ctx)
	fail := func(message string) error { return s.finish(settle, id, "failed", message) }

	srv, ok, err := s.server(ctx)
	if err != nil {
		_ = q.RequeueKindleSend(settle, id)
		return err
	}
	if !ok {
		return fail("Sending to a Kindle is not set up on this server.")
	}
	role, err := q.GetUserRole(ctx, row.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail("The account is disabled or gone.")
	}
	if err != nil {
		_ = q.RequeueKindleSend(settle, id)
		return err
	}
	info, f, err := s.files.OpenFile(ctx, library.ScopeOf(auth.User{ID: row.UserID, Role: role}), row.FileID)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrFileGone) {
			return fail("The EPUB is no longer in the library.")
		}
		_ = q.RequeueKindleSend(settle, id)
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		_ = q.RequeueKindleSend(settle, id)
		return err
	}
	if stat.Size() > MaxAttachment {
		return fail("The EPUB is larger than the 50 MB Amazon takes by e-mail.")
	}
	_, domain, _ := strings.Cut(srv.Sender, "@")
	err = srv.Send(ctx, Message{
		To: row.Address, ID: id.String() + "@" + domain, Subject: row.Title,
		Body:     "Sent by GOtome: " + row.Title,
		Filename: filepath.Base(info.Name), ContentType: mime.TypeByExtension(".epub"), Attachment: f,
	})
	var sendErr *SendError
	switch {
	case err == nil:
		return s.finish(settle, id, "sent", "")
	case errors.As(err, &sendErr) && sendErr.Temporary && !last:
		s.log.Warn("could not send a book to a Kindle; will try again", "send", id, "error", err)
		if rq := q.RequeueKindleSend(settle, id); rq != nil {
			return rq
		}
		return err
	default:
		s.log.Warn("could not send a book to a Kindle", "send", id, "error", err)
		return fail(shorten(err.Error()))
	}
}

func shorten(s string) string {
	if len(s) > maxMessage {
		return strings.ToValidUTF8(s[:maxMessage], "") + "…"
	}
	return s
}

// finish records the outcome and tells the person, in one transaction.
func (s *Service) finish(ctx context.Context, id uuid.UUID, state, message string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).FinishKindleSend(ctx, sqlc.FinishKindleSendParams{ID: id, State: state, Message: message})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n := notify.New{Kind: notify.KindKindleSent, Data: map[string]any{"title": row.Title}, Link: "/books/" + row.BookID.String(), BookID: &row.BookID}
		if state == "failed" {
			n.Kind = notify.KindKindleFailed
			n.Data = map[string]any{"title": row.Title, "message": message}
		}
		_, err = notify.CreateTx(ctx, tx, row.UserID, n)
		return err
	})
}
