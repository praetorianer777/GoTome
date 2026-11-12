package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// client is who sent a request and how the browser reached the server, as
// far as the connection and the proxies trusted to say so tell.
type client struct {
	address string
	https   bool
}

// forwarded works out the client of every request once, so that every
// per-address limit and every address handed out agrees on it. Only a
// connection from one of the trusted proxies may name another address
// (X-Forwarded-For) or the scheme the browser used (X-Forwarded-Proto). With
// none trusted, X-Forwarded-Proto is still taken from anyone, as before
// there was a choice: a plain HTTP installation behind a proxy depends on it.
func forwarded(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := clientOf(r, trusted)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey, c)))
		})
	}
}

func clientOf(r *http.Request, trusted []netip.Prefix) client {
	peer := peerAddress(r)
	if len(trusted) == 0 {
		return client{address: peer, https: r.TLS != nil || forwardedHTTPS(r)}
	}
	addr, err := parseHop(peer)
	if err != nil || !isTrusted(addr, trusted) {
		return client{address: peer, https: r.TLS != nil}
	}
	return client{address: forwardedFor(r, addr, trusted).String(), https: r.TLS != nil || forwardedHTTPS(r)}
}

// forwardedFor is the rightmost address of X-Forwarded-For that is not a
// trusted proxy's: everything left of it was written by the client itself
// and may be anything. A hop that is no address ends the walk at the proxy
// that passed it on.
func forwardedFor(r *http.Request, peer netip.Addr, trusted []netip.Prefix) netip.Addr {
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	current := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		addr, err := parseHop(hop)
		if err != nil {
			return current
		}
		current = addr
		if !isTrusted(addr, trusted) {
			return addr
		}
	}
	return current
}

// forwardedHTTPS reads the scheme the last proxy saw.
func forwardedHTTPS(r *http.Request) bool {
	values := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(values[len(values)-1]), "https")
}

func parseHop(s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		port, perr := netip.ParseAddrPort(s)
		if perr != nil {
			return netip.Addr{}, err
		}
		addr = port.Addr()
	}
	return addr.Unmap().WithZone(""), nil
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientAddress is the address a request came from: the connection's, or
// the one a trusted proxy named. Every per-address limit counts by it.
func clientAddress(r *http.Request) string {
	if c, ok := r.Context().Value(clientKey).(client); ok {
		return c.address
	}
	return clientOf(r, nil).address
}

// overTLS reports whether the browser reached the server over HTTPS, directly
// or through a reverse proxy that says so. Home installations often run plain
// HTTP on a local network, where a Secure cookie would never be sent back.
func overTLS(r *http.Request) bool {
	if c, ok := r.Context().Value(clientKey).(client); ok {
		return c.https
	}
	return clientOf(r, nil).https
}
