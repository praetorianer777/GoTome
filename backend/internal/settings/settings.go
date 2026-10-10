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
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
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
	// AutoMatch is "on" or "off": whether new books are looked up by
	// themselves.
	AutoMatch = "metadata.autoMatch"
	// MatchThreshold is the score from 0.5 to 1 above which a match found
	// by itself is taken without asking.
	MatchThreshold = "metadata.matchThreshold"
	// Providers lists the metadata providers asked, comma-separated.
	Providers = "metadata.providers"
	// ContactEmail is the address given to providers that ask whom to
	// write to about the requests they get: CrossRef serves those faster.
	ContactEmail = "metadata.contactEmail"
	// TrashRetentionDays is how many days a trashed file is kept before it
	// is deleted for good.
	TrashRetentionDays = "trash.retentionDays"
	// EmbeddingEnabled is "on" or "off": whether books are embedded for
	// suggesting similar ones. Off pauses it; on again goes on with the
	// books that have no vector yet.
	EmbeddingEnabled = "embedding.enabled"
	// EmbeddingModel is the name of one of embed.Specs. Another one has
	// every book embedded again.
	EmbeddingModel = "embedding.model"
	// EmbeddingServer is the address of an Ollama server that runs the
	// models GOtome does not run itself, such as bge-m3 on a GPU: the
	// models of embed.Specs with a Server. Unset, they are not available.
	EmbeddingServer = "embedding.server"
	// Passwords is "on" or "off": whether people may sign in with a
	// password. Off leaves it to administrators while none of them can sign
	// in through the identity provider.
	Passwords = "auth.passwords"
	// OIDCIssuer is the identity provider's issuer URL; while it is unset
	// nobody signs in through one.
	OIDCIssuer       = "oidc.issuer"
	OIDCClientID     = "oidc.clientId"
	OIDCClientSecret = "oidc.clientSecret"
	// OIDCName is what the sign-in button calls the provider.
	OIDCName   = "oidc.name"
	OIDCScopes = "oidc.scopes"
	// OIDCGroupsClaim names the claim of the ID token that lists the
	// person's groups.
	OIDCGroupsClaim = "oidc.groupsClaim"
	// OIDCAdminGroups, OIDCEditorGroups and OIDCReaderGroups list the
	// groups, comma-separated, that make someone that role. While all are
	// unset, roles are given in GOtome; once one is set, the groups decide
	// at every sign-in.
	OIDCAdminGroups  = "oidc.adminGroups"
	OIDCEditorGroups = "oidc.editorGroups"
	OIDCReaderGroups = "oidc.readerGroups"
	// OIDCDefaultRole is the role of someone in none of the groups, or
	// "none" to refuse them.
	OIDCDefaultRole = "oidc.defaultRole"
	// OIDCSignup is "on" or "off": whether a first sign-in makes an account.
	OIDCSignup = "oidc.signup"
	// OIDCLinkByEmail is "on" or "off": whether a first sign-in takes the
	// account of the address the provider says it has verified.
	OIDCLinkByEmail = "oidc.linkByEmail"
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
	{Key: AutoMatch, Kind: KindText, Default: "on", Check: onOff},
	{Key: MatchThreshold, Kind: KindText, Default: "0.95", Check: threshold},
	{Key: Providers, Kind: KindText, Default: "openlibrary,crossref,hardcover", Check: names},
	{Key: ContactEmail, Kind: KindText, Check: email},
	{Key: TrashRetentionDays, Kind: KindText, Default: "30", Check: days},
	{Key: EmbeddingEnabled, Kind: KindText, Default: "on", Check: onOff},
	{Key: EmbeddingModel, Kind: KindText, Default: embed.DefaultSpec, Check: embeddingModel},
	{Key: EmbeddingServer, Kind: KindText, Check: serverURL},
	{Key: Passwords, Kind: KindText, Default: "on", Check: onOff},
	{Key: OIDCIssuer, Kind: KindText, Check: issuer},
	{Key: OIDCClientID, Kind: KindText},
	{Key: OIDCClientSecret, Kind: KindSecret},
	{Key: OIDCName, Kind: KindText, Default: "single sign-on"},
	{Key: OIDCScopes, Kind: KindText, Default: "openid profile email", Check: scopes},
	{Key: OIDCGroupsClaim, Kind: KindText, Default: "groups"},
	{Key: OIDCAdminGroups, Kind: KindText, Check: list},
	{Key: OIDCEditorGroups, Kind: KindText, Check: list},
	{Key: OIDCReaderGroups, Kind: KindText, Check: list},
	{Key: OIDCDefaultRole, Kind: KindText, Default: "reader", Check: role},
	{Key: OIDCSignup, Kind: KindText, Default: "on", Check: onOff},
	{Key: OIDCLinkByEmail, Kind: KindText, Default: "off", Check: onOff},
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

func onOff(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "on", "off":
		return v, nil
	}
	return "", errors.New("Say on or off.")
}

func threshold(v string) (string, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0.5 || f > 1 {
		return "", errors.New("Give a number from 0.5 to 1, such as 0.95.")
	}
	return strconv.FormatFloat(f, 'f', -1, 64), nil
}

func email(v string) (string, error) {
	local, domain, ok := strings.Cut(v, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") || strings.ContainsAny(v, " \t<>,;\"") {
		return "", errors.New("Give an e-mail address, such as library@example.org.")
	}
	return v, nil
}

// issuer is an identity provider's issuer URL, as it names itself: plain
// HTTP only for one on the same network, which a home installation may run.
func issuer(v string) (string, error) {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Give the provider's issuer URL, such as https://auth.example.org/application/o/gotome/.")
	}
	return v, nil
}

// serverURL is the address of a server GOtome sends requests to, without a
// path: http://host.docker.internal:11434.
func serverURL(v string) (string, error) {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Give the server's address, such as http://host.docker.internal:11434.")
	}
	return strings.TrimRight(v, "/"), nil
}

// scopes keeps the scopes space-separated, with openid among them.
func scopes(v string) (string, error) {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' })
	if !slices.Contains(fields, "openid") {
		return "", errors.New("The scopes must include openid.")
	}
	return strings.Join(fields, " "), nil
}

// list keeps a comma-separated list without blanks around its items.
func list(v string) (string, error) {
	var items []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return strings.Join(items, ","), nil
}

func role(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "admin", "editor", "reader", "none":
		return v, nil
	}
	return "", errors.New("Say admin, editor, reader or none.")
}

// PasswordsOff says whether signing in with a password is turned off: only
// while an identity provider is set up, the other way in. A setting that
// cannot be read leaves them on.
func (s *Store) PasswordsOff(ctx context.Context) bool {
	for _, key := range []string{OIDCIssuer, OIDCClientID} {
		if v, err := s.Text(ctx, key); err != nil || v == "" {
			return false
		}
	}
	v, err := s.Text(ctx, Passwords)
	return err == nil && v == "off"
}

// Items returns a comma-separated text setting as its items.
func (s *Store) Items(ctx context.Context, key string) ([]string, error) {
	v, err := s.Text(ctx, key)
	if err != nil || v == "" {
		return nil, err
	}
	return strings.Split(v, ","), nil
}

// days is a whole number of days, from one to ten years.
func days(v string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 3650 {
		return "", errors.New("Give a whole number of days from 1 to 3650.")
	}
	return strconv.Itoa(n), nil
}

func embeddingModel(v string) (string, error) {
	v = strings.ToLower(v)
	if _, ok := embed.Specs[v]; !ok {
		names := slices.Sorted(maps.Keys(embed.Specs))
		return "", fmt.Errorf("Choose one of %s.", strings.Join(names, ", "))
	}
	return v, nil
}

// names keeps a list of provider names in one spelling: "openlibrary,google".
// Which names exist is the metadata package's to know; an unknown one is
// asked nothing.
func names(v string) (string, error) {
	var out []string
	for name := range strings.SplitSeq(strings.ToLower(v), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.IndexFunc(name, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }) >= 0 {
			return "", errors.New("List the sources by name, separated by commas, such as openlibrary,google.")
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return strings.Join(out, ","), nil
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
	// OnChange, when set, is told the keys of every update after it is
	// saved.
	OnChange func(ctx context.Context, keys []string)
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
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
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
	if err == nil && s.OnChange != nil {
		s.OnChange(ctx, slices.Sorted(maps.Keys(values)))
	}
	return err
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

// Embedding says which model books are embedded with, and whether they are
// embedded at all.
func (s *Store) Embedding(ctx context.Context) (embed.Spec, bool, error) {
	on, err := s.Text(ctx, EmbeddingEnabled)
	if err != nil {
		return embed.Spec{}, false, err
	}
	name, err := s.Text(ctx, EmbeddingModel)
	if err != nil {
		return embed.Spec{}, false, err
	}
	spec, ok := embed.Specs[name]
	if !ok {
		return embed.Spec{}, false, fmt.Errorf("%s: no model %q", EmbeddingModel, name)
	}
	return spec, on == "on", nil
}

// TrashRetention is how long a trashed file is kept before it is deleted
// for good.
func (s *Store) TrashRetention(ctx context.Context) (time.Duration, error) {
	v, err := s.Text(ctx, TrashRetentionDays)
	if err != nil {
		return 0, err
	}
	days, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", TrashRetentionDays, err)
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// EmbeddingServer is the address of the embedding server, or "" for none.
func (s *Store) EmbeddingServer(ctx context.Context) (string, error) {
	return s.Text(ctx, EmbeddingServer)
}
