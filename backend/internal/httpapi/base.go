package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type basePathKeyType struct{}

var basePathKey basePathKeyType

// underBase serves h for the paths below base ("/gotome/") as if they were
// at the root, which is what the routes and the web app are written for, and
// answers every other path as not found. The probes stay at the root as
// well: the container's health check asks the process itself, not a proxy.
// A proxy must pass the prefix on (docs/reverse-proxy-sub-path.md).
func underBase(base string, h http.Handler) http.Handler {
	if base == "" || base == "/" {
		return h
	}
	bare := strings.TrimSuffix(base, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == bare:
			target := base
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
		case strings.HasPrefix(p, base):
			r2 := new(http.Request)
			*r2 = *r
			u := *r.URL
			u.Path = "/" + p[len(base):]
			u.RawPath = ""
			if raw := r.URL.RawPath; raw != "" {
				if rest, ok := strings.CutPrefix(raw, base); ok {
					u.RawPath = "/" + rest
				}
			}
			r2.URL = &u
			h.ServeHTTP(w, r2.WithContext(context.WithValue(r.Context(), basePathKey, bare)))
		case p == HealthPath || p == ReadyPath:
			h.ServeHTTP(w, r)
		default:
			writeError(w, r, ErrNotFound(""))
		}
	})
}

// basePrefix is the path GOtome is served under without its last slash:
// "/gotome", or "" at the root.
func basePrefix(r *http.Request) string {
	prefix, _ := r.Context().Value(basePathKey).(string)
	return prefix
}

// withBase puts the base path before an address on this server.
func withBase(r *http.Request, p string) string { return basePrefix(r) + p }

// publicBase is the scheme and host the browser used, with the base path.
func publicBase(r *http.Request) string {
	scheme := "http"
	if overTLS(r) {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: r.Host}).String() + basePrefix(r)
}

// publicBaseFor is publicBase for a client that may drop the port from Host
// (a Kobo reader does): when Host has none and one of the configured public
// addresses names the same host name, that address's host and port are used.
// Without a match Host stands as it came. The listener's own port is no
// substitute, as a container's published port is not the one it listens on.
func publicBaseFor(r *http.Request, public []string) string {
	host := r.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		for _, p := range public {
			u, err := url.Parse(p)
			if err == nil && u.Port() != "" && strings.EqualFold(u.Hostname(), strings.Trim(host, "[]")) {
				host = u.Host
				break
			}
		}
	}
	scheme := "http"
	if overTLS(r) {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: host}).String() + basePrefix(r)
}
