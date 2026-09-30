//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

const emmaMetadata = `
    <dc:identifier id="uid">urn:isbn:0-14-143958-0</dc:identifier>
    <dc:title>Emma</dc:title>
    <dc:creator opf:file-as="Austen, Jane" opf:role="aut">Jane Austen</dc:creator>
    <dc:contributor opf:role="ill">Hugh Thomson</dc:contributor>
    <dc:contributor opf:role="bkp">calibre</dc:contributor>
    <dc:language>en</dc:language>
    <dc:publisher>Penguin Classics</dc:publisher>
    <dc:date>1815-12</dc:date>
    <dc:description>&lt;p&gt;A comedy of manners.&lt;/p&gt;</dc:description>
    <dc:subject>Fiction</dc:subject>
    <dc:subject>Classics</dc:subject>
    <meta name="calibre:series" content="Austen Novels"/>
    <meta name="calibre:series_index" content="4"/>
    <meta name="cover" content="cover-image"/>`

// writeEPUB puts a small EPUB 2 at the path: the metadata given, a cover when
// there is one, and one chapter per text.
func writeEPUB(t *testing.T, path, metadata string, cover []byte, chapters ...string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, content)
	}
	add("mimetype", "application/epub+zip", zip.Store)
	add("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`, zip.Deflate)
	var manifest, spine strings.Builder
	for i, text := range chapters {
		fmt.Fprintf(&manifest, `<item id="ch%d" href="ch%d.xhtml" media-type="application/xhtml+xml"/>`, i, i)
		fmt.Fprintf(&spine, `<itemref idref="ch%d"/>`, i)
		add(fmt.Sprintf("OEBPS/ch%d.xhtml", i), `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>`+text+`</p></body></html>`, zip.Deflate)
	}
	if cover != nil {
		manifest.WriteString(`<item id="cover-image" href="cover.png" media-type="image/png"/>`)
		add("OEBPS/cover.png", string(cover), zip.Deflate)
	}
	add("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">`+metadata+`
  </metadata>
  <manifest>`+manifest.String()+`</manifest>
  <spine>`+spine.String()+`</spine>
</package>`, zip.Deflate)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 600, 900))
	for i := range img.Pix {
		img.Pix[i] = 0xc0
	}
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// shelfFile is a file of the library as the extraction tests look at it.
type shelfFile struct {
	ID, BookID    uuid.UUID
	State         string
	Error         *string
	SHA256        []byte
	ContentSHA256 []byte
	HasText       *bool
	Pages         *int32
}

func (a *app) shelfFiles() map[string]shelfFile {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(),
		`SELECT rel_path, id, book_id, extract_state, extract_error, sha256, content_sha256, has_text, page_count FROM book_files`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]shelfFile{}
	for rows.Next() {
		var relPath string
		var f shelfFile
		if err := rows.Scan(&relPath, &f.ID, &f.BookID, &f.State, &f.Error, &f.SHA256, &f.ContentSHA256, &f.HasText, &f.Pages); err != nil {
			a.t.Fatal(err)
		}
		out[relPath] = f
	}
	return out
}

// scanNow scans the library in the test itself, without the queue.
func (a *app) scanNow(libraryID string) {
	a.t.Helper()
	complete, err := a.scans.Run(context.Background(), uuid.MustParse(libraryID), 0)
	if err != nil || !complete {
		a.t.Fatalf("scan: complete %v, %v", complete, err)
	}
}

func (a *app) extract(id uuid.UUID) {
	a.t.Helper()
	if err := a.scans.Extract(context.Background(), id); err != nil {
		a.t.Fatalf("extract: %v", err)
	}
}

func (a *app) book(id uuid.UUID) catalog.Book {
	a.t.Helper()
	book, err := catalog.NewService(a.pool).Get(context.Background(), library.Scope{SeesAll: true}, id)
	if err != nil {
		a.t.Fatal(err)
	}
	return book
}

func TestImportedEPUBGetsItsMetadataAndCover(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "austen_emma_final(2).epub"), emmaMetadata, coverPNG(t),
		strings.Repeat("Emma Woodhouse, handsome, clever, and rich. ", 100), "The end.")
	os.WriteFile(filepath.Join(books, "Broken.epub"), []byte("this is no zip archive"), 0o644)

	id := a.externalLibrary(admin, "Shelf", "private", books)
	a.workJobs()
	// A file that cannot be read does not keep the scan from finishing, nor
	// the other file from being read.
	if scan := a.lastScan(admin, id, "done"); scan["filesAdded"] != 2.0 {
		t.Fatalf("scan: %v", scan)
	}
	var files map[string]shelfFile
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		files = a.shelfFiles()
		if files["austen_emma_final(2).epub"].State == "done" && files["Broken.epub"].State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("extraction did not finish: %+v", files)
		}
	}
	if broken := files["Broken.epub"]; broken.Error == nil || !strings.Contains(*broken.Error, "not an EPUB") {
		t.Errorf("the broken file's error is %v, want it to say that this is no EPUB", broken.Error)
	}
	if a.book(files["Broken.epub"].BookID).Title != "Broken" {
		t.Error("the book of the broken file lost the title its file name gave it")
	}

	file := files["austen_emma_final(2).epub"]
	if len(file.ContentSHA256) != 32 || file.HasText == nil || !*file.HasText || file.Pages == nil || *file.Pages != 3 {
		t.Errorf("the file after extraction: %+v, want a content hash, text and 3 pages", file)
	}
	book := a.book(file.BookID)
	if book.Title != "Emma" || book.Language != "en" || book.Publisher != "Penguin Classics" ||
		book.Description != "A comedy of manners." || book.Series != "Austen Novels" ||
		book.SeriesIndex == nil || *book.SeriesIndex != 4 || book.PageCount == nil || *book.PageCount != 3 {
		t.Errorf("the book after extraction: %+v", book)
	}
	if book.PublishedOn == nil || book.PublishedOn.Format("2006-01") != "1815-12" || book.PublishedPrecision != "month" {
		t.Errorf("published %v (%s), want December 1815 to the month", book.PublishedOn, book.PublishedPrecision)
	}
	var credits []string
	for _, c := range book.Contributors {
		credits = append(credits, c.Role+": "+c.SortName)
	}
	if want := []string{"author: Austen, Jane", "illustrator: Thomson, Hugh"}; !slices.Equal(credits, want) {
		t.Errorf("credits = %v, want %v", credits, want)
	}
	if !slices.Equal(book.Tags, []string{"Classics", "Fiction"}) {
		t.Errorf("tags = %v", book.Tags)
	}
	if len(book.Identifiers) != 1 || book.Identifiers[0].Type != "isbn" || book.Identifiers[0].Value != "9780141439587" ||
		book.Identifiers[0].FileID == nil || *book.Identifiers[0].FileID != file.ID {
		t.Errorf("identifiers = %+v, want the file's ISBN as 13 digits", book.Identifiers)
	}

	// The cover, as the browser asks for it.
	cover := func(c *http.Client, path string, header http.Header) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, a.url+httpapi.APIPrefix+path, nil)
		for name, values := range header {
			req.Header[name] = values
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	base := "/books/" + book.ID.String() + "/covers/"
	small := cover(admin, base+"small", nil)
	img, err := jpeg.Decode(small.Body)
	if small.StatusCode != 200 || small.Header.Get("Content-Type") != "image/jpeg" || err != nil || img.Bounds().Dx() != 400 {
		t.Fatalf("small cover: %d %s, %v", small.StatusCode, small.Header.Get("Content-Type"), err)
	}
	etag := small.Header.Get("ETag")
	if etag == "" || small.Header.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("small cover: ETag %q, Cache-Control %q", etag, small.Header.Get("Cache-Control"))
	}
	if again := cover(admin, base+"small", http.Header{"If-None-Match": {etag}}); again.StatusCode != 304 {
		t.Errorf("asking again with the ETag: %d, want 304", again.StatusCode)
	}
	if large := cover(admin, base+"large", nil); large.StatusCode != 200 || large.Header.Get("ETag") == etag {
		t.Errorf("large cover: %d with ETag %q", large.StatusCode, large.Header.Get("ETag"))
	}
	key := strings.TrimSuffix(strings.Trim(etag, `"`), "-small")
	if pinned := cover(admin, base+"small?v="+key, nil); !strings.Contains(pinned.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("a cover asked for by its hash: Cache-Control %q, want it kept for good", pinned.Header.Get("Cache-Control"))
	}
	for why, path := range map[string]string{
		"an unknown size":       base + "huge",
		"a book without cover":  "/books/" + files["Broken.epub"].BookID.String() + "/covers/small",
		"a book that is no one": "/books/" + uuid.NewString() + "/covers/small",
	} {
		if resp := cover(admin, path, nil); resp.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", why, resp.StatusCode)
		}
	}
	// The library is private: to the reader its covers are not there.
	if resp := cover(reader, base+"small", nil); resp.StatusCode != 404 {
		t.Errorf("a cover of a hidden library: %d, want 404", resp.StatusCode)
	}
}

func TestEPUBsThatDifferOnlyInMetadataShareAContentHash(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	text := []string{"Chapter one of the same book.", "Chapter two of the same book."}
	writeEPUB(t, filepath.Join(books, "a/Emma.epub"), emmaMetadata, coverPNG(t), text...)
	writeEPUB(t, filepath.Join(books, "b/Emma.epub"), `<dc:title>EMMA (retail)</dc:title><dc:language>en</dc:language>`, nil, text...)
	writeEPUB(t, filepath.Join(books, "c/Emma.epub"), emmaMetadata, coverPNG(t), text[0], "Chapter two, abridged.")

	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}
	files := a.shelfFiles()
	first, second, third := files["a/Emma.epub"], files["b/Emma.epub"], files["c/Emma.epub"]
	if bytes.Equal(first.SHA256, second.SHA256) {
		t.Fatal("the two files are the same file; the test proves nothing")
	}
	if len(first.ContentSHA256) != 32 || !bytes.Equal(first.ContentSHA256, second.ContentSHA256) {
		t.Errorf("content hashes %x and %x, want the same", first.ContentSHA256, second.ContentSHA256)
	}
	if bytes.Equal(first.ContentSHA256, third.ContentSHA256) {
		t.Error("a file with other text has the same content hash")
	}
}

func TestWhatAFileSaysYieldsToBetterSources(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	first, second := filepath.Join(books, "emma.epub"), filepath.Join(books, "Emma.EPUB")
	writeEPUB(t, first, emmaMetadata, nil, "Text.")
	writeEPUB(t, second, `<dc:title>Emma: A Novel</dc:title><dc:creator>Austen</dc:creator>
		<dc:identifier>urn:uuid:123e4567-e89b-12d3-a456-426614174000</dc:identifier>`, coverPNG(t), "Text.")

	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	files := a.shelfFiles()
	if files["emma.epub"].BookID != files["Emma.EPUB"].BookID {
		t.Fatal("two files of one name in one folder are not one book")
	}
	bookID := files["emma.epub"].BookID

	// The first file read names the book; the second adds what was not said.
	a.extract(files["emma.epub"].ID)
	a.extract(files["Emma.EPUB"].ID)
	book := a.book(bookID)
	if book.Title != "Emma" || len(book.Contributors) != 2 || book.Contributors[0].Name != "Jane Austen" {
		t.Errorf("after the second file: %q by %+v, want what the first file said", book.Title, book.Contributors)
	}
	if len(book.Identifiers) != 2 {
		t.Errorf("identifiers = %+v, want one from each file", book.Identifiers)
	}
	var cover *string
	if err := a.pool.QueryRow(ctx, "SELECT cover_key FROM books WHERE id = $1", bookID).Scan(&cover); err != nil || cover == nil {
		t.Errorf("the cover only the second file has was not taken: %v", err)
	}

	// The first file changes: what it said before, it may say differently
	// now, except where a person has locked the field.
	if _, err := a.pool.Exec(ctx, "UPDATE books SET locked_fields = '{title}' WHERE id = $1", bookID); err != nil {
		t.Fatal(err)
	}
	changed := strings.NewReplacer("<dc:title>Emma</dc:title>", "<dc:title>Emma (2nd ed.)</dc:title>",
		"Penguin Classics", "Oxford University Press").Replace(emmaMetadata)
	writeEPUB(t, first, changed, nil, "Text, revised and longer than before.")
	a.scanNow(id)
	if state := a.shelfFiles()["emma.epub"].State; state != "pending" {
		t.Fatalf("the changed file is %s, want pending", state)
	}
	a.extract(files["emma.epub"].ID)
	book = a.book(bookID)
	if book.Title != "Emma" || book.Publisher != "Oxford University Press" {
		t.Errorf("after the change: %q, published by %q; want the locked title kept and the publisher updated", book.Title, book.Publisher)
	}
	if len(book.Identifiers) != 2 {
		t.Errorf("identifiers after reading the file again = %+v, want no duplicates", book.Identifiers)
	}
}

func TestAnEPUBOutranksAPDFOfTheSameBook(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	for _, name := range []string{"Emma.epub", "Emma.pdf"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "books", "Jane Austen", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(books, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A PDF's page one says what the book is called in its own words.
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	files := a.shelfFiles()
	pdf, epub := files["Emma.pdf"], files["Emma.epub"]
	if pdf.BookID != epub.BookID {
		t.Fatal("the EPUB and the PDF of Emma are not one book")
	}
	source := func() string {
		var s string
		if err := a.pool.QueryRow(ctx, "SELECT field_sources->>'title' FROM books WHERE id = $1", pdf.BookID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	a.extract(pdf.ID)
	if got := source(); !strings.HasPrefix(got, "file:pdf:") {
		t.Fatalf("after the PDF the title comes from %q", got)
	}
	file := a.shelfFiles()["Emma.pdf"]
	if file.State != "done" || file.HasText == nil || !*file.HasText || file.Pages == nil || *file.Pages != 1 {
		t.Errorf("the PDF after extraction: %+v", file)
	}
	a.extract(epub.ID)
	if got := source(); !strings.HasPrefix(got, "file:epub:") {
		t.Errorf("after the EPUB the title comes from %q, want the EPUB", got)
	}
	a.extract(pdf.ID)
	if got := source(); !strings.HasPrefix(got, "file:epub:") {
		t.Errorf("after reading the PDF again the title comes from %q, want the EPUB still", got)
	}
	if book := a.book(pdf.BookID); book.Title != "Emma" || len(book.Authors()) != 1 || book.Authors()[0] != "Jane Austen" {
		t.Errorf("the book is %q by %v", book.Title, book.Authors())
	}
}

func TestKindleFilesAreRead(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	for from, to := range map[string]string{
		"persuasion.mobi": "mobi/Persuasion.mobi",
		"persuasion.azw3": "azw3/Persuasion.azw3",
		"protected.azw":   "azw/Persuasion.azw",
	} {
		data, err := os.ReadFile(filepath.Join("..", "internal", "format", "mobi", "testdata", from))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(filepath.Join(books, to)), 0o755)
		if err := os.WriteFile(filepath.Join(books, to), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := a.externalLibrary(admin, "Kindle", "shared", books)
	a.scanNow(id)
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
	}

	files := a.shelfFiles()
	for _, path := range []string{"mobi/Persuasion.mobi", "azw3/Persuasion.azw3"} {
		f := files[path]
		if f.State != "done" || f.HasText == nil || !*f.HasText || len(f.ContentSHA256) != 32 {
			t.Errorf("%s after extraction: %+v", path, f)
		}
		book := a.book(f.BookID)
		if book.Title != "Persuasion" || book.Publisher != "Penguin" || book.Language != "en" ||
			len(book.Contributors) != 1 || book.Contributors[0].Name != "Jane Austen" {
			t.Errorf("%s: book %+v", path, book)
		}
		var cover *string
		if err := a.pool.QueryRow(ctx, "SELECT cover_key FROM books WHERE id = $1", f.BookID).Scan(&cover); err != nil || cover == nil {
			t.Errorf("%s: no cover (%v)", path, err)
		}
	}
	// The same text in both formats.
	if !bytes.Equal(files["mobi/Persuasion.mobi"].ContentSHA256, files["azw3/Persuasion.azw3"].ContentSHA256) {
		t.Error("the MOBI and the AZW3 of one text have different content hashes")
	}

	protected := files["azw/Persuasion.azw"]
	var drm bool
	if err := a.pool.QueryRow(ctx, "SELECT drm FROM book_files WHERE id = $1", protected.ID).Scan(&drm); err != nil {
		t.Fatal(err)
	}
	if !drm || protected.HasText == nil || *protected.HasText || protected.ContentSHA256 != nil || protected.State != "done" {
		t.Errorf("the protected file: drm %v, %+v; want it flagged, with no text read", drm, protected)
	}
	if book := a.book(protected.BookID); book.Title != "Persuasion" {
		t.Errorf("the protected file's book is called %q; its metadata is not protected", book.Title)
	}
}

// writeTone makes an audio file of the length with the tags, and with
// chapters when there are any, by asking ffmpeg for a tone.
func writeTone(t *testing.T, path string, seconds float64, tags map[string]string, chapters ...string) {
	t.Helper()
	var meta strings.Builder
	meta.WriteString(";FFMETADATA1\n")
	for k, v := range tags {
		fmt.Fprintf(&meta, "%s=%s\n", k, v)
	}
	step := int(seconds * 1000 / float64(max(len(chapters), 1)))
	for i, title := range chapters {
		fmt.Fprintf(&meta, "[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n", i*step, (i+1)*step, title)
	}
	metaPath := filepath.Join(t.TempDir(), "meta.txt")
	os.WriteFile(metaPath, []byte(meta.String()), 0o644)
	os.MkdirAll(filepath.Dir(path), 0o755)
	args := []string{"-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("sine=duration=%g", seconds), "-i", metaPath,
		"-map_metadata", "1", "-map_chapters", "1"}
	if strings.HasSuffix(path, ".m4b") {
		args = append(args, "-c:a", "aac", "-f", "ipod")
	}
	if out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
}

func TestAudiobooksAreRead(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed here; the toolchain image has it")
	}
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	// The names say one order, the tags another; the tags are right.
	for name, track := range map[string]string{"Emma 1.mp3": "2", "Emma 2.mp3": "3", "Emma 3.mp3": "1"} {
		writeTone(t, filepath.Join(books, "Austen", name), 1, map[string]string{
			"album": "Emma", "artist": "Jane Austen", "title": "Part " + track, "track": track,
		})
	}
	writeTone(t, filepath.Join(books, "Herbert", "Dune.m4b"), 3, map[string]string{"title": "Dune", "artist": "Frank Herbert"},
		"Book One", "Book Two", "Book Three")

	id := a.externalLibrary(admin, "Audio", "shared", books)
	a.scanNow(id)
	files := a.shelfFiles()
	for _, f := range files {
		a.extract(f.ID)
	}

	parts := map[string]int32{}
	var total int64
	rows, err := a.pool.Query(ctx, `SELECT rel_path, part_index, duration_ms FROM book_files WHERE book_id = $1`, files["Austen/Emma 1.mp3"].BookID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var path string
		var part int32
		var duration int64
		if err := rows.Scan(&path, &part, &duration); err != nil {
			t.Fatal(err)
		}
		parts[path] = part
		total += duration
	}
	if want := map[string]int32{"Austen/Emma 3.mp3": 0, "Austen/Emma 1.mp3": 1, "Austen/Emma 2.mp3": 2}; fmt.Sprint(parts) != fmt.Sprint(want) {
		t.Errorf("parts = %v, want the order of the track tags %v", parts, want)
	}
	if total < 2900 || total > 3300 {
		t.Errorf("the book lasts %d ms, want about 3000", total)
	}
	if book := a.book(files["Austen/Emma 1.mp3"].BookID); book.Title != "Emma" || len(book.Authors()) != 1 {
		t.Errorf("the book of the parts: %q by %v", book.Title, book.Authors())
	}

	dune := files["Herbert/Dune.m4b"]
	var chapters []string
	rows, err = a.pool.Query(ctx, "SELECT title || ' ' || start_ms || '-' || end_ms FROM audio_chapters WHERE file_id = $1 ORDER BY position", dune.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		rows.Scan(&c)
		chapters = append(chapters, c)
	}
	if want := []string{"Book One 0-1000", "Book Two 1000-2000", "Book Three 2000-3000"}; !slices.Equal(chapters, want) {
		t.Errorf("chapters = %v, want %v", chapters, want)
	}
	// Reading the file again gives the same chapters, not twice as many.
	a.extract(dune.ID)
	var n int
	a.pool.QueryRow(ctx, "SELECT count(*) FROM audio_chapters WHERE file_id = $1", dune.ID).Scan(&n)
	if n != 3 {
		t.Errorf("%d chapters after reading the file again, want 3", n)
	}
}
