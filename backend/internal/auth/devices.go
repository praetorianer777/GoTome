package auth

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// MaxDeviceNameLen bounds a device's name.
const MaxDeviceNameLen = 80

// tokenAlphabet has no letters easily taken for one another, as a token is
// typed on an e-reader's keyboard: no 0, 1, l or o.
const tokenAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// tokenGroups of tokenGroupLen letters make a token: 20 of 32 letters are
// 100 bits, which no one guesses through a limiter.
const (
	tokenGroups   = 5
	tokenGroupLen = 4
)

// ErrNoDevice is returned for a device that is not the user's.
var ErrNoDevice = errors.New("no such device")

// Device is an e-reader a person signs in with a token of its own.
type Device struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateDevice makes a device for the user and returns the token it signs
// in with. The token is the only copy: the database keeps its hashes.
func (s *Service) CreateDevice(ctx context.Context, userID uuid.UUID, name string) (Device, string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return Device{}, "", &ValidationError{Fields: map[string]string{"name": "Give the device a name."}}
	case utf8.RuneCountInString(name) > MaxDeviceNameLen:
		return Device{}, "", &ValidationError{Fields: map[string]string{"name": "A device's name has at most 80 characters."}}
	}
	return createDevice(ctx, sqlc.New(s.pool), userID, name)
}

// CreateDeviceTx is CreateDevice in the transaction, for a device made as
// part of another change: a request accepted and collected.
func CreateDeviceTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, name string) (Device, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxDeviceNameLen {
		return Device{}, "", &ValidationError{Fields: map[string]string{"name": "A device's name has 1 to 80 characters."}}
	}
	return createDevice(ctx, sqlc.New(tx), userID, name)
}

func createDevice(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, name string) (Device, string, error) {
	token, err := newDeviceToken()
	if err != nil {
		return Device{}, "", err
	}
	row, err := q.CreateDevice(ctx, sqlc.CreateDeviceParams{
		UserID: userID, Name: name, TokenHash: hashToken(token), SyncKeyHash: hashToken(SyncKey(token)),
	})
	if err != nil {
		return Device{}, "", err
	}
	return Device{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt}, token, nil
}

// Devices lists the user's devices, the oldest first.
func (s *Service) Devices(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := sqlc.New(s.pool).ListUserDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Device, len(rows))
	for i, row := range rows {
		out[i] = Device{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt}
	}
	return out, nil
}

// DeleteDevice revokes one of the user's devices: its token signs nobody in
// from then on. ErrNoDevice covers one that is not theirs.
func (s *Service) DeleteDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteUserDevice(ctx, sqlc.DeleteUserDeviceParams{ID: deviceID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoDevice
	}
	return nil
}

// AuthenticateDevice returns the user a device token signs in under the user
// name, and the device. A wrong token, another user's, a revoked one or a
// disabled account are all ErrInvalidCredentials.
func (s *Service) AuthenticateDevice(ctx context.Context, username, token string) (User, uuid.UUID, error) {
	if token == "" || len(token) > MaxPasswordLen {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	q := sqlc.New(s.pool)
	row, err := q.GetDeviceUser(ctx, sqlc.GetDeviceUserParams{TokenHash: hashToken(NormalizeDeviceToken(token)), Username: strings.TrimSpace(username)})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, uuid.Nil, err
	}
	now := s.now()
	if row.DeviceLastUsedAt == nil || now.Sub(*row.DeviceLastUsedAt) >= touchInterval {
		if err := q.TouchDevice(ctx, sqlc.TouchDeviceParams{ID: row.DeviceID, Now: now}); err != nil {
			return User{}, uuid.Nil, err
		}
	}
	return User{ID: row.ID, Username: row.Username, Email: row.Email, Role: row.Role}, row.DeviceID, nil
}

// AuthenticateDeviceToken is AuthenticateDevice by the token alone, which
// a Kobo reader carries in the address it syncs with (#230): tokens are
// unique, and the reader sends no user name.
func (s *Service) AuthenticateDeviceToken(ctx context.Context, _, token string) (User, uuid.UUID, error) {
	if token == "" || len(token) > MaxPasswordLen {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	q := sqlc.New(s.pool)
	row, err := q.GetDeviceUserByToken(ctx, hashToken(NormalizeDeviceToken(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, uuid.Nil, err
	}
	now := s.now()
	if row.DeviceLastUsedAt == nil || now.Sub(*row.DeviceLastUsedAt) >= touchInterval {
		if err := q.TouchDevice(ctx, sqlc.TouchDeviceParams{ID: row.DeviceID, Now: now}); err != nil {
			return User{}, uuid.Nil, err
		}
	}
	return User{ID: row.ID, Username: row.Username, Email: row.Email, Role: row.Role}, row.DeviceID, nil
}

// SyncKey is what KOReader's progress sync sends for a password: its MD5 in
// hex.
func SyncKey(token string) string {
	sum := md5.Sum([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LooksLikeDeviceToken says whether the string has a device token's shape:
// what does not cannot be one, and is no guess worth counting.
func LooksLikeDeviceToken(token string) bool {
	token = NormalizeDeviceToken(token)
	if len(token) != tokenGroups*(tokenGroupLen+1)-1 {
		return false
	}
	for i, c := range token {
		if (i+1)%(tokenGroupLen+1) == 0 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune(tokenAlphabet, c) {
			return false
		}
	}
	return true
}

// NormalizeDeviceToken takes a token as typed: in capitals, or with spaces
// where the dashes go.
func NormalizeDeviceToken(token string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(token)), " ", "-")
}

func newDeviceToken() (string, error) {
	var raw [tokenGroups * tokenGroupLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range raw {
		if i > 0 && i%tokenGroupLen == 0 {
			b.WriteByte('-')
		}
		// 256 is a multiple of the alphabet's 32, so every letter is as likely.
		b.WriteByte(tokenAlphabet[int(c)%len(tokenAlphabet)])
	}
	return b.String(), nil
}

// AuthenticateSyncKey is AuthenticateDevice for KOReader's progress sync,
// which sends the MD5 of the token in hex instead of the token.
func (s *Service) AuthenticateSyncKey(ctx context.Context, username, key string) (User, uuid.UUID, error) {
	if len(key) != md5.Size*2 {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	row, err := sqlc.New(s.pool).GetSyncKeyUser(ctx, sqlc.GetSyncKeyUserParams{
		SyncKeyHash: hashToken(strings.ToLower(key)), Username: strings.TrimSpace(username),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, uuid.Nil, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, uuid.Nil, err
	}
	return User{ID: row.ID, Username: row.Username, Email: row.Email, Role: row.Role}, row.DeviceID, nil
}
