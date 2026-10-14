package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func get(t *testing.T, c *http.Client, url string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func counting(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestInternalAddressesAreRefused(t *testing.T) {
	inside, hits := counting(t, func(w http.ResponseWriter, r *http.Request) {})
	c := NewClient(Options{})
	if err := get(t, c, inside.URL); !errors.Is(err, ErrForbiddenAddress) {
		t.Errorf("a loopback address: %v", err)
	}
	// A name is checked by what it resolves to.
	byName := strings.Replace(inside.URL, "127.0.0.1", "localhost", 1)
	if err := get(t, c, byName); !errors.Is(err, ErrForbiddenAddress) {
		t.Errorf("a name that resolves inside: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("the server inside was reached %d times", hits.Load())
	}
}

func TestATrustedAddressIsReachedButNotWhereItRedirects(t *testing.T) {
	inside, insideHits := counting(t, func(w http.ResponseWriter, r *http.Request) {})
	trusted, trustedHits := counting(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, inside.URL, http.StatusFound)
		}
	})
	c := NewClient(Options{Trusted: []string{trusted.URL + "/some/path"}})
	if err := get(t, c, trusted.URL+"/here"); err != nil {
		t.Fatalf("the trusted address: %v", err)
	}
	if err := get(t, c, trusted.URL+"/away"); !errors.Is(err, ErrForbiddenAddress) {
		t.Errorf("a redirect from the trusted address inside: %v", err)
	}
	if trustedHits.Load() != 2 || insideHits.Load() != 0 {
		t.Errorf("trusted reached %d times, inside %d", trustedHits.Load(), insideHits.Load())
	}
}

func TestOnlyWebAddressesAreFollowed(t *testing.T) {
	srv, _ := counting(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	if err := get(t, NewClient(Options{AllowPrivate: true}), srv.URL); err == nil || errors.Is(err, ErrForbiddenAddress) {
		t.Errorf("a redirect to a file: %v", err)
	}
}

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2a00:1450:4001::200e": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false,
		"fd00::1": false, "fe80::1": false, "::ffff:127.0.0.1": false, "224.0.0.1": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v", addr, got)
		}
	}
}
