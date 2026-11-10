//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// onDatabase points the commands at the test's database, as the app
// container's environment points them at the installation's. The tests of
// this file change the process's environment and standard streams, so none
// runs in parallel.
func onDatabase(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	dir := t.TempDir()
	key, err := secret.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "secret-key")
	if err := os.WriteFile(keyFile, []byte(key), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOTOME_DATABASE_URL", pool.Config().ConnString())
	t.Setenv("GOTOME_SECRET_KEY_FILE", keyFile)
	t.Setenv("GOTOME_DATA_DIR", dir)
	t.Setenv("GOTOME_OFFLINE", "true")
	t.Setenv("GOTOME_LOG_LEVEL", "error")
}

// command runs gotome with the arguments, stdin read from in, and returns
// what it printed on stdout.
func command(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	stdin, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if _, err := stdin.WriteString(in); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	runErr := run(args)
	out, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func TestMigrateBringsAnEmptyDatabaseUpToDate(t *testing.T) {
	pool := dbtest.NewEmpty(t)
	onDatabase(t, pool)
	if _, err := command(t, "", "migrate"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	version, err := db.SchemaVersion(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := db.Migrate(ctx, pool); err != nil || applied != 0 {
		t.Errorf("schema %d, migrations left after migrate: %d %v", version, applied, err)
	}
	var riverTables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE tablename = 'river_job'").Scan(&riverTables); err != nil || riverTables != 1 {
		t.Errorf("the job queue's table: %d %v", riverTables, err)
	}
	if _, err := command(t, "", "migrate"); err != nil {
		t.Errorf("migrate again: %v", err)
	}
}

func TestResetPasswordLetsTheAccountSignInAgain(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	ctx := context.Background()
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Setup(ctx, "root", "the-first-password", nil); err != nil {
		t.Fatal(err)
	}
	ada, err := accounts.CreateAccount(ctx, auth.NewAccount{Username: "ada", Password: "forgotten-password", Role: auth.RoleEditor})
	if err != nil {
		t.Fatal(err)
	}
	disabled := true
	if _, err := accounts.UpdateAccount(ctx, ada.ID, auth.AccountChanges{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	_, session, _, err := accounts.Login(ctx, "root", "the-first-password", "test")
	if err != nil {
		t.Fatal(err)
	}

	out, err := command(t, "a new password for root\n", "reset-password", "root")
	if err != nil || !strings.Contains(out, "The password of root is set") {
		t.Fatalf("reset-password root: %q %v", out, err)
	}
	if _, _, _, err := accounts.Login(ctx, "root", "a new password for root", "test"); err != nil {
		t.Errorf("signing in with the new password: %v", err)
	}
	if _, _, _, err := accounts.Login(ctx, "root", "the-first-password", "test"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("signing in with the old password: %v", err)
	}
	if _, err := accounts.Authenticate(ctx, session); err == nil {
		t.Error("the session from before the reset still signs in")
	}

	// Disabled, the account stays so unless -enable, and an editor unless -admin.
	out, err = command(t, "ada's new password\n", "reset-password", "ada")
	if err != nil || !strings.Contains(out, "-enable takes that back") || !strings.Contains(out, "an editor") {
		t.Errorf("reset-password ada: %q %v", out, err)
	}
	if _, _, _, err := accounts.Login(ctx, "ada", "ada's new password", "test"); err == nil {
		t.Error("a disabled account signs in")
	}
	if _, err := command(t, "ada's new password\n", "reset-password", "-enable", "ada", "-admin"); err != nil {
		t.Fatal(err)
	}
	user, _, _, err := accounts.Login(ctx, "ada", "ada's new password", "test")
	if err != nil || user.Role != auth.RoleAdmin {
		t.Errorf("ada after -enable -admin: %+v %v", user, err)
	}

	if _, err := command(t, "whatever password\n", "reset-password", "nobody"); err == nil || !strings.Contains(err.Error(), `no account named "nobody"`) {
		t.Errorf("an account that does not exist: %v", err)
	}
	if _, err := command(t, "short\n", "reset-password", "root"); err == nil {
		t.Error("a password too short is taken")
	}
}

// seedVectors makes five books of one author, two of them in a series,
// and one of another, with a content vector of the default model each.
func seedVectors(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := accounts.Setup(ctx, "root", "the-first-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.NewService(pool, t.TempDir()).Create(ctx, admin.ID, library.NewLibrary{Name: "Shelf", Mode: "managed", Visibility: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	books := []struct{ title, author, series, tag string }{
		{"Die Schreiberin", "Ulrike Weidner", "Archivreihe", "Historischer Roman"},
		{"Das stille Archiv", "Ulrike Weidner", "Archivreihe", "Historischer Roman"},
		{"Die Gräfin", "Ulrike Weidner", "", "Historischer Roman"},
		{"Der verschollene Abt", "Ulrike Weidner", "", "Historischer Roman"},
		{"Die Domherrin", "Ulrike Weidner", "", "Historischer Roman"},
		{"Die Silberweberin", "Greta Berndt", "", "Historischer Roman"},
	}
	model := embed.Specs[embed.DefaultSpec].Model
	for i, b := range books {
		id, err := catalog.NewService(pool).CreateBook(ctx, catalog.NewBook{
			LibraryID: lib.ID, Title: b.title, Series: b.series, Tags: []string{b.tag},
			Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		v := make([]string, model.Dim)
		for j := range v {
			v[j] = "0"
		}
		v[0], v[i+1] = "1", "0.1"
		if _, err := pool.Exec(ctx, `
			INSERT INTO book_vectors (book_id, kind, model, model_version, source_hash, embedding)
			VALUES ($1, $2, $3, $4, sha256('test'), $5::text::vector)`,
			id, similar.KindContent, model.Name, similar.Version(model), "["+strings.Join(v, ",")+"]"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSimilarAndGenreChecksMeasureTheLibrary(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	seedVectors(t, pool)
	// The genres' descriptions are embedded for the check, which the test
	// toolchain has no ONNX Runtime for.
	localOpener = func(embed.OnnxOptions) similar.Opener {
		return func(context.Context, embed.Spec) (embed.Embedder, error) { return &embed.Fake{}, nil }
	}
	t.Cleanup(func() { localOpener = similar.OnnxOpener })

	out, err := command(t, "", "similar-check", "-json", "-n", "10", "-k", "3")
	if err != nil {
		t.Fatalf("similar-check: %v\n%s", err, out)
	}
	var similarReport similar.Report
	if err := json.Unmarshal([]byte(out), &similarReport); err != nil {
		t.Fatalf("similar-check: %v\n%s", err, out)
	}
	// Only the books of an author of five are asked about.
	if similarReport.Books != 5 || similarReport.InSeries != 2 || similarReport.Neighbours != 15 {
		t.Errorf("similar-check: %+v", similarReport)
	}
	if out, err := command(t, "", "similar-check", "-n", "10"); err != nil || !strings.Contains(out, "Books asked about           5") {
		t.Errorf("similar-check as text: %v\n%s", err, out)
	}

	out, err = command(t, "", "genre-check", "-json", "-n", "10", "-neighbours=false")
	if err != nil {
		t.Fatalf("genre-check: %v\n%s", err, out)
	}
	var genreReport genres.CheckReport
	if err := json.Unmarshal([]byte(out), &genreReport); err != nil {
		t.Fatalf("genre-check: %v\n%s", err, out)
	}
	if genreReport.Books != 6 || genreReport.Tagged != 6 || genreReport.Embedded != 6 || genreReport.Sample != 6 {
		t.Errorf("genre-check: %+v", genreReport)
	}
}

func TestTheMatchCheckFindsTheBooksAgainWithoutTheirISBNs(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	ctx := context.Background()
	records := filepath.Join(t.TempDir(), "metadata.json")
	// Der Process is found as itself, Dusk as another edition of its work
	// (the deluxe edition, which the search does not find, carries its
	// ISBN), Emma first as an edition nobody says is hers and then as
	// itself, and the last book not at all.
	if err := os.WriteFile(records, []byte(`[
		{"id": "process", "title": "Der Process", "authors": ["Franz Kafka"], "language": "de", "isbns": ["9783596294312"]},
		{"id": "dusk-2019", "work": "dusk", "title": "Dusk", "authors": ["Henrik Halden"], "language": "en", "isbns": ["9781950001057"]},
		{"id": "dusk-deluxe", "work": "dusk", "title": "Dusk Deluxe", "authors": ["Henrik Halden"], "isbns": ["9780195001013"]},
		{"id": "emma-other", "title": "Emma", "authors": ["Jane Austen"], "language": "en", "isbns": ["9780306406157"]},
		{"id": "emma", "title": "Emma", "language": "de", "isbns": ["9780141439587"]}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOTOME_TEST_METADATA", records)
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := accounts.Setup(ctx, "root", "the-first-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.NewService(pool, t.TempDir()).Create(ctx, admin.ID, library.NewLibrary{Name: "Shelf", Mode: "managed", Visibility: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	books := []struct{ title, author, language, isbn string }{
		{"Der Process", "Franz Kafka", "de", "9783596294312"},
		{"Dusk", "Henrik Halden", "en", "9780195001013"},
		{"Emma", "Jane Austen", "en", "9780141439587"},
		{"Nothing Like It", "Nobody Known", "en", "9780141199085"},
		{"Emma", "Jane Austen", "en", ""},
	}
	for _, b := range books {
		nb := catalog.NewBook{
			LibraryID: lib.ID, Title: b.title, Language: b.language,
			Contributors: []catalog.NewContributor{{Name: b.author}},
		}
		if b.isbn != "" {
			nb.Identifiers = []catalog.Identifier{{Type: catalog.IDISBN, Value: b.isbn}}
		}
		if _, err := catalog.NewService(pool).CreateBook(ctx, nb); err != nil {
			t.Fatal(err)
		}
	}

	out, err := command(t, "", "match-check", "-json", "-n", "10")
	if err != nil {
		t.Fatalf("match-check: %v\n%s", err, out)
	}
	var r metadata.CheckReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("match-check: %v\n%s", err, out)
	}
	all := r.All
	if r.Books != 4 || r.Sample != 4 || all.Books != 4 || all.Right != 1 || all.SameWork != 1 || all.Wrong != 1 ||
		all.None != 1 || all.Failed != 0 || all.Ranks[2] != 1 || all.Missing != 0 {
		t.Errorf("match-check: %+v", r)
	}
	if len(r.PerProvider) != 1 || r.PerProvider[0].Name != "canned" || r.PerProvider[0].Right != 1 {
		t.Errorf("per provider: %+v", r.PerProvider)
	}
	if len(r.PerLanguage) != 3 || r.PerLanguage[0].Books != 1 || r.PerLanguage[0].Right != 1 || r.PerLanguage[1].Books != 3 {
		t.Errorf("per language: %+v", r.PerLanguage)
	}
	if r.Threshold != 0.95 || r.Thresholds[0].Taken != 3 {
		t.Errorf("thresholds: %v %+v", r.Threshold, r.Thresholds)
	}

	out, err = command(t, "", "match-check", "-n", "2", "-providers", "canned")
	if err != nil || !strings.Contains(out, "Looked up without it         2, at canned") {
		t.Errorf("match-check as text: %v\n%s", err, out)
	}
	if _, err := command(t, "", "match-check", "-providers", "nobody"); err == nil || !strings.Contains(err.Error(), `no provider "nobody"`) {
		t.Errorf("an unknown provider: %v", err)
	}
}

func TestTheDuplicateCheckSaysWhatTheRulesWouldDoAndNamesNothing(t *testing.T) {
	pool := dbtest.New(t)
	onDatabase(t, pool)
	ctx := context.Background()
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := accounts.Setup(ctx, "root", "the-first-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := library.NewService(pool, t.TempDir()).Create(ctx, admin.ID, library.NewLibrary{Name: "Shelf", Mode: "managed", Visibility: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	books := catalog.NewService(pool)
	add := func(title, author string) string {
		t.Helper()
		b, err := books.CreateBook(ctx, catalog.NewBook{
			LibraryID: lib.ID, Title: title, Contributors: []catalog.NewContributor{{Name: author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	pair := func(x, y, kind, detail string) {
		t.Helper()
		if y < x {
			x, y = y, x
		}
		if _, err := pool.Exec(ctx, `WITH p AS (INSERT INTO duplicate_pairs (book_a, book_b) VALUES ($1, $2) RETURNING id)
			INSERT INTO duplicate_evidence (pair_id, kind, detail) SELECT id, $3, $4 FROM p`, x, y, kind, detail); err != nil {
			t.Fatal(err)
		}
	}
	pair(add("Der Sternwanderer", "Lena Brandt"), add("Der Sternwanderer", "Lena Brandt"), "content", "ab")
	pair(add("Die Mondkarte", "Lena Brandt"), add("Das Wetterbuch", "Hanno Vogt"), "overlap", "jaccard=0.97 a_in_b=0.98 b_in_a=0.99")
	pair(add("Der Atlas", "Hanno Vogt"), add("Der Atlas, Teil eins", "Hanno Vogt"), "overlap", "jaccard=0.20 a_in_b=0.10 b_in_a=0.97")

	out, err := command(t, "", "duplicate-check", "-json")
	if err != nil {
		t.Fatalf("duplicate-check: %v\n%s", err, out)
	}
	var r struct {
		OnConflict string
		Classes    []struct {
			Class                                string
			Pairs, Sampled, Merged, Linked, Left int
			Differing, Reasons                   map[string]int
		}
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("duplicate-check: %v\n%s", err, out)
	}
	got := map[string][3]int{}
	for _, c := range r.Classes {
		got[c.Class] = [3]int{c.Merged, c.Linked, c.Left}
	}
	if r.OnConflict != "skip" || got["same_text"] != [3]int{1, 0, 0} || got["overlap_high"] != [3]int{0, 0, 1} || got["contained"] != [3]int{0, 1, 0} {
		t.Errorf("duplicate-check: %s", out)
	}
	for _, c := range r.Classes {
		if c.Class == "overlap_high" && (c.Reasons["fields differ"] != 1 || c.Differing["title"] != 1) {
			t.Errorf("why the pair is left: %+v", c)
		}
	}
	text, err := command(t, "", "duplicate-check", "-n", "5", "-on-conflict", "mergeAgreeing")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Sternwanderer", "Mondkarte", "Wetterbuch", "Atlas", "Brandt", "Vogt"} {
		if strings.Contains(text, title) || strings.Contains(out, title) {
			t.Errorf("the check names %q:\n%s", title, text)
		}
	}
	if !strings.Contains(text, "same_text: 1 open, 1 looked at") || !strings.Contains(text, "merged 1") {
		t.Errorf("duplicate-check as text:\n%s", text)
	}
	if _, err := command(t, "", "duplicate-check", "-on-conflict", "maybe"); err == nil {
		t.Error("an unknown conflict rule is accepted")
	}
}
