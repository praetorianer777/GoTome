package httpapi

import (
	"compress/gzip"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/praetorianer777/gotome/backend/internal/webui"
)

// compressFrom is the size from which a JSON answer is sent compressed:
// below it, gzip's header and the work take more than they save.
const compressFrom = 1024

var gzipWriters = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return w
}}

// compressed sends a JSON answer of at least compressFrom bytes gzipped to a
// client that accepts it. Everything else passes as it is written: files,
// whose ranges and ETags are of their bytes, the event stream, which must
// reach the browser as each event is flushed, and images, which are
// compressed already.
func compressed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !webui.Accepts(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		// Not deferred: after a panic, recovery answers with the envelope,
		// and what the handler had written so far must not go out first.
		cw.finish()
	})
}

// compressWriter holds an answer back until it is known to be large enough
// to compress, or ends.
type compressWriter struct {
	http.ResponseWriter
	status  int
	held    []byte
	gz      *gzip.Writer
	through bool
}

func (c *compressWriter) WriteHeader(code int) {
	if c.through || c.gz != nil || c.status != 0 {
		c.ResponseWriter.WriteHeader(code)
		return
	}
	if code < http.StatusOK || code == http.StatusNoContent || code == http.StatusNotModified ||
		!compressible(c.Header()) {
		c.through = true
		c.ResponseWriter.WriteHeader(code)
		return
	}
	c.status = code
}

func (c *compressWriter) Write(b []byte) (int, error) {
	switch {
	case c.through:
		return c.ResponseWriter.Write(b)
	case c.gz != nil:
		return c.gz.Write(b)
	}
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
		if c.through {
			return c.ResponseWriter.Write(b)
		}
	}
	c.held = append(c.held, b...)
	if len(c.held) < compressFrom {
		return len(b), nil
	}
	h := c.Header()
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	c.ResponseWriter.WriteHeader(c.status)
	c.gz = gzipWriters.Get().(*gzip.Writer)
	c.gz.Reset(c.ResponseWriter)
	if _, err := c.gz.Write(c.held); err != nil {
		return 0, err
	}
	c.held = nil
	return len(b), nil
}

// Flush sends what is held, compressed or not, so that a flush means what
// it does without this writer.
func (c *compressWriter) Flush() {
	if c.gz != nil {
		_ = c.gz.Flush()
	} else if !c.through && c.status != 0 {
		c.release()
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer.
func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *compressWriter) release() {
	c.through = true
	c.ResponseWriter.WriteHeader(c.status)
	if len(c.held) > 0 {
		_, _ = c.ResponseWriter.Write(c.held)
	}
	c.held = nil
}

func (c *compressWriter) finish() {
	switch {
	case c.gz != nil:
		_ = c.gz.Close()
		c.gz.Reset(nil)
		gzipWriters.Put(c.gz)
		c.gz = nil
	case !c.through && c.status != 0:
		c.release()
	}
}

func compressible(h http.Header) bool {
	if h.Get("Content-Encoding") != "" {
		return false
	}
	media, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}
