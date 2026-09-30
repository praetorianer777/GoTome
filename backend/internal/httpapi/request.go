package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxBodyBytes caps JSON request bodies so a malformed or hostile client cannot
// make the server allocate without bound.
const maxBodyBytes = 1 << 20

// decodeJSON reads a JSON request body, rejecting unknown fields so that a typo
// in a client payload is an error rather than a silent no-op.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return ErrBadRequest("The request body is too large.")
		case errors.Is(err, io.EOF):
			return ErrBadRequest("The request needs a JSON body.")
		default:
			return ErrBadRequest("The request body is not valid JSON: " + err.Error() + ".")
		}
	}
	if dec.More() {
		return ErrBadRequest("Send a single JSON object as the request body.")
	}
	return nil
}
