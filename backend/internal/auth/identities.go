package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

var (
	// ErrNoAccount is an identity linked to no account, where none may be
	// made or found for it.
	ErrNoAccount = errors.New("no account is linked to this identity")
	// ErrNoRole is a person the identity provider's groups give no role.
	ErrNoRole = errors.New("the identity provider gives this person no role")
	// ErrDisabled is a disabled account signing in through a provider,
	// which has already said who they are.
	ErrDisabled = errors.New("the account is disabled")
	// ErrIdentityTaken is an identity already linked to another account.
	ErrIdentityTaken = errors.New("the identity is linked to another account")
	// ErrNoIdentity is an identity that is not the account's.
	ErrNoIdentity = errors.New("no such identity")
	// ErrLastSignIn is unlinking the one way an account has left to sign in.
	ErrLastSignIn = errors.New("the account would have no way left to sign in")
	// ErrPasswordsOff is a correct password while signing in with one is
	// turned off.
	ErrPasswordsOff = errors.New("signing in with a password is turned off")
)

// maxUsernameTries is how many numbered variants of a name a new account
// from an identity provider tries before it gives up on that name.
const maxUsernameTries = 20

// Identity is a person as an identity provider vouches for them.
type Identity struct {
	Issuer  string
	Subject string
	// Username is the name the provider would give them, which may be
	// taken or unusable here.
	Username      string
	Email         *string
	EmailVerified bool
}

// IdentityPolicy is what a sign-in through a provider may do besides
// signing in the account an identity is linked to.
type IdentityPolicy struct {
	// Signup makes an account for an identity linked to none.
	Signup bool
	// LinkByEmail links an identity to the account of the address the
	// provider says it verified.
	LinkByEmail bool
	// Role is what the provider's groups make the person, "" nothing. A new
	// account gets it; with Sync, so does an existing one at every sign-in.
	Role string
	Sync bool
}

// LinkedIdentity is one of an account's identities, as its owner sees it.
type LinkedIdentity struct {
	ID         uuid.UUID
	Issuer     string
	Email      *string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// SignInIdentity finds or makes the account of an identity and opens a
// session for it.
func (s *Service) SignInIdentity(ctx context.Context, id Identity, p IdentityPolicy, userAgent string) (User, string, error) {
	var user sqlc.User
	var token string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		var admins []uuid.UUID
		if p.Sync {
			var err error
			if admins, err = q.LockActiveAdmins(ctx); err != nil {
				return err
			}
		}
		var identityID uuid.UUID
		row, err := q.GetIdentityUser(ctx, sqlc.GetIdentityUserParams{Issuer: id.Issuer, Subject: id.Subject})
		switch {
		case err == nil:
			user, identityID = sqlc.User{
				ID: row.ID, Username: row.Username, Email: row.Email, PasswordHash: row.PasswordHash, Role: row.Role,
				QuotaBytes: row.QuotaBytes, DisabledAt: row.DisabledAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}, row.IdentityID
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		default:
			if user, err = s.accountFor(ctx, q, id, p); err != nil {
				return err
			}
			if identityID, err = q.CreateIdentity(ctx, sqlc.CreateIdentityParams{
				UserID: user.ID, Issuer: id.Issuer, Subject: id.Subject, Email: id.Email,
			}); err != nil {
				return err
			}
		}
		if user.DisabledAt != nil {
			return ErrDisabled
		}
		if p.Sync && p.Role != user.Role {
			if p.Role == "" {
				return ErrNoRole
			}
			// The provider may not take away the last administrator: whoever
			// is that must still be able to sign in and hand it on.
			lastAdmin := user.Role == RoleAdmin && len(admins) == 1 && admins[0] == user.ID
			if !lastAdmin {
				if user, err = q.SetRole(ctx, sqlc.SetRoleParams{ID: user.ID, Role: p.Role}); err != nil {
					return err
				}
			}
		}
		if err := q.TouchIdentity(ctx, sqlc.TouchIdentityParams{ID: identityID, Email: id.Email}); err != nil {
			return err
		}
		token, _, err = s.openSession(ctx, q, user.ID, userAgent)
		return err
	})
	if err != nil {
		return User{}, "", err
	}
	return toUser(user), token, nil
}

// accountFor is the account an identity linked to none gets: the one of its
// verified address, or a new one.
func (s *Service) accountFor(ctx context.Context, q *sqlc.Queries, id Identity, p IdentityPolicy) (sqlc.User, error) {
	if p.LinkByEmail && id.EmailVerified && id.Email != nil {
		row, err := q.GetUserByEmail(ctx, id.Email)
		if err == nil {
			return row, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, err
		}
	}
	if !p.Signup {
		return sqlc.User{}, ErrNoAccount
	}
	if p.Role == "" {
		return sqlc.User{}, ErrNoRole
	}
	username, err := freeUsername(ctx, q, id)
	if err != nil {
		return sqlc.User{}, err
	}
	email := id.Email
	if email != nil {
		// Another account's address stays that account's.
		if _, err := q.GetUserByEmail(ctx, email); err == nil {
			email = nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, err
		}
	}
	return q.CreateUser(ctx, sqlc.CreateUserParams{Username: username, Email: email, Role: p.Role})
}

// freeUsername is the first name not taken among the provider's, the
// address's local part and "user", each as it is or numbered.
func freeUsername(ctx context.Context, q *sqlc.Queries, id Identity) (string, error) {
	bases := []string{id.Username}
	if id.Email != nil {
		local, _, _ := strings.Cut(*id.Email, "@")
		bases = append(bases, local)
	}
	for _, base := range append(bases, "user") {
		base, msg := checkUsername(base)
		if msg != "" {
			continue
		}
		for n := 1; n <= maxUsernameTries; n++ {
			name := base
			if n > 1 {
				suffix := fmt.Sprintf("-%d", n)
				name = base[:min(len(base), MaxUsernameLen-len(suffix))] + suffix
			}
			taken, err := q.UsernameTaken(ctx, name)
			if err != nil {
				return "", err
			}
			if !taken {
				return name, nil
			}
		}
	}
	return "", ErrTaken
}

// LinkIdentity links an identity to a signed-in person's account.
func (s *Service) LinkIdentity(ctx context.Context, userID uuid.UUID, id Identity) error {
	q := sqlc.New(s.pool)
	_, err := q.CreateIdentity(ctx, sqlc.CreateIdentityParams{UserID: userID, Issuer: id.Issuer, Subject: id.Subject, Email: id.Email})
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	row, err := q.GetIdentityUser(ctx, sqlc.GetIdentityUserParams{Issuer: id.Issuer, Subject: id.Subject})
	if err != nil {
		return err
	}
	if row.ID != userID {
		return ErrIdentityTaken
	}
	return nil
}

// Identities lists the identities linked to an account.
func (s *Service) Identities(ctx context.Context, userID uuid.UUID) ([]LinkedIdentity, error) {
	rows, err := sqlc.New(s.pool).ListUserIdentities(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]LinkedIdentity, len(rows))
	for i, row := range rows {
		out[i] = LinkedIdentity{ID: row.ID, Issuer: row.Issuer, Email: row.Email, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt}
	}
	return out, nil
}

// UnlinkIdentity removes one of the account's identities, unless it is the
// only way left to sign in: ErrLastSignIn.
func (s *Service) UnlinkIdentity(ctx context.Context, userID, identityID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		ids, err := q.ListUserIdentities(ctx, userID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(ids, func(r sqlc.ListUserIdentitiesRow) bool { return r.ID == identityID }) {
			return ErrNoIdentity
		}
		if len(ids) == 1 && (row.PasswordHash == nil || s.passwordsOff(ctx)) {
			return ErrLastSignIn
		}
		_, err = q.DeleteUserIdentity(ctx, sqlc.DeleteUserIdentityParams{ID: identityID, UserID: userID})
		return err
	})
}

// PasswordsAllowed says whether the sign-in page offers a password: always,
// unless they are turned off and an administrator can sign in through an
// identity provider.
func (s *Service) PasswordsAllowed(ctx context.Context) (bool, error) {
	if !s.passwordsOff(ctx) {
		return true, nil
	}
	n, err := sqlc.New(s.pool).ActiveAdminsWithIdentity(ctx)
	return n == 0, err
}

// passwordLoginRefused says whether a password, correct for the account,
// must still be refused.
func (s *Service) passwordLoginRefused(ctx context.Context, q *sqlc.Queries, role string) (bool, error) {
	if !s.passwordsOff(ctx) {
		return false, nil
	}
	if role != RoleAdmin {
		return true, nil
	}
	n, err := q.ActiveAdminsWithIdentity(ctx)
	return n > 0, err
}

func (s *Service) passwordsOff(ctx context.Context) bool {
	return s.PasswordsOff != nil && s.PasswordsOff(ctx)
}
