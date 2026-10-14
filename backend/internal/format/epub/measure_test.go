package epub

import (
	"bytes"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"
)

func pngOf(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func measure(t *testing.T, data []byte) Measures {
	t.Helper()
	m, err := Measure(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	return m
}

// heavyOPF lists a large chapter, three fonts, a large picture and a file
// the archive does not hold.
const heavyOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">heavy</dc:identifier><dc:title>Heavy</dc:title><dc:language>de</dc:language>
  </metadata>
  <manifest>
    <item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="f1" href="fonts/DejaVuSerif.ttf" media-type="application/vnd.ms-opentype"/>
    <item id="f2" href="fonts/DejaVuSans.otf" media-type="font/otf"/>
    <item id="map" href="images/map.png" media-type="image/png"/>
    <item id="small" href="images/small.png" media-type="image/png"/>
    <item id="drawing" href="images/drawing.svg" media-type="image/svg+xml"/>
    <item id="gone" href="images/gone.jpg" media-type="image/jpeg"/>
    <item id="web" href="https://example.com/font.woff" media-type="font/woff"/>
  </manifest>
  <spine><itemref idref="ch1"/><itemref idref="ch2"/></spine>
</package>`

func TestMeasureFindsWhatIsSlow(t *testing.T) {
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", heavyOPF},
		entry{"OEBPS/text/ch1.xhtml", "<html><body><p>" + strings.Repeat("Geralt ritt weiter. ", 20000) + "</p></body></html>"},
		entry{"OEBPS/text/ch2.xhtml", "<html><body><p>Kurz.</p></body></html>"},
		entry{"OEBPS/fonts/DejaVuSerif.ttf", strings.Repeat("s", 700_000)},
		entry{"OEBPS/fonts/DejaVuSans.otf", strings.Repeat("a", 500_000)},
		entry{"OEBPS/fonts/unlisted.woff2", strings.Repeat("u", 1000)},
		entry{"OEBPS/images/map.png", pngOf(t, 2500, 1700)},
		entry{"OEBPS/images/small.png", pngOf(t, 10, 10) + strings.Repeat("\x00", 1_200_000)},
		entry{"OEBPS/images/drawing.svg", `<svg xmlns="http://www.w3.org/2000/svg" width="90000" height="90000"/>`},
	)
	m := measure(t, data)
	if m.LargestDocumentName != "OEBPS/text/ch1.xhtml" || m.LargestDocument < 400_000 {
		t.Errorf("largest document %q, %d bytes", m.LargestDocumentName, m.LargestDocument)
	}
	if m.Fonts != 3 || m.FontBytes != 1_201_000 || m.LargestFont != 700_000 {
		t.Errorf("fonts %d, %d bytes, largest %d", m.Fonts, m.FontBytes, m.LargestFont)
	}
	if m.ImageWidth != 2500 || m.ImageHeight != 1700 || m.ImagePixelsName != "OEBPS/images/map.png" {
		t.Errorf("image %dx%d %q", m.ImageWidth, m.ImageHeight, m.ImagePixelsName)
	}
	if m.LargestImageName != "OEBPS/images/small.png" || m.LargestImage < 1_200_000 {
		t.Errorf("largest image %q, %d bytes", m.LargestImageName, m.LargestImage)
	}
	if !slices.Equal(m.MissingResources, []string{"OEBPS/images/gone.jpg"}) {
		t.Errorf("missing %v", m.MissingResources)
	}
	if got := m.Findings(); !slices.Equal(got, Findings) {
		t.Errorf("findings %v", got)
	}
}

func TestMeasureFindsNothingInAnOrdinaryBook(t *testing.T) {
	// Named entities under an XHTML DTD are valid, and nothing here reads
	// them as broken.
	data := build(t,
		entry{"mimetype", "application/epub+zip"},
		container("OEBPS/content.opf"),
		entry{"OEBPS/content.opf", epub3OPF},
		entry{"OEBPS/nav.xhtml", `<html><body><nav><ol><li><a href="text/ch1.xhtml">One</a></li></ol></nav></body></html>`},
		entry{"OEBPS/text/ch1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Rittersporn&nbsp;sang &mdash; laut.</p></body></html>`},
		entry{"OEBPS/text/ch 2.xhtml", `<html><body><p>Two.</p></body></html>`},
		entry{"OEBPS/images/front.jpg", "\xff\xd8\xffcover-bytes"},
		entry{"OEBPS/style.css", "p { margin: 0 }"},
	)
	m := measure(t, data)
	if got := m.Findings(); len(got) != 0 {
		t.Errorf("findings %v in %+v", got, m)
	}
	if m.Fonts != 0 || m.LargestDocumentName == "" {
		t.Errorf("measures %+v", m)
	}
}

func TestMeasureRefusesWhatIsNoEPUB(t *testing.T) {
	if _, err := Measure(bytes.NewReader([]byte("no zip")), 6); err == nil {
		t.Error("measured a file that is no zip")
	}
}
