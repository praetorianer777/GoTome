// Package settings is what an administrator changes while GOtome runs:
// metadata provider keys, the language metadata is asked in, and later
// whatever a feature needs. Every setting is defined in Definitions with its
// kind; the store refuses any other. A secret is kept only sealed (package
// secret), and is never handed out again except to the code that uses it.
package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/secret"
)

// Kinds of setting.
const (
	KindText = "text"
	// KindSecret is written, never read back through the API.
	KindSecret = "secret"
)

// Keys of the settings, as stored and as the API names them.
const (
	MetadataLanguage = "metadata.language"
	GoogleBooksKey   = "metadata.googleBooksKey"
	HardcoverToken   = "metadata.hardcoverToken"
)

// maxValueLen bounds a value; no key or token comes near it.
const maxValueLen = 4096

// keyCheck is the row a key leaves behind on its first start, so that a
// later start under another key is found out before it seals anything.
const keyCheck = "_secret_key_check"

// Definition is one setting.
type Definition struct {
	Key  string
	Kind string
	// Default is what a text setting is while nobody has set it.
	Default string
	// Check refuses a value with a reason for the person who typed it, or
	// returns it as it is kept. Nil keeps it as typed, trimmed.
	Check func(string) (string, error)
}

// Definitions are every setting there is, in the order a page lists them.
var Definitions = []Definition{
	{Key: MetadataLanguage, Kind: KindText, Default: "en", Check: languageCode},
	{Key: GoogleBooksKey, Kind: KindSecret},
	{Key: HardcoverToken, Kind: KindSecret},
}

func definition(key string) (Definition, bool) {
	i := slices.IndexFunc(Definitions, func(d Definition) bool { return d.Key == key })
	if i < 0 {
		return Definition{}, false
	}
	return Definitions[i], true
}

func languageCode(v string) (string, error) {
	v = strings.ToLower(v)
	if len(v) < 2 || len(v) > 3 || strings.IndexFunc(v, func(r rune) bool { return r < 'a' || r > 'z' }) >= 0 {
		return "", errors.New("Give a language as its two-letter code, such as en or de.")
	}
	return v, nil
}

var (
	// ErrWrongKey is a start under another key than the one the secrets in
	// the database were sealed with.
	ErrWrongKey = errors.New("GOTOME_SECRET_KEY_FILE holds another key than the one GOtome's secrets were encrypted with; " +
		"put back the secret-key file from the secrets volume this database was used with")
	// ErrUnknown is a setting that is not in Definitions.
	ErrUnknown = errors.New("no such setting")
)

// ValidationError says which values were refused and why.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "invalid settings" }

// Setting is one setting as an administrator sees it. A secret's value is
// never in it, only whether there is one.
type Setting struct {
	Key  string
	Kind string
	// Value is a text setting's value, or its default when unset.
	Value     string
	IsSet     bool
	UpdatedAt *time.Time
}

// Store keeps the settings.
type Store struct {
	pool *pgxpool.Pool
	box  *secret.Box
}

// Open returns the store, after making sure that the key is the one the
// secrets in the database were sealed with: ErrWrongKey if it is not.
func Open(ctx context.Context, pool *pgxpool.Pool, box *secret.Box) (*Store, error) {
	q := sqlc.New(pool)
	check, err := box.Seal(keyCheck, []byte("gotome"))
	if err != nil {
		return nil, err
	}
	if err := q.AddKeyCheck(ctx, sqlc.AddKeyCheckParams{Key: keyCheck, Sealed: check}); err != nil {
		return nil, err
	}
	row, err := q.GetSetting(ctx, keyCheck)
	if err != nil {
		return nil, err
	}
	if _, err := box.Open(keyCheck, row.Sealed); err != nil {
		return nil, ErrWrongKey
	}
	return &Store{pool: pool, box: box}, nil
}

// List returns every setting, in the order of Definitions.
func (s *Store) List(ctx context.Context) ([]Setting, error) {
	rows, err := sqlc.New(s.pool).ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	stored := map[string]sqlc.Setting{}
	for _, row := range rows {
		stored[row.Key] = row
	}
	out := make([]Setting, len(Definitions))
	for i, d := range Definitions {
		st := Setting{Key: d.Key, Kind: d.Kind, Value: d.Default}
		if row, ok := stored[d.Key]; ok {
			st.IsSet = true
			st.UpdatedAt = &row.UpdatedAt
			if d.Kind == KindText && row.Value != nil {
				st.Value = *row.Value
			}
		}
		if d.Kind == KindSecret {
			st.Value = ""
		}
		out[i] = st
	}
	return out, nil
}

// Update sets the values, all or none. A nil or empty value takes the
// setting back to unset.
func (s *Store) Update(ctx context.Context, by uuid.UUID, values map[string]*string) error {
	fields := map[string]string{}
	type put struct {
		def    Definition
		value  string
		remove bool
	}
	var puts []put
	for key, v := range values {
		d, ok := definition(key)
		if !ok {
			fields[key] = "There is no such setting."
			continue
		}
		if v == nil || strings.TrimSpace(*v) == "" {
			puts = append(puts, put{def: d, remove: true})
			continue
		}
		value := strings.TrimSpace(*v)
		if len(value) > maxValueLen {
			fields[key] = fmt.Sprintf("A value is at most %d characters.", maxValueLen)
			continue
		}
		if d.Check != nil {
			checked, err := d.Check(value)
			if err != nil {
				fields[key] = err.Error()
				continue
			}
			value = checked
		}
		puts = append(puts, put{def: d, value: value})
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		for _, p := range puts {
			if p.remove {
				if err := q.DeleteSetting(ctx, p.def.Key); err != nil {
					return err
				}
				continue
			}
			params := sqlc.PutSettingParams{Key: p.def.Key, UpdatedBy: &by}
			if p.def.Kind == KindSecret {
				sealed, err := s.box.Seal(p.def.Key, []byte(p.value))
				if err != nil {
					return err
				}
				params.Sealed = sealed
			} else {
				params.Value = &p.value
			}
			if err := q.PutSetting(ctx, params); err != nil {
				return err
			}
		}
		return nil
	})
}

// Text returns a text setting, or its default while unset.
func (s *Store) Text(ctx context.Context, key string) (string, error) {
	d, ok := definition(key)
	if !ok || d.Kind != KindText {
		return "", fmt.Errorf("%w: %s", ErrUnknown, key)
	}
	row, err := sqlc.New(s.pool).GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Value == nil) {
		return d.Default, nil
	}
	if err != nil {
		return "", err
	}
	return *row.Value, nil
}

// Secret returns a secret for the code that uses it, and false when none is
// set. It must not leave the server.
func (s *Store) Secret(ctx context.Context, key string) (string, bool, error) {
	d, ok := definition(key)
	if !ok || d.Kind != KindSecret {
		return "", false, fmt.Errorf("%w: %s", ErrUnknown, key)
	}
	row, err := sqlc.New(s.pool).GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, err := s.box.Open(key, row.Sealed)
	if err != nil {
		return "", false, fmt.Errorf("setting %s: %w", key, err)
	}
	return string(value), true, nil
}
