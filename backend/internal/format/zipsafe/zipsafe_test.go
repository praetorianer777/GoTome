package zipsafe

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

type entry struct {
	name string
	data []byte
	mode fs.FileMode
}

func archive(t *testing.T, entries ...entry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// prose is text that packs as a book's does, a few times over and no more.
func prose(n int) []byte {
	words := strings.Fields("it was a dark and stormy night the rain fell in torrents except at occasional intervals when it was checked by a violent gust of wind which swept up the streets")
	var b bytes.Buffer
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%s%d ", words[(i*7919)%len(words)], i%97)
	}
	return b.Bytes()
}

func open(r *bytes.Reader, limits Limits) error {
	_, err := Open(r, r.Size(), limits)
	return err
}

func TestARealArchiveOpens(t *testing.T) {
	r := archive(t,
		entry{name: "mimetype", data: []byte("application/epub+zip")},
		entry{name: "OEBPS/", data: nil},
		entry{name: "OEBPS/text.xhtml", data: prose(2 << 20)},
		entry{name: `OEBPS\from windows.xhtml`, data: []byte("<p/>")},
	)
	if err := open(r, DefaultLimits); err != nil {
		t.Fatal(err)
	}
}

func TestAZipBombIsRefused(t *testing.T) {
	r := archive(t, entry{name: "OEBPS/bomb.xhtml", data: make([]byte, 64<<20)})
	if r.Size() > 1<<20 {
		t.Fatalf("the bomb is %d bytes", r.Size())
	}
	if err := open(r, DefaultLimits); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestTooMuchIsRefused(t *testing.T) {
	r := archive(t, entry{name: "a", data: []byte("a")}, entry{name: "b", data: []byte("b")}, entry{name: "c", data: []byte("c")})
	if err := open(r, Limits{MaxEntries: 2, MaxTotalBytes: 1 << 20, MaxRatio: 200}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many entries: %v", err)
	}
	if err := open(r, Limits{MaxEntries: 10, MaxTotalBytes: 2, MaxRatio: 200}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many bytes: %v", err)
	}
}

func TestUnsafeEntriesAreRefused(t *testing.T) {
	for name, e := range map[string]entry{
		"a name that climbs out":       {name: "OEBPS/../../etc/passwd", data: []byte("x")},
		"a name that climbs out on NT": {name: `OEBPS\..\..\boot.ini`, data: []byte("x")},
		"an absolute name":             {name: "/etc/passwd", data: []byte("x")},
		"a drive":                      {name: `C:\Windows\win.ini`, data: []byte("x")},
		"a link":                       {name: "OEBPS/cover.jpg", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777},
	} {
		r := archive(t, entry{name: "mimetype", data: []byte("application/epub+zip")}, e)
		if err := open(r, DefaultLimits); !errors.Is(err, ErrUnsafe) {
			t.Errorf("%s: err = %v, want ErrUnsafe", name, err)
		}
	}
	twice := archive(t, entry{name: "OEBPS/a.xhtml", data: []byte("one")}, entry{name: "OEBPS/a.xhtml", data: []byte("two")})
	if err := open(twice, DefaultLimits); !errors.Is(err, ErrUnsafe) {
		t.Errorf("a name twice: err = %v, want ErrUnsafe", err)
	}
}

func TestWhatIsNoZipIsSaidSo(t *testing.T) {
	r := bytes.NewReader([]byte("PK\x03\x04 and nothing of a zip after it"))
	if err := open(r, DefaultLimits); !errors.Is(err, ErrNotZip) {
		t.Errorf("err = %v, want ErrNotZip", err)
	}
}
