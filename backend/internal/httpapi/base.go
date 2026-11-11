package httpapi

import (
	"context"
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
