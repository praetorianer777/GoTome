// Package auth signs people in: accounts with passwords, and the sessions a
// browser holds afterwards.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// Roles, from most to least allowed.
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleReader = "reader"
)

const (
	// DefaultSessionTTL is how long a session lasts without being used.
	DefaultSessionTTL = 30 * 24 * time.Hour
	// touchInterval is how stale a session's last-seen time may get. Writing
	// it on every request would turn every read into a write.
	touchInterval = time.Hour

	MinPasswordLen = 8
	// MaxPasswordLen bounds what one login attempt makes argon2 chew on.
	MaxPasswordLen = 1024
	MaxUsernameLen = 64
)

var (
	// ErrSetupDone is returned by Setup once an account exists.
	ErrSetupDone = errors.New("setup has already been completed")
	// ErrInvalidCredentials covers a wrong password, an unknown user and a
	// disabled one alike, so an answer never says which names exist.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrNoSession is returned for a token that names no live session.
	ErrNoSession = errors.New("no such session")
	// ErrTaken is returned when the user name or e-mail address is in use.
	ErrTaken = errors.New("user name or e-mail address already in use")
)

// ValidationError says which fields of an account were refused and why, in
// words for the person who typed them.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "invalid account details" }

// User is an account as the rest of the application sees it.
type User struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Email    *string   `json:"email,omitempty"`
	Role     string    `json:"role"`
}

// Service signs people in and out.
type Service struct {
	pool   *pgxpool.Pool
	params PasswordParams
	ttl    time.Duration
	// now is the clock; tests move it.
	now func() time.Time
	// decoy is verified when the user does not exist, so that answer takes as
	// long as a wrong password does.
	decoy string
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool, params PasswordParams, ttl time.Duration) (*Service, error) {
	decoy, err := HashPassword("decoy", params)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, params: params, ttl: ttl, now: time.Now, decoy: decoy}, nil
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// SetupNeeded reports whether no account exists yet.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	n, err := sqlc.New(s.pool).CountUsers(ctx)
	return n == 0, err
}

// Setup creates the first account, an administrator. It works once: with any
// account present it returns ErrSetupDone.
func (s *Service) Setup(ctx context.Context, username, password string, email *string) (User, error) {
	username, email, err := validate(username, password, email)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return User{}, err
	}

	var created sqlc.User
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.LockSetup(ctx); err != nil {
			return err
		}
		n, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		created, err = q.CreateUser(ctx, sqlc.CreateUserParams{
			Username: username, Email: email, PasswordHash: &hash, Role: RoleAdmin,
		})
		return err
	})
	if err != nil {
		return User{}, taken(err)
	}
	return toUser(created), nil
}

// Login checks the password and opens a session. The token it returns is the
// only copy: the database keeps its hash.
func (s *Service) Login(ctx context.Context, username, password, userAgent string) (User, string, time.Time, error) {
	if len(password) > MaxPasswordLen {
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}
	q := sqlc.New(s.pool)
	row, err := q.GetUserByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(password, s.decoy)
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	hash := s.decoy
	if row.PasswordHash != nil {
		hash = *row.PasswordHash
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return User{}, "", time.Time{}, fmt.Errorf("user %s: %w", row.ID, err)
	}
	if !ok || row.PasswordHash == nil || row.DisabledAt != nil {
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}

	token, expires, err := s.openSession(ctx, q, row.ID, userAgent)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	return toUser(row), token, expires, nil
}

// OpenSession opens a session for a user who has just been created.
func (s *Service) OpenSession(ctx context.Context, userID uuid.UUID, userAgent string) (string, time.Time, error) {
	return s.openSession(ctx, sqlc.New(s.pool), userID, userAgent)
}

func (s *Service) openSession(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, userAgent string) (string, time.Time, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.now()
	expires := now.Add(s.ttl)

	// Expired sessions are swept here, on the one path that already writes.
	if _, err := q.DeleteExpiredSessions(ctx, now); err != nil {
		return "", time.Time{}, err
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hashToken(token), UserID: userID, UserAgent: strings.ToValidUTF8(userAgent, ""), ExpiresAt: expires,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Authenticate returns the user a token's session belongs to, or ErrNoSession.
// A session in use keeps being extended, so it ends after the TTL of not
// being used rather than the TTL after sign-in.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrNoSession
	}
	q := sqlc.New(s.pool)
	now := s.now()
	hash := hashToken(token)
	row, err := q.GetSessionUser(ctx, sqlc.GetSessionUserParams{TokenHash: hash, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoSession
	}
	if err != nil {
		return User{}, err
	}
	if now.Sub(row.SessionLastSeenAt) >= touchInterval {
		if err := q.TouchSession(ctx, sqlc.TouchSessionParams{TokenHash: hash, Now: now, ExpiresAt: now.Add(s.ttl)}); err != nil {
			return User{}, err
		}
	}
	return User{ID: row.ID, Username: row.Username, Email: row.Email, Role: row.Role}, nil
}

// Logout ends the session. An unknown token is not an error: the caller is
// signed out either way.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return sqlc.New(s.pool).DeleteSession(ctx, hashToken(token))
}

// SessionTTL is how long a session lasts without being used.
func (s *Service) SessionTTL() time.Duration { return s.ttl }

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func toUser(u sqlc.User) User {
	return User{ID: u.ID, Username: u.Username, Email: u.Email, Role: u.Role}
}

// taken turns a unique violation into ErrTaken.
func taken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

// validate normalises an account's details and refuses what cannot be stored.
func validate(username, password string, email *string) (string, *string, error) {
	fields := map[string]string{}

	username = strings.TrimSpace(username)
	switch {
	case username == "":
		fields["username"] = "Choose a user name."
	case len(username) > MaxUsernameLen:
		fields["username"] = fmt.Sprintf("A user name has at most %d characters.", MaxUsernameLen)
	case strings.ContainsFunc(username, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		fields["username"] = "A user name has no spaces in it."
	}

	switch {
	case len(password) < MinPasswordLen:
		fields["password"] = fmt.Sprintf("A password has at least %d characters.", MinPasswordLen)
	case len(password) > MaxPasswordLen:
		fields["password"] = fmt.Sprintf("A password has at most %d characters.", MaxPasswordLen)
	}

	if email != nil {
		trimmed := strings.TrimSpace(*email)
		switch at := strings.LastIndex(trimmed, "@"); {
		case trimmed == "":
			email = nil
		case at <= 0 || at == len(trimmed)-1 || len(trimmed) > 254 || strings.ContainsFunc(trimmed, unicode.IsSpace):
			fields["email"] = "That does not look like an e-mail address."
		default:
			email = &trimmed
		}
	}

	if len(fields) > 0 {
		return "", nil, &ValidationError{Fields: fields}
	}
	return username, email, nil
}
