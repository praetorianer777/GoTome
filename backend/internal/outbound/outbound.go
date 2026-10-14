// Package outbound is the one HTTP client GOtome reaches other servers
// with. It dials only addresses on the public internet, checked on the
// address a name resolved to as it is dialled, so neither a name that
// points inside nor a redirect to one gets through. Every request the
// server makes on someone's behalf goes through it: metadata providers,
// the embedding model's download, the embedding server and the identity
// provider.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrForbiddenAddress is a request to an address inside the server's own
// network, which nobody outside it has any business making it reach.
var ErrForbiddenAddress = errors.New("the address is not on the public internet")

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	maxRedirects     = 5
)

// Options configure a client.
type Options struct {
	// Timeout bounds a whole request, body included; zero is none, for
	// downloads whose length depends on the line.
	Timeout time.Duration
	// HeaderTimeout bounds the wait for an answer's header; zero is
	// Timeout, or 30 seconds without one.
	HeaderTimeout time.Duration
	// Trusted are addresses an administrator named, such as an identity
	// provider or an embedding server, which may well be on the local
	// network: the host and port of each may be dialled wherever they
	// resolve to. Only the same host and port is trusted, so a redirect
	// from there to another address inside is still refused.
	Trusted []string
	// AllowPrivate dials any address. Only tests set it, for servers on
	// the loopback.
	AllowPrivate bool
}

// NewClient returns a client that dials only public addresses, except the
// trusted ones. It uses no proxy: the guard would then check the proxy's
// address and not the one asked for.
func NewClient(o Options) *http.Client {
	header := o.HeaderTimeout
	if header == 0 {
		header = o.Timeout
	}
	if header == 0 {
		header = 30 * time.Second
	}
	trusted := map[string]bool{}
	for _, raw := range o.Trusted {
		if hp := hostPort(raw); hp != "" {
			trusted[hp] = true
		}
	}
	guarded := &net.Dialer{Timeout: dialTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		if o.AllowPrivate {
			return nil
		}
		return Check(address)
	}}
	open := &net.Dialer{Timeout: dialTimeout}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if trusted[strings.ToLower(address)] {
			return open.DialContext(ctx, network, address)
		}
		return guarded.DialContext(ctx, network, address)
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dial,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   handshakeTimeout,
			ResponseHeaderTimeout: header,
		},
		Timeout: o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return fmt.Errorf("redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// Check refuses an address, as host and port, that is not on the public
// internet. The host must be an IP address: names are checked once they
// resolved.
func Check(address string) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrForbiddenAddress, address)
	}
	if !Public(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrForbiddenAddress, ap.Addr())
	}
	return nil
}

// hostPort is the host and port a URL is dialled at, as the transport
// names it, or "" for what is not a web address.
func hostPort(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return ""
		}
	}
	return strings.ToLower(net.JoinHostPort(u.Hostname(), port))
}

// nonPublic are the ranges nobody on the internet lives in beside those
// netip names.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // shared address space, carrier NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

// Public reports whether an address is on the public internet.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
