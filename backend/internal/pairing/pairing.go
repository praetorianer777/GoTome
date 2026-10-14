// Package pairing adds an e-reader to an account without anyone typing its
// token (#218). The device's app asks with a user name and is answered with a
// secret and a short code; the person sees the request with the same code on
// their profile and accepts or declines it; the app polls with its secret and,
// once accepted, collects the token of a new device exactly once.
package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// Lifetime is how long a request may be accepted and its token collected.
const Lifetime = 10 * time.Minute

// MaxWaiting is how many requests one account has waiting at most; more are
// answered as usual and never shown, so nobody fills a profile with them.
const MaxWaiting = 3

// codeDigits is the length of the code the app shows and the profile repeats.
const codeDigits = 4

// What the app learns when it asks about its request.
const (
	StatePending  = "pending"
	StateAccepted = "accepted"
	StateDeclined = "declined"
	// StateExpired is a request whose time is up, or whose token was
	// collected already.
	StateExpired = "expired"
)

// ErrNotFound is a request that does not exist, is not the user's, asked
// with the wrong secret, or no longer waits.
var ErrNotFound = errors.New("no such device request")

// Service keeps the requests.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Ticket is what the app is answered with: the request, the secret it polls
// with, and the code it shows.
type Ticket struct {
	ID        uuid.UUID
	Secret    string
	Code      string
	ExpiresAt time.Time
}

// Request records a device asking to be added to the account of the user
// name. Whether the name is anyone's, or the account takes no more requests,
// does not change the answer: such a request waits until it expires and is
// shown to no one. The account's person is told of a request they see.
func (s *Service) Request(ctx context.Context, username, deviceName, address string) (Ticket, error) {
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" || utf8.RuneCountInString(deviceName) > auth.MaxDeviceNameLen {
		return Ticket{}, &auth.ValidationError{Fields: map[string]string{"deviceName": "A device's name has 1 to 80 characters."}}
	}
	secret, err := newSecret()
	if err != nil {
		return Ticket{}, err
	}
	code, err := newCode()
	if err != nil {
		return Ticket{}, err
	}
	var ticket Ticket
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.DropOldDeviceRequests(ctx); err != nil {
			return err
		}
		var user *uuid.UUID
		id, err := q.GetActiveUserIDByName(ctx, strings.TrimSpace(username))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			waiting, err := q.CountWaitingDeviceRequests(ctx, &id)
			if err != nil {
				return err
			}
			if waiting < MaxWaiting {
				user = &id
			}
		}
		row, err := q.CreateDeviceRequest(ctx, sqlc.CreateDeviceRequestParams{
			UserID: user, DeviceName: deviceName, Code: code, SecretHash: hash(secret),
			Address: address, Seconds: int32(Lifetime / time.Second),
		})
		if err != nil {
			return err
		}
		ticket = Ticket{ID: row.ID, Secret: secret, Code: code, ExpiresAt: row.ExpiresAt}
		if user == nil {
			return nil
		}
		_, err = notify.CreateTx(ctx, tx, *user, notify.New{
			Kind: notify.KindDeviceRequested, Link: "/profile",
			Data: map[string]any{"device": deviceName, "code": code},
		})
		return err
	})
	return ticket, err
}

// Waiting is a request waiting for its person to decide.
type Waiting struct {
	ID         uuid.UUID
	DeviceName string
	Code       string
	Address    string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// Waiting lists the user's requests that may still be decided, the oldest
// first.
func (s *Service) Waiting(ctx context.Context, userID uuid.UUID) ([]Waiting, error) {
	rows, err := sqlc.New(s.pool).ListWaitingDeviceRequests(ctx, &userID)
	if err != nil {
		return nil, err
	}
	out := make([]Waiting, len(rows))
	for i, r := range rows {
		out[i] = Waiting{ID: r.ID, DeviceName: r.DeviceName, Code: r.Code, Address: r.Address, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt}
	}
	return out, nil
}

// Decide accepts or declines one of the user's waiting requests.
func (s *Service) Decide(ctx context.Context, userID, id uuid.UUID, accept bool) error {
	state := StateDeclined
	if accept {
		state = StateAccepted
	}
	n, err := sqlc.New(s.pool).DecideDeviceRequest(ctx, sqlc.DecideDeviceRequestParams{ID: id, UserID: &userID, State: state})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Answer is what the app is told about its request: the state, and once
// accepted the user name and the token, which it gets this once.
type Answer struct {
	State    string
	Username string
	Token    string
}

// Collect answers the app that asks about its request with its secret. An
// accepted request makes the device then, under the name it asked with, and
// hands over its token; asked again, the request has expired.
func (s *Service) Collect(ctx context.Context, id uuid.UUID, secret string) (Answer, error) {
	var answer Answer
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		req, err := q.TakeDeviceRequest(ctx, sqlc.TakeDeviceRequestParams{ID: id, SecretHash: hash(secret)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case req.State == "collected" || req.Expired:
			answer.State = StateExpired
		case req.State == StateAccepted && req.UserID != nil:
			user, err := q.GetUser(ctx, *req.UserID)
			if err != nil {
				return err
			}
			_, token, err := auth.CreateDeviceTx(ctx, tx, *req.UserID, req.DeviceName)
			if err != nil {
				return err
			}
			answer = Answer{State: StateAccepted, Username: user.Username, Token: token}
			return q.SetDeviceRequestState(ctx, sqlc.SetDeviceRequestStateParams{ID: id, State: "collected"})
		default:
			answer.State = req.State
		}
		return nil
	})
	return answer, err
}

func hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func newSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", codeDigits, n.Int64()), nil
}
