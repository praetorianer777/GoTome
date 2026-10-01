package auth

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

var (
	// ErrNoUser is an account that does not exist.
	ErrNoUser = errors.New("no such user")
	// ErrLastAdmin is a change that would leave nobody to administer GOtome:
	// demoting or disabling the last administrator who can sign in.
	ErrLastAdmin = errors.New("the last administrator cannot be demoted or disabled")
	// ErrWrongPassword is a current password that does not match, when
	// someone changes their own.
	ErrWrongPassword = errors.New("the current password is wrong")
)

// Account is a user as administration sees one.
type Account struct {
	User
	Disabled  bool
	CreatedAt time.Time
	// LastSeenAt is when one of the account's sessions was last used; nil if
	// it has none.
	LastSeenAt *time.Time
	// Sessions is how many live sessions the account has.
	Sessions int
	// QuotaBytes is how much the account may upload; nil is no limit.
	QuotaBytes *int64
	// UsedBytes is what its uploads take up now.
	UsedBytes int64
}

// NewAccount is what creating an account takes.
type NewAccount struct {
	Username string
	Password string
	Email    *string
	Role     string
}

// AccountChanges is what an administrator may change; nil leaves a field.
type AccountChanges struct {
	Role *string
	// Email is the new address; an empty one removes it.
	Email    *string
	Disabled *bool
	// Password replaces the account's, and ends all its sessions.
	Password *string
}

// Session is one of an account's sessions, as its owner sees it.
type Session struct {
	ID         uuid.UUID
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	// Current is the session the request came with.
	Current bool
}

func validRole(role string) bool {
	return slices.Contains([]string{RoleAdmin, RoleEditor, RoleReader}, role)
}

// Accounts lists every account by user name.
func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	q := sqlc.New(s.pool)
	rows, err := q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	use, err := q.ListSessionUse(ctx, s.now())
	if err != nil {
		return nil, err
	}
	byUser := map[uuid.UUID]sqlc.ListSessionUseRow{}
	for _, u := range use {
		byUser[u.UserID] = u
	}
	storage, err := q.ListStorageUse(ctx)
	if err != nil {
		return nil, err
	}
	used := map[uuid.UUID]int64{}
	for _, u := range storage {
		if u.UserID != nil {
			used[*u.UserID] = u.UsedBytes
		}
	}
	out := make([]Account, len(rows))
	for i, row := range rows {
		out[i] = toAccount(row)
		out[i].UsedBytes = used[row.ID]
		if u, ok := byUser[row.ID]; ok {
			out[i].LastSeenAt = &u.LastSeenAt
			out[i].Sessions = int(u.Live)
		}
	}
	return out, nil
}

func toAccount(row sqlc.User) Account {
	return Account{User: toUser(row), Disabled: row.DisabledAt != nil, CreatedAt: row.CreatedAt, QuotaBytes: row.QuotaBytes}
}

// SetQuota sets how much the account may upload; nil is no limit. What it
// has uploaded already stays, even when that is more.
func (s *Service) SetQuota(ctx context.Context, id uuid.UUID, quotaBytes *int64) (Account, error) {
	if quotaBytes != nil && *quotaBytes < 0 {
		return Account{}, &ValidationError{Fields: map[string]string{"quotaBytes": "A quota is zero or more."}}
	}
	row, err := sqlc.New(s.pool).SetQuota(ctx, sqlc.SetQuotaParams{ID: id, QuotaBytes: quotaBytes})
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNoUser
	}
	if err != nil {
		return Account{}, err
	}
	return toAccount(row), nil
}

// CreateAccount adds an account with a password.
func (s *Service) CreateAccount(ctx context.Context, in NewAccount) (Account, error) {
	username, email, err := validate(in.Username, in.Password, in.Email)
	var invalid *ValidationError
	if !validRole(in.Role) {
		if !errors.As(err, &invalid) {
			invalid = &ValidationError{Fields: map[string]string{}}
		}
		invalid.Fields["role"] = "Choose administrator, editor or reader."
		err = invalid
	}
	if err != nil {
		return Account{}, err
	}
	hash, err := HashPassword(in.Password, s.params)
	if err != nil {
		return Account{}, err
	}
	row, err := sqlc.New(s.pool).CreateUser(ctx, sqlc.CreateUserParams{
		Username: username, Email: email, PasswordHash: &hash, Role: in.Role,
	})
	if err != nil {
		return Account{}, taken(err)
	}
	return toAccount(row), nil
}

// UpdateAccount changes an account. Disabling it, or giving it a new
// password, ends its sessions; ErrLastAdmin keeps one administrator who can
// sign in.
func (s *Service) UpdateAccount(ctx context.Context, id uuid.UUID, c AccountChanges) (Account, error) {
	fields := map[string]string{}
	if c.Role != nil && !validRole(*c.Role) {
		fields["role"] = "Choose administrator, editor or reader."
	}
	email, msg := checkEmail(c.Email)
	if msg != "" {
		fields["email"] = msg
	}
	var hash *string
	if c.Password != nil {
		if msg := checkPassword(*c.Password); msg != "" {
			fields["password"] = msg
		} else {
			h, err := HashPassword(*c.Password, s.params)
			if err != nil {
				return Account{}, err
			}
			hash = &h
		}
	}
	if len(fields) > 0 {
		return Account{}, &ValidationError{Fields: fields}
	}

	var updated sqlc.User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		admins, err := q.LockActiveAdmins(ctx)
		if err != nil {
			return err
		}
		row, err := q.GetUser(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		params := sqlc.UpdateUserParams{ID: id, Role: row.Role, Email: row.Email, DisabledAt: row.DisabledAt}
		if c.Role != nil {
			params.Role = *c.Role
		}
		if c.Email != nil {
			params.Email = email
		}
		if c.Disabled != nil {
			switch {
			case *c.Disabled && row.DisabledAt == nil:
				now := s.now()
				params.DisabledAt = &now
			case !*c.Disabled:
				params.DisabledAt = nil
			}
		}
		stillAdmin := params.Role == RoleAdmin && params.DisabledAt == nil
		if slices.Contains(admins, id) && len(admins) == 1 && !stillAdmin {
			return ErrLastAdmin
		}
		if updated, err = q.UpdateUser(ctx, params); err != nil {
			return taken(err)
		}
		if hash != nil {
			if err := q.SetPassword(ctx, sqlc.SetPasswordParams{ID: id, PasswordHash: hash}); err != nil {
				return err
			}
		}
		if hash != nil || params.DisabledAt != nil {
			return q.DeleteUserSessions(ctx, sqlc.DeleteUserSessionsParams{UserID: id})
		}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return toAccount(updated), nil
}

// EndSessions signs the account out everywhere.
func (s *Service) EndSessions(ctx context.Context, id uuid.UUID) error {
	if _, err := sqlc.New(s.pool).GetUser(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return ErrNoUser
	} else if err != nil {
		return err
	}
	return sqlc.New(s.pool).DeleteUserSessions(ctx, sqlc.DeleteUserSessionsParams{UserID: id})
}

// ChangePassword changes the user's own password, after checking the one
// they have now, and ends every session but the one they are using.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentToken, oldPassword, newPassword string) error {
	if msg := checkPassword(newPassword); msg != "" {
		return &ValidationError{Fields: map[string]string{"newPassword": msg}}
	}
	q := sqlc.New(s.pool)
	row, err := q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if row.PasswordHash == nil || len(oldPassword) > MaxPasswordLen {
		return ErrWrongPassword
	}
	ok, err := VerifyPassword(oldPassword, *row.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWrongPassword
	}
	hash, err := HashPassword(newPassword, s.params)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.SetPassword(ctx, sqlc.SetPasswordParams{ID: userID, PasswordHash: &hash}); err != nil {
			return err
		}
		return q.DeleteUserSessions(ctx, sqlc.DeleteUserSessionsParams{UserID: userID, Keep: hashToken(currentToken)})
	})
}

// Sessions lists the user's live sessions, the one in use marked.
func (s *Service) Sessions(ctx context.Context, userID uuid.UUID, currentToken string) ([]Session, error) {
	rows, err := sqlc.New(s.pool).ListUserSessions(ctx, sqlc.ListUserSessionsParams{UserID: userID, Now: s.now()})
	if err != nil {
		return nil, err
	}
	current := hashToken(currentToken)
	out := make([]Session, len(rows))
	for i, row := range rows {
		out[i] = Session{
			ID: row.ID, UserAgent: row.UserAgent, CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt,
			Current: bytes.Equal(row.TokenHash, current),
		}
	}
	return out, nil
}

// EndSession ends one of the user's own sessions. ErrNoSession covers one
// that is not theirs.
func (s *Service) EndSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteUserSession(ctx, sqlc.DeleteUserSessionParams{ID: sessionID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoSession
	}
	return nil
}
