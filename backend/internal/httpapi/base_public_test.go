package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicBaseForKeepsThePortOfAPublicAddress(t *testing.T) {
	public := []string{"https://books.example.org", "http://192.168.1.5:8080/gotome"}
	cases := []struct {
		name, host, proto, base, want string
	}{
		{"host without the port", "192.168.1.5", "", "", "http://192.168.1.5:8080"},
		{"host with its port is kept", "192.168.1.5:9000", "", "", "http://192.168.1.5:9000"},
		{"no address of that host", "10.0.0.7", "", "", "http://10.0.0.7"},
		{"name without a port in the address", "books.example.org", "https", "", "https://books.example.org"},
		{"case does not matter", "Books.Example.org", "https", "", "https://Books.Example.org"},
		{"sub-path", "192.168.1.5", "", "/gotome", "http://192.168.1.5:8080/gotome"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = c.host
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		if c.base != "" {
			r = r.WithContext(context.WithValue(r.Context(), basePathKey, c.base))
		}
		if got := publicBaseFor(r, public); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if got := publicBaseFor(r, nil); c.name == "host without the port" && got != "http://192.168.1.5" {
			t.Errorf("without public addresses Host stands: %q", got)
		}
	}
}
