package secret

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func box(t *testing.T) *Box {
	t.Helper()
	encoded, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParseKey(encoded + "\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealAndOpen(t *testing.T) {
	b := box(t)
	sealed, err := b.Seal("metadata.hardcoverToken", []byte("tok-123"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("tok-123")) {
		t.Fatal("the sealed value contains the plain one")
	}
	again, _ := b.Seal("metadata.hardcoverToken", []byte("tok-123"))
	if bytes.Equal(sealed, again) {
		t.Error("the same value sealed twice looks the same")
	}
	got, err := b.Open("metadata.hardcoverToken", sealed)
	if err != nil || string(got) != "tok-123" {
		t.Fatalf("open: %q, %v", got, err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	for what, open := range map[string]func() ([]byte, error){
		"another name":  func() ([]byte, error) { return b.Open("metadata.googleBooksKey", sealed) },
		"another key":   func() ([]byte, error) { return box(t).Open("metadata.hardcoverToken", sealed) },
		"a changed bit": func() ([]byte, error) { return b.Open("metadata.hardcoverToken", tampered) },
		"too short":     func() ([]byte, error) { return b.Open("metadata.hardcoverToken", sealed[:5]) },
		"no version":    func() ([]byte, error) { return b.Open("metadata.hardcoverToken", append([]byte{9}, sealed[1:]...)) },
	} {
		if _, err := open(); !errors.Is(err, ErrOpen) {
			t.Errorf("%s: err = %v, want ErrOpen", what, err)
		}
	}
}

func TestParseKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xfb}, KeyLen)
	for _, s := range []string{
		base64.RawURLEncoding.EncodeToString(raw),
		base64.StdEncoding.EncodeToString(raw),
		"  " + base64.URLEncoding.EncodeToString(raw) + "\n",
	} {
		if key, err := ParseKey(s); err != nil || !bytes.Equal(key, raw) {
			t.Errorf("%q: %x %v", s, key, err)
		}
	}
	for s, want := range map[string]string{
		"not base64!": "not base64",
		base64.RawURLEncoding.EncodeToString(raw[:16]): "16 bytes long",
		"": "0 bytes long",
	} {
		if _, err := ParseKey(s); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one saying %q", s, err, want)
		}
	}
}
