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
	"net"
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
	// CurationCleanupKinds lists, comma-separated, the kinds of clean-up
	// suggestion that a daily run applies by itself (cleanup.AutoKinds).
	CurationCleanupKinds = "curation.cleanup.kinds"
	// CurationMatchesAuto is "on" or "off": whether a daily run takes the
	// fields of matches that providers agree on, and dismisses matches
	// that contradict the text, by itself.
	CurationMatchesAuto = "curation.matches.auto"
	// Providers lists the metadata providers asked, comma-separated.
	Providers = "metadata.providers"
	// ContactEmail is the address given to providers that ask whom to
	// write to about the requests they get: CrossRef serves those faster.
	ContactEmail = "metadata.contactEmail"
	// TrashRetentionDays is how many days a trashed file is kept before it
	// is deleted for good.
	TrashRetentionDays = "trash.retentionDays"
	// UpdatesCheck is "on" or "off": whether GitHub is asked once a day for
	// the latest release, to tell administrators of a newer one.
	UpdatesCheck = "updates.check"
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
	// EmbeddingRemoveUnusedDays is how many days a model is no longer the
	// chosen one before its files and vectors are removed (#357).
	EmbeddingRemoveUnusedDays = "embedding.removeUnusedAfterDays"
	// GenreSuggestions is how books are given genres of the fixed list
	// (internal/genres): "off", the default, writes nothing; "tags" gives
	// the genres their tags mean; "vector", "neighbours" and "combined"
	// suggest more from the books' vectors, for a person to review.
	GenreSuggestions = "genres.suggest"
	// EmbeddingPassages is "on" or "off": whether every passage of a book's
	// text gets a vector of its own, for searching by description (#174).
	// Off by default: on a library of 50,000 books it is about 1.5 GB on
	// disk and days of the CPU (docs/decisions/passage-vectors.md).
	EmbeddingPassages = "embedding.passages"
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
	// MailHost and MailPort are the SMTP server books are sent through to
	// a Kindle (#412), the only host GOtome ever sends mail to; unset, the
	// feature is off.
	MailHost = "mail.host"
	MailPort = "mail.port"
	// MailSecurity is "starttls", "tls" (the whole connection is TLS) or
	// "none". Mail is not sent, and the password not offered, without TLS
	// unless it is "none".
	MailSecurity = "mail.security"
	MailUser     = "mail.user"
	MailPassword = "mail.password"
	// DuplicateClasses lists, comma-separated, the classes of open duplicate
	// pairs that are handled by themselves (#466, dedupe.AutoClasses); empty,
	// the default, handles none.
	DuplicateClasses = "curation.duplicates.classes"
	// DuplicateConflict is what is done with a pair whose books differ in a
	// field no rule can decide: "skip" leaves it whole for a person,
	// "mergeAgreeing" merges what agrees and keeps the surviving book's value
	// for the rest.
	DuplicateConflict = "curation.duplicates.onConflict"
	// MailSender is the From address, the one each person approves at
	// Amazon.
	MailSender = "mail.sender"
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
	// Choices, when set, are the only values the setting takes, after
	// Check; a page offers them to choose from.
	Choices []string
	// Multiple says that the value is a comma-separated list of Choices;
	// with no Default it may be empty.
	Multiple bool
}

// clean checks a value as typed and returns it as it is kept.
func (d Definition) clean(value string) (string, error) {
	if d.Check != nil {
		checked, err := d.Check(value)
		if err != nil {
			return "", err
		}
		value = checked
	}
	if d.Choices == nil {
		return value, nil
	}
	items := []string{value}
	if d.Multiple {
		if value == "" && d.Default == "" {
			return value, nil
		}
		items = strings.Split(value, ",")
	}
	for _, item := range items {
		if !slices.Contains(d.Choices, item) {
			return "", fmt.Errorf("Choose from %s.", strings.Join(d.Choices, ", "))
		}
	}
	return value, nil
}

// Definitions are every setting there is, in the order a page lists them.
var Definitions = []Definition{
	{Key: MetadataLanguage, Kind: KindText, Default: "en", Check: languageCode},
	{Key: GoogleBooksKey, Kind: KindSecret},
	{Key: HardcoverToken, Kind: KindSecret},
	{Key: AutoMatch, Kind: KindText, Default: "on", Check: onOff, Choices: OnOff},
	{Key: MatchThreshold, Kind: KindText, Default: "0.95", Check: threshold},
	{Key: CurationCleanupKinds, Kind: KindText, Check: names, Choices: CleanupKinds, Multiple: true},
	{Key: CurationMatchesAuto, Kind: KindText, Default: "off", Check: onOff, Choices: OnOff},
	{Key: Providers, Kind: KindText, Default: "openlibrary,crossref,hardcover", Check: names, Choices: ProviderNames, Multiple: true},
	{Key: ContactEmail, Kind: KindText, Check: email},
	{Key: TrashRetentionDays, Kind: KindText, Default: "30", Check: days},
	{Key: UpdatesCheck, Kind: KindText, Default: "on", Check: onOff, Choices: OnOff},
	{Key: EmbeddingEnabled, Kind: KindText, Default: "on", Check: onOff, Choices: OnOff},
	{Key: EmbeddingModel, Kind: KindText, Default: embed.DefaultSpec, Check: embeddingModel, Choices: slices.Sorted(maps.Keys(embed.Specs))},
	{Key: EmbeddingServer, Kind: KindText, Check: serverURL},
	{Key: EmbeddingRemoveUnusedDays, Kind: KindText, Default: "1", Check: days},
	{Key: GenreSuggestions, Kind: KindText, Default: "off", Check: genreMode, Choices: GenreModes},
	{Key: EmbeddingPassages, Kind: KindText, Default: "off", Check: onOff, Choices: OnOff},
	{Key: Passwords, Kind: KindText, Default: "on", Check: onOff, Choices: OnOff},
	{Key: OIDCIssuer, Kind: KindText, Check: issuer},
	{Key: OIDCClientID, Kind: KindText},
	{Key: OIDCClientSecret, Kind: KindSecret},
	{Key: OIDCName, Kind: KindText, Default: "single sign-on"},
	{Key: OIDCScopes, Kind: KindText, Default: "openid profile email", Check: scopes},
	{Key: OIDCGroupsClaim, Kind: KindText, Default: "groups"},
	{Key: OIDCAdminGroups, Kind: KindText, Check: list},
	{Key: OIDCEditorGroups, Kind: KindText, Check: list},
	{Key: OIDCReaderGroups, Kind: KindText, Check: list},
	{Key: OIDCDefaultRole, Kind: KindText, Default: "reader", Check: role, Choices: Roles},
	{Key: OIDCSignup, Kind: KindText, Default: "on", Check: onOff, Choices: OnOff},
	{Key: OIDCLinkByEmail, Kind: KindText, Default: "off", Check: onOff, Choices: OnOff},
	{Key: MailHost, Kind: KindText, Check: mailHost},
	{Key: MailPort, Kind: KindText, Default: "587", Check: port},
	{Key: MailSecurity, Kind: KindText, Default: "starttls", Check: onlyChoices, Choices: MailSecurities},
	{Key: MailUser, Kind: KindText},
	{Key: MailPassword, Kind: KindSecret},
	{Key: MailSender, Kind: KindText, Check: email},
	{Key: DuplicateClasses, Kind: KindText, Check: duplicateClasses, Choices: DuplicateClassNames, Multiple: true},
	{Key: DuplicateConflict, Kind: KindText, Default: "skip", Check: duplicateConflict, Choices: DuplicateConflicts},
}

// DuplicateClassNames are the values of DuplicateClasses, as
// dedupe.AutoClasses names them (a test holds the two to one list).
var DuplicateClassNames = []string{"same_text", "overlap_high", "overlap_mid", "contained", "part_inside"}

// DuplicateConflicts are the values of DuplicateConflict.
var DuplicateConflicts = []string{"skip", "mergeAgreeing"}

func duplicateClasses(v string) (string, error) {
	var out []string
	for name := range strings.SplitSeq(strings.ToLower(v), ",") {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return strings.Join(out, ","), nil
}

func duplicateConflict(v string) (string, error) {
	for _, c := range DuplicateConflicts {
		if strings.EqualFold(strings.TrimSpace(v), c) {
			return c, nil
		}
	}
	return "", errors.New("Say skip or mergeAgreeing.")
}

// MailSecurities are the values of MailSecurity.
var MailSecurities = []string{"starttls", "tls", "none"}

// OnOff are the choices of a setting that is on or off.
var OnOff = []string{"on", "off"}

// ProviderNames are the metadata providers Providers may name, as each
// provider's Name says.
var ProviderNames = []string{"openlibrary", "crossref", "hardcover"}

// Roles are the values of OIDCDefaultRole: a role, or none to refuse.
var Roles = []string{"admin", "editor", "reader", "none"}

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
	if v = strings.ToLower(strings.TrimSpace(v)); slices.Contains(OnOff, v) {
		return v, nil
	}
	return "", errors.New("Say on or off.")
}

// GenreModes are the values of GenreSuggestions, as genres.Modes names
// them.
var GenreModes = []string{"off", "tags", "vector", "neighbours", "combined"}

func genreMode(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if slices.Contains(GenreModes, v) {
		return v, nil
	}
	return "", errors.New("Say off, tags, vector, neighbours or combined.")
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
	if v = strings.ToLower(strings.TrimSpace(v)); slices.Contains(Roles, v) {
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
// mailHost is a host name or address without scheme, port or path.
func mailHost(v string) (string, error) {
	if v == "" || len(v) > 253 || strings.ContainsAny(v, " \t/@?#,;\"<>") || strings.Contains(v, "://") ||
		(strings.Contains(v, ":") && net.ParseIP(strings.Trim(v, "[]")) == nil) {
		return "", errors.New("Give the mail server's name, such as smtp.example.org, without a port.")
	}
	return strings.ToLower(strings.Trim(v, "[]")), nil
}

func port(v string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("Give a port from 1 to 65535.")
	}
	return strconv.Itoa(n), nil
}

// onlyChoices leaves a value to the Choices that follow it.
func onlyChoices(v string) (string, error) { return strings.ToLower(v), nil }

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

// names keeps a list of provider names in one spelling: "openlibrary,crossref".
func names(v string) (string, error) {
	var out []string
	for name := range strings.SplitSeq(strings.ToLower(v), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.IndexFunc(name, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') }) >= 0 {
			return "", errors.New("List the sources by name, separated by commas, such as openlibrary,crossref.")
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
	Key      string
	Kind     string
	Choices  []string
	Multiple bool
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
	return OpenReadOnly(ctx, pool, box)
}

// OpenReadOnly is Open for a database that may not be written to, such as
// a command that only measures: the check value must be there already.
func OpenReadOnly(ctx context.Context, pool *pgxpool.Pool, box *secret.Box) (*Store, error) {
	row, err := sqlc.New(pool).GetSetting(ctx, keyCheck)
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
		st := Setting{Key: d.Key, Kind: d.Kind, Choices: d.Choices, Multiple: d.Multiple, Value: d.Default}
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
		value, err := d.clean(value)
		if err != nil {
			fields[key] = err.Error()
			continue
		}
		puts = append(puts, put{def: d, value: value})
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		_, choosing := values[EmbeddingModel]
		var before string
		if choosing {
			var err error
			if before, err = s.TextTx(ctx, tx, EmbeddingModel); err != nil {
				return err
			}
		}
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
		if choosing {
			return recordChoice(ctx, s, tx, before)
		}
		return nil
	})
	if err == nil && s.OnChange != nil {
		s.OnChange(ctx, slices.Sorted(maps.Keys(values)))
	}
	return err
}

// recordChoice notes when the model chosen before stopped being chosen,
// which is when its files and vectors may go, and that the one chosen now
// is not unused.
func recordChoice(ctx context.Context, s *Store, tx pgx.Tx, before string) error {
	after, err := s.TextTx(ctx, tx, EmbeddingModel)
	if err != nil || after == before {
		return err
	}
	q := sqlc.New(tx)
	if err := q.MarkModelLeft(ctx, before); err != nil {
		return err
	}
	return q.MarkModelChosen(ctx, after)
}

// RemoveUnusedAfter is how long a model is not chosen before its files and
// vectors are removed.
func (s *Store) RemoveUnusedAfter(ctx context.Context) (time.Duration, error) {
	v, err := s.Text(ctx, EmbeddingRemoveUnusedDays)
	if err != nil {
		return 0, err
	}
	n, err := days(v)
	if err != nil {
		return 0, fmt.Errorf("%s is %q: %w", EmbeddingRemoveUnusedDays, v, err)
	}
	d, _ := strconv.Atoi(n)
	return time.Duration(d) * 24 * time.Hour, nil
}

// Text returns a text setting, or its default while unset.
func (s *Store) Text(ctx context.Context, key string) (string, error) {
	return s.TextTx(ctx, s.pool, key)
}

// TextTx is Text inside a transaction, on its connection. Code that holds
// a transaction must read a setting so: asking the pool for another
// connection while holding locks waits forever once every connection is
// taken by code waiting on those locks (#189).
func (s *Store) TextTx(ctx context.Context, db sqlc.DBTX, key string) (string, error) {
	d, ok := definition(key)
	if !ok || d.Kind != KindText {
		return "", fmt.Errorf("%w: %s", ErrUnknown, key)
	}
	row, err := sqlc.New(db).GetSetting(ctx, key)
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
	// The value is checked on its way in; this one may have come another
	// way, and a retention of none would purge the whole trash.
	n, err := days(v)
	if err != nil {
		return 0, fmt.Errorf("%s is %q: %w", TrashRetentionDays, v, err)
	}
	d, _ := strconv.Atoi(n)
	return time.Duration(d) * 24 * time.Hour, nil
}

// EmbeddingPassages says whether passages are embedded and searched.
func (s *Store) EmbeddingPassages(ctx context.Context) (bool, error) {
	v, err := s.Text(ctx, EmbeddingPassages)
	return v == "on", err
}

// EmbeddingServer is the address of the embedding server, or "" for none.
func (s *Store) EmbeddingServer(ctx context.Context) (string, error) {
	return s.Text(ctx, EmbeddingServer)
}

// CleanupKinds are the values of CurationCleanupKinds, as cleanup.AutoKinds
// names them.
var CleanupKinds = []string{"placeholders", "titles", "authors", "series"}
