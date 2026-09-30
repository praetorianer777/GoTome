package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/openapi"
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

// decodeQuery reads a route's query parameters into dst, a pointer to the
// struct the route declares as its Query. A parameter that is left out keeps
// its zero value.
func decodeQuery(r *http.Request, dst any) error {
	values := r.URL.Query()
	v := reflect.ValueOf(dst).Elem()
	fields := map[string]string{}
	for i := range v.NumField() {
		f := v.Type().Field(i)
		name := f.Tag.Get("query")
		raw := values.Get(name)
		if name == "" || raw == "" {
			continue
		}
		if enum := f.Tag.Get("enum"); enum != "" && !slices.Contains(strings.Split(enum, ","), raw) {
			fields[name] = "Use one of: " + strings.ReplaceAll(enum, ",", ", ") + "."
			continue
		}
		switch f.Type.Kind() {
		case reflect.String:
			v.Field(i).SetString(raw)
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				fields[name] = "Give a whole number above zero."
				continue
			}
			v.Field(i).SetInt(int64(n))
		}
	}
	if len(fields) > 0 {
		return ErrValidation(fields)
	}
	return nil
}

// queryParameters describes the fields of a route's Query for the OpenAPI
// document.
func queryParameters(query any) []openapi.Parameter {
	if query == nil {
		return nil
	}
	t := reflect.TypeOf(query)
	var params []openapi.Parameter
	for i := range t.NumField() {
		f := t.Field(i)
		name := f.Tag.Get("query")
		if name == "" {
			continue
		}
		schema := &openapi.Schema{Type: "string"}
		if f.Type.Kind() == reflect.Int {
			schema.Type = "integer"
		}
		if enum := f.Tag.Get("enum"); enum != "" {
			schema.Enum = strings.Split(enum, ",")
		}
		params = append(params, openapi.Parameter{Name: name, In: "query", Schema: schema, Description: f.Tag.Get("doc")})
	}
	return params
}
