package covers

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func pngOf(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decode(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestPutStoresBothSizesOnce(t *testing.T) {
	store := NewStore(t.TempDir())
	data := pngOf(t, 1600, 2400, color.NRGBA{R: 200, A: 255})

	key, err := store.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	for size, want := range map[Size]image.Point{Small: {400, 600}, Large: {1000, 1500}} {
		path, err := store.Path(key, size)
		if err != nil {
			t.Fatalf("%s: %v", size, err)
		}
		if got := decode(t, path).Bounds().Size(); got != want {
			t.Errorf("%s is %v, want %v", size, got, want)
		}
	}

	small, _ := store.Path(key, Small)
	before, _ := os.Stat(small)
	again, err := store.Put(data)
	after, _ := os.Stat(small)
	if err != nil || again != key || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("storing the same image again: key %s (%v), rewritten %v", again, err, !after.ModTime().Equal(before.ModTime()))
	}
}

func TestPutDoesNotEnlargeAndPutsTransparencyOnWhite(t *testing.T) {
	store := NewStore(t.TempDir())
	key, err := store.Put(pngOf(t, 120, 180, color.NRGBA{}))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.Path(key, Large)
	img := decode(t, path)
	if got := img.Bounds().Size(); got != (image.Point{120, 180}) {
		t.Errorf("a small cover became %v", got)
	}
	if r, g, b, _ := img.At(60, 90).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Errorf("a transparent pixel became %d %d %d, want white", r>>8, g>>8, b>>8)
	}
}

func TestPutRefusesWhatIsNoCover(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Put([]byte("<html>not an image</html>")); !errors.Is(err, ErrNotAnImage) {
		t.Errorf("text: %v, want ErrNotAnImage", err)
	}
	// A PNG header that declares an image of a hundred thousand pixels a
	// side: small to send, ruinous to decode.
	bomb := pngOf(t, 1, 1, color.Black)
	copy(bomb[16:24], []byte{0, 1, 0x86, 0xa0, 0, 1, 0x86, 0xa0})
	if _, err := store.Put(bomb); !errors.Is(err, ErrNotAnImage) {
		t.Errorf("an oversized image: %v, want ErrNotAnImage", err)
	}
}

func TestPathRefusesKeysThatAreNotKeys(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, key := range []string{"", "../../etc/passwd", "abc", "ZZ" + string(make([]byte, 62))} {
		if _, err := store.Path(key, Small); !errors.Is(err, ErrNotFound) {
			t.Errorf("key %q: %v, want ErrNotFound", key, err)
		}
	}
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := store.Path(valid, Small); !errors.Is(err, ErrNotFound) {
		t.Errorf("a cover that was never stored: %v, want ErrNotFound", err)
	}
	if _, err := store.Path(valid, Size("huge")); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown size: %v, want ErrNotFound", err)
	}
}
