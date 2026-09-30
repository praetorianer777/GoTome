package httpapi

import (
	"strconv"
	"strings"
	"testing"
)

func TestEveryRouteIsDocumented(t *testing.T) {
	doc := Spec()
	ids := map[string]bool{}
	for _, rt := range testServer().routes() {
		name := rt.Method + " " + rt.Path
		if rt.ID == "" || rt.Summary == "" || rt.Tag == "" || rt.Handler == nil {
			t.Errorf("%s: a route needs an ID, a summary, a tag and a handler", name)
		}
		if !strings.HasPrefix(rt.Path, "/") {
			t.Errorf("%s: the path must start with a slash", name)
		}
		if ids[rt.ID] {
			t.Errorf("%s: operation ID %q is used twice", name, rt.ID)
		}
		ids[rt.ID] = true

		op := doc.Paths[rt.Path][strings.ToLower(rt.Method)]
		if op == nil {
			t.Errorf("%s is not in the document", name)
			continue
		}
		success := op.Responses[strconv.Itoa(rt.successStatus())]
		if success == nil {
			t.Errorf("%s: no response for status %d", name, rt.successStatus())
		} else if (rt.Response != nil) != (success.Content[jsonMedia].Schema != nil) {
			t.Errorf("%s: the response schema does not match the route", name)
		}
		if (rt.Request != nil) != (op.RequestBody != nil) {
			t.Errorf("%s: the request body does not match the route", name)
		}
		if failure := op.Responses["default"]; failure == nil || failure.Content[jsonMedia].Schema.Ref != "#/components/schemas/ErrorEnvelope" {
			t.Errorf("%s: failures are not documented as the error envelope", name)
		}
	}

	documented := 0
	for _, item := range doc.Paths {
		documented += len(item)
	}
	if documented != len(ids) {
		t.Errorf("the document has %d operations, the route table %d", documented, len(ids))
	}
}

func TestErrorEnvelopeSchema(t *testing.T) {
	schemas := Spec().Components.Schemas
	apiErr := schemas["APIError"]
	if apiErr == nil || schemas["ErrorEnvelope"] == nil {
		t.Fatalf("components = %v, want ErrorEnvelope and APIError", schemas)
	}
	for _, field := range []string{"code", "message", "fields", "requestId"} {
		if apiErr.Properties[field] == nil {
			t.Errorf("APIError has no %s", field)
		}
	}
	if apiErr.Properties["Status"] != nil || apiErr.Properties["status"] != nil {
		t.Error("the HTTP status is in the schema, but never in the body")
	}
}

func TestPathParametersAreDocumented(t *testing.T) {
	m := pathParam.FindAllStringSubmatch("/books/{bookId}/files/{fileId}", -1)
	if len(m) != 2 || m[0][1] != "bookId" || m[1][1] != "fileId" {
		t.Errorf("parameters = %v", m)
	}
}
