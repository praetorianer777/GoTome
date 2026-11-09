package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// What an API key may be limited to. Each scope is a set of the permissions
// routes declare; a key never holds a permission its owner's role lacks.
const (
	ScopeRead     = "read"
	ScopeProgress = "progress"
	ScopeUpload   = "upload"
	ScopeEdit     = "edit"
	ScopeAdmin    = "admin"
)

// MaxAPIKeyNameLen bounds a key's name; MaxAPIKeys the keys one person holds.
const (
	MaxAPIKeyNameLen = 80
	MaxAPIKeys       = 50
)

const (
	apiKeyPrefix = "gotome_"
	// 32 random bytes in base64url: 256 bits, which no one guesses.
	apiKeyRandom    = 43
	apiKeyShownLen  = len(apiKeyPrefix) + 5
	apiKeyTokenSize = len(apiKeyPrefix) + apiKeyRandom
)

var scopePermissions = map[string][]Permission{
	ScopeRead:     {LibraryRead},
	ScopeProgress: {PersonalManage},
	ScopeUpload:   {BooksUpload},
	ScopeEdit:     {MetadataEdit, IndexRebuild},
	ScopeAdmin:    {UsersManage, SettingsManage, StorageManage},
}

// Scopes lists the scopes a key may be made with, narrowest first.
func Scopes() []string {
	return []string{ScopeRead, ScopeProgress, ScopeUpload, ScopeEdit, ScopeAdmin}
}

// ScopesAllow reports whether one of the scopes holds the permission. A
// scope this build does not know holds nothing.
func ScopesAllow(scopes []string, p Permission) bool {
	for _, scope := range scopes {
		if slices.Contains(scopePermissions[scope], p) {
			return true
		}
	}
	return false
}

// ErrNoAPIKey is returned for a key that is not the user's.
var ErrNoAPIKey = errors.New("no such API key")

// APIKey is a key's record; the key itself is shown once, on creation.
type APIKey struct {
	ID         uuid.UUID
	Name       string
	Prefix     string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
}

// APIKeyUse is what a request signed in with a key carries besides its user.
type APIKeyUse struct {
	ID     uuid.UUID
	Scopes []string
}

// CreateAPIKey makes a key for the user and returns it. The key is the only
// copy: the database keeps its hash. A scope the user's role does not fully
// hold is refused, so a key is never made that its owner could not use.
func (s *Service) CreateAPIKey(ctx context.Context, user User, name string, scopes []string, expiresAt *time.Time) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	switch {
	case name == "":
		fields["name"] = "Give the key a name."
	case utf8.RuneCountInString(name) > MaxAPIKeyNameLen:
		fields["name"] = "A key's name has at most 80 characters."
	}
	var chosen []string
	for _, scope := range Scopes() {
		if slices.Contains(scopes, scope) {
			chosen = append(chosen, scope)
		}
	}
	switch {
	case len(chosen) != len(dedupe(scopes)):
		fields["scopes"] = "One of the scopes is not known."
	case len(chosen) == 0:
		fields["scopes"] = "Choose at least one scope."
	default:
		for _, scope := range chosen {
			for _, p := range scopePermissions[scope] {
				if !Allows(user.Role, p) {
					fields["scopes"] = "Your role does not hold the scope " + scope + "."
				}
			}
		}
	}
	if expiresAt != nil && !expiresAt.After(s.now()) {
		fields["expiresAt"] = "The expiry must be in the future."
	}
	if len(fields) > 0 {
		return APIKey{}, "", &ValidationError{Fields: fields}
	}

	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return APIKey{}, "", err
	}
	token := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	row, err := sqlc.New(s.pool).CreateAPIKey(ctx, sqlc.CreateAPIKeyParams{
		UserID: user.ID, Name: name, KeyHash: hashToken(token), KeyPrefix: token[:apiKeyShownLen],
		Scopes: chosen, ExpiresAt: expiresAt, MaxKeys: MaxAPIKeys,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, "", &ValidationError{Fields: map[string]string{"name": "At most 50 API keys per person. Revoke one first."}}
	}
	if err != nil {
		return APIKey{}, "", err
	}
	return APIKey{
		ID: row.ID, Name: row.Name, Prefix: row.KeyPrefix, Scopes: row.Scopes,
		CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt, ExpiresAt: row.ExpiresAt,
	}, token, nil
}

func dedupe(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// APIKeys lists the user's keys, the oldest first, expired ones included.
func (s *Service) APIKeys(ctx context.Context, userID uuid.UUID) ([]APIKey, error) {
	rows, err := sqlc.New(s.pool).ListUserAPIKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]APIKey, len(rows))
	for i, row := range rows {
		out[i] = APIKey{
			ID: row.ID, Name: row.Name, Prefix: row.KeyPrefix, Scopes: row.Scopes,
			CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt, ExpiresAt: row.ExpiresAt,
		}
	}
	return out, nil
}

// DeleteAPIKey revokes one of the user's keys: it signs nobody in from then
// on. ErrNoAPIKey covers one that is not theirs.
func (s *Service) DeleteAPIKey(ctx context.Context, userID, keyID uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteUserAPIKey(ctx, sqlc.DeleteUserAPIKeyParams{ID: keyID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoAPIKey
	}
	return nil
}

// AuthenticateAPIKey returns the user a key signs in, with the key's scopes.
// A wrong, revoked or expired key and the key of a disabled account are all
// ErrInvalidCredentials. The user carries their role as it is now, so a
// demotion narrows their keys at once.
func (s *Service) AuthenticateAPIKey(ctx context.Context, token string) (User, APIKeyUse, error) {
	if !LooksLikeAPIKey(token) {
		return User{}, APIKeyUse{}, ErrInvalidCredentials
	}
	q := sqlc.New(s.pool)
	now := s.now()
	row, err := q.GetAPIKeyUser(ctx, sqlc.GetAPIKeyUserParams{KeyHash: hashToken(token), Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, APIKeyUse{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, APIKeyUse{}, err
	}
	if row.KeyLastUsedAt == nil || now.Sub(*row.KeyLastUsedAt) >= touchInterval {
		if err := q.TouchAPIKey(ctx, sqlc.TouchAPIKeyParams{ID: row.KeyID, Now: now}); err != nil {
			return User{}, APIKeyUse{}, err
		}
	}
	return User{ID: row.ID, Username: row.Username, Email: row.Email, Role: row.Role}, APIKeyUse{ID: row.KeyID, Scopes: row.KeyScopes}, nil
}

// LooksLikeAPIKey says whether the string has an API key's shape: what does
// not cannot be one, and is no guess worth counting.
func LooksLikeAPIKey(token string) bool {
	if len(token) != apiKeyTokenSize || !strings.HasPrefix(token, apiKeyPrefix) {
		return false
	}
	for _, c := range token[len(apiKeyPrefix):] {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
