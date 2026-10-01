package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

var modified = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func rewrite(t *testing.T, data []byte, u Update) []byte {
	t.Helper()
	if u.Modified.IsZero() {
		u.Modified = modified
	}
	var out bytes.Buffer
	if err := Rewrite(bytes.NewReader(data), int64(len(data)), &out, u); err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	checkStructure(t, out.Bytes())
	return out.Bytes()
}

// checkStructure holds a written EPUB to the rules of the container and the
// package document that epubcheck checks first.
func checkStructure(t *testing.T, data []byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	first := zr.File[0]
	if first.Name != "mimetype" || first.Method != zip.Store || len(first.Extra) != 0 {
		t.Errorf("first entry %q, method %d, extra %d bytes", first.Name, first.Method, len(first.Extra))
	}
	a := newArchive(zr, DefaultLimits)
	if mt, _ := a.read("mimetype"); string(mt) != "application/epub+zip" {
		t.Errorf("mimetype = %q", mt)
	}
	names := map[string]int{}
	for _, f := range zr.File {
		names[f.Name]++
		if names[f.Name] > 1 {
			t.Errorf("entry %q twice", f.Name)
		}
	}
	opfPath, err := a.packagePath()
	if err != nil {
		t.Fatal(err)
	}
	opf, err := a.read(opfPath)
	if err != nil {
		t.Fatal(err)
	}

	// Well formed, strictly, with every element's namespace bound.
	d := xml.NewDecoder(bytes.NewReader(opf))
	ids := map[string]string{}
	var refines []string
	modifiedCount, coverImages := 0, 0
	type element struct{ space, local string }
	var dcSeen []element
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("package document: %v\n%s", err, opf)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space == "" || strings.HasPrefix(start.Name.Space, "xmlns") || !strings.Contains(start.Name.Space, "/") {
			t.Errorf("element %s has no namespace", start.Name.Local)
		}
		if start.Name.Space == nsDC {
			dcSeen = append(dcSeen, element{start.Name.Space, start.Name.Local})
		}
		for _, at := range start.Attr {
			switch {
			case at.Name.Local == "id":
				if _, dup := ids[at.Value]; dup {
					t.Errorf("id %q twice", at.Value)
				}
				ids[at.Value] = start.Name.Local
			case at.Name.Local == "refines":
				refines = append(refines, strings.TrimPrefix(at.Value, "#"))
			case at.Name.Local == "property" && at.Value == "dcterms:modified":
				modifiedCount++
			case at.Name.Local == "properties" && slices.Contains(strings.Fields(at.Value), "cover-image"):
				coverImages++
			}
		}
	}
	for _, r := range refines {
		if _, ok := ids[r]; !ok {
			t.Errorf("refines #%s, which is not there", r)
		}
	}
	for _, local := range []string{"title", "language", "identifier"} {
		if !slices.Contains(dcSeen, element{nsDC, local}) {
			t.Errorf("no dc:%s", local)
		}
	}
	pkg, err := parsePackage(opf)
	if err != nil {
		t.Fatal(err)
	}
	var unique struct {
		ID string `xml:"unique-identifier,attr"`
	}
	_ = xml.Unmarshal(opf, &unique)
	if ids[unique.ID] != "identifier" {
		t.Errorf("unique-identifier %q names %q", unique.ID, ids[unique.ID])
	}
	if strings.HasPrefix(pkg.Version, "3") && modifiedCount != 1 {
		t.Errorf("%d dcterms:modified", modifiedCount)
	}
	if coverImages > 1 {
		t.Errorf("%d items are the cover image", coverImages)
	}
	listed := map[string]bool{"mimetype": true, opfPath: true}
	for _, it := range pkg.Manifest {
		href, err := resolve(opfPath, it.Href)
		if err != nil || a.find(href) == nil {
			t.Errorf("manifest item %q points at %q, which is not there", it.ID, it.Href)
			continue
		}
		listed[a.find(href).Name] = true
	}
	for name := range names {
		if !listed[name] && !strings.HasPrefix(name, "META-INF/") {
			t.Errorf("entry %q is in no manifest item", name)
		}
	}
}

func index(f float64) *float64 { return &f }

var newCover = &Cover{MediaType: "image/jpeg", Data: []byte("\xff\xd8\xffnew-cover")}

var described = Metadata{
	Title:    "Words of Radiance",
	Subtitle: "Book Two <of> the Stormlight Archive",
	Contributors: []Contributor{
		{Name: "Brandon Sanderson", FileAs: "Sanderson, Brandon", Role: "aut"},
		{Name: "Kate Reading", Role: "nrt"},
	},
	Language:    "en-GB",
	Identifiers: []Identifier{{"isbn", "9780765326362"}, {"asin", "B00DA6YEKS"}, {"isbn", "978-3-16-148410-0"}},
	Publisher:   "Tor & Sons",
	Published:   "2014-03",
	Description: "Szeth & Kaladin.\n\nA second paragraph.",
	Subjects:    []string{"Fantasy", "Épique"},
	Series:      "Stormlight",
	SeriesIndex: index(2.5),
}

func TestRewriteEPUB3(t *testing.T) {
	original := epub3(t)
	before := parse(t, original)
	written := rewrite(t, original, Update{Metadata: described, Cover: newCover})
	after := parse(t, written)
	md := after.Metadata

	if md.Title != described.Title || md.Subtitle != described.Subtitle || md.Language != "en-GB" ||
		md.Publisher != "Tor & Sons" || md.Published != "2014-03" || md.Description != "Szeth & Kaladin. A second paragraph." {
		t.Errorf("metadata = %+v", md)
	}
	if !slices.Equal(md.Contributors, described.Contributors) {
		t.Errorf("contributors = %+v", md.Contributors)
	}
	// The package's own identifier stays, and is not written twice.
	wantIDs := []Identifier{{"isbn", "978-3-16-148410-0"}, {"isbn", "9780765326362"}, {"asin", "B00DA6YEKS"}}
	if !slices.Equal(md.Identifiers, wantIDs) {
		t.Errorf("identifiers = %+v", md.Identifiers)
	}
	if !slices.Equal(md.Subjects, described.Subjects) || md.Series != "Stormlight" || md.SeriesIndex == nil || *md.SeriesIndex != 2.5 {
		t.Errorf("subjects %v, series %q %v", md.Subjects, md.Series, md.SeriesIndex)
	}
	if after.Cover == nil || !bytes.Equal(after.Cover.Data, newCover.Data) {
		t.Errorf("cover = %+v", after.Cover)
	}
	if !bytes.Equal(after.ContentHash, before.ContentHash) || after.Text() != before.Text() {
		t.Error("the content changed")
	}
	// The old cover image stays where the book shows it.
	if _, err := newArchive(mustZip(t, written), DefaultLimits).read("OEBPS/images/front.jpg"); err != nil {
		t.Error(err)
	}
}

func mustZip(t *testing.T, data []byte) *zip.Reader {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestRewritingAgainReplacesWhatWasWritten(t *testing.T) {
	once := rewrite(t, epub3(t), Update{Metadata: described, Cover: newCover})
	other := &Cover{MediaType: "image/png", Data: []byte("\x89PNG-other")}
	changed := described
	changed.Title, changed.Series = "Oathbringer", ""
	twice := rewrite(t, once, Update{Metadata: changed, Cover: other})

	book := parse(t, twice)
	if book.Metadata.Title != "Oathbringer" || book.Metadata.Series != "" || !bytes.Equal(book.Cover.Data, other.Data) {
		t.Errorf("after the second rewrite: %+v, cover %q", book.Metadata, book.Cover.Data)
	}
	var covers []string
	for _, f := range mustZip(t, twice).File {
		if strings.Contains(f.Name, "gotome-cover") {
			covers = append(covers, f.Name)
		}
	}
	if len(covers) != 1 {
		t.Errorf("cover entries %v", covers)
	}
	opf, _ := newArchive(mustZip(t, twice), DefaultLimits).read("OEBPS/content.opf")
	if bytes.Contains(opf, []byte("gotome-title-2")) || bytes.Count(opf, []byte("calibre:series")) != 0 {
		t.Errorf("leftovers of the first rewrite:\n%s", opf)
	}
	// What GOtome does not manage is still there.
	if !bytes.Contains(opf, []byte(`id="css"`)) || !bytes.Contains(opf, []byte(`<itemref idref="ch1"/>`)) {
		t.Errorf("the manifest or spine changed:\n%s", opf)
	}
}

func TestRewriteEPUB2(t *testing.T) {
	original := epub2(t)
	before := parse(t, original)
	written := rewrite(t, original, Update{Metadata: Metadata{
		Title:        "Die Verwandlung (Reclam)",
		Subtitle:     "has no place in EPUB 2",
		Contributors: []Contributor{{Name: "Franz Kafka", FileAs: "Kafka, Franz", Role: "aut"}, {Name: "Ünal Über", Role: "trl"}},
		Identifiers:  []Identifier{{"isbn", "3150091004"}, {"asin", "B000FC1PJI"}},
		Series:       "Erzählungen",
		SeriesIndex:  index(3),
	}})
	after := parse(t, written)
	md := after.Metadata

	if md.Title != "Die Verwandlung (Reclam)" || md.Subtitle != "" || md.Language != "de" || md.Published != "" {
		t.Errorf("metadata = %+v", md)
	}
	if len(md.Contributors) != 2 || md.Contributors[1] != (Contributor{"Ünal Über", "", "trl"}) {
		t.Errorf("contributors = %+v", md.Contributors)
	}
	if !slices.Equal(md.Identifiers, []Identifier{{"isbn", "3150091004"}, {"asin", "B000FC1PJI"}}) {
		t.Errorf("identifiers = %+v", md.Identifiers)
	}
	if md.Series != "Erzählungen" || *md.SeriesIndex != 3 {
		t.Errorf("series %q %v", md.Series, md.SeriesIndex)
	}
	// Left alone: the cover, and the content.
	if after.Cover == nil || after.Cover.MediaType != "image/png" || !bytes.Equal(after.ContentHash, before.ContentHash) {
		t.Errorf("cover %+v, content the same: %v", after.Cover, bytes.Equal(after.ContentHash, before.ContentHash))
	}
	opf, _ := newArchive(mustZip(t, written), DefaultLimits).read("content.opf")
	if !bytes.HasPrefix(opf, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Errorf("declaration: %s", opf[:60])
	}
}

func TestRewriteRemovesTheCover(t *testing.T) {
	written := rewrite(t, epub3(t), Update{Metadata: described, RemoveCover: true})
	if book := parse(t, written); book.Cover != nil {
		t.Errorf("cover = %+v", book.Cover)
	}
	written = rewrite(t, epub2(t), Update{Metadata: described, RemoveCover: true})
	// The EPUB 2 image is still found by its name, as a file without any
	// marked cover would be.
	if book := parse(t, written); book.Cover == nil {
		t.Error("the image called cover is gone")
	}
}

func TestRewriteRefuses(t *testing.T) {
	encrypted := build(t,
		container("p.opf"),
		entry{"p.opf", `<package version="2.0"><metadata><title>Locked</title></metadata><manifest/></package>`},
		entry{encryptionPath, `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
			<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
			<EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`},
	)
	brokenOPF := build(t,
		container("p.opf"),
		entry{"p.opf", `<package version="2.0"><metadata><title>Broken</metadata></package>`},
	)
	for name, data := range map[string][]byte{"encrypted": encrypted, "broken package document": brokenOPF} {
		err := Rewrite(bytes.NewReader(data), int64(len(data)), io.Discard, Update{Metadata: described})
		if !errors.Is(err, ErrNotWritable) {
			t.Errorf("%s: %v, want ErrNotWritable", name, err)
		}
	}
	if err := Rewrite(bytes.NewReader([]byte("no zip")), 6, io.Discard, Update{Metadata: described}); !errors.Is(err, ErrNotEPUB) {
		t.Errorf("not a zip: %v", err)
	}
}

type failingWriter struct{ left int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		n := w.left
		w.left = 0
		return n, errors.New("disk full")
	}
	w.left -= len(p)
	return len(p), nil
}

func TestRewriteReportsAFailedWrite(t *testing.T) {
	data := epub3(t)
	err := Rewrite(bytes.NewReader(data), int64(len(data)), &failingWriter{left: 200}, Update{Metadata: described, Modified: modified})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v", err)
	}
}

func FuzzRewrite(f *testing.F) {
	f.Add([]byte(epub3OPF))
	f.Add([]byte(epub2OPF))
	f.Fuzz(func(t *testing.T, opf []byte) {
		data := build(t, entry{"mimetype", "application/epub+zip"}, container("p.opf"), entry{"p.opf", string(opf)})
		var out bytes.Buffer
		if err := Rewrite(bytes.NewReader(data), int64(len(data)), &out, Update{Metadata: described, Cover: newCover}); err != nil {
			return
		}
		// Whatever is written can be read again, as well formed as before.
		if _, err := Parse(bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
			t.Fatalf("written but not readable: %v", err)
		}
	})
}
