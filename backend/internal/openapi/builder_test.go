package openapi

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type base struct {
	ID string `json:"id"`
}

type kind string

type book struct {
	base
	Title    string            `json:"title"`
	Subtitle *string           `json:"subtitle"`
	Rating   *int              `json:"rating,omitempty"`
	Tags     []string          `json:"tags"`
	Added    time.Time         `json:"added"`
	Kind     kind              `json:"kind"`
	Extra    map[string]string `json:"extra,omitempty"`
	Parent   *book             `json:"parent,omitempty"`
	Hidden   string            `json:"-"`
	private  string
}

func TestObjectFollowsEncodingJSON(t *testing.T) {
	b := NewBuilder()
	b.Enums[reflect.TypeOf(kind(""))] = []string{"ebook", "audio"}

	ref := b.SchemaOf(book{})
	if ref.Ref != "#/components/schemas/Book" {
		t.Fatalf("ref = %q, want the Book component", ref.Ref)
	}
	s := b.Components()["Book"]

	wantRequired := []string{"id", "title", "subtitle", "tags", "added", "kind"}
	if !reflect.DeepEqual(s.Required, wantRequired) {
		t.Errorf("required = %v, want %v", s.Required, wantRequired)
	}
	if s.Properties["Hidden"] != nil || s.Properties["private"] != nil {
		t.Error("a field encoding/json never writes is in the schema")
	}
	if got := s.Properties["id"]; got == nil || got.Type != "string" {
		t.Errorf("the embedded struct's field is not flattened: %+v", got)
	}
	if got := s.Properties["subtitle"].Type; !reflect.DeepEqual(got, []string{"string", "null"}) {
		t.Errorf("a pointer without omitempty is %v, want nullable", got)
	}
	if got := s.Properties["rating"].Type; got != "integer" {
		t.Errorf("a pointer with omitempty is %v, want absent rather than null", got)
	}
	if got := s.Properties["added"]; got.Type != "string" || got.Format != "date-time" {
		t.Errorf("time is %+v", got)
	}
	if got := s.Properties["kind"].Enum; !reflect.DeepEqual(got, []string{"ebook", "audio"}) {
		t.Errorf("enum = %v", got)
	}
	if got := s.Properties["tags"]; got.Type != "array" || got.Items.Type != "string" {
		t.Errorf("slice is %+v", got)
	}
	if got := s.Properties["parent"].Ref; got != "#/components/schemas/Book" {
		t.Errorf("a type that refers to itself is %q", got)
	}
}

func TestDocumentEncodesTheSameEveryTime(t *testing.T) {
	encode := func() string {
		b := NewBuilder()
		b.SchemaOf(book{})
		doc := &Document{OpenAPI: "3.1.0", Paths: map[string]PathItem{}}
		doc.Components.Schemas = b.Components()
		out, err := doc.MarshalIndent()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	first := encode()
	for range 5 {
		if encode() != first {
			t.Fatal("two runs encoded the document differently")
		}
	}
	if !json.Valid([]byte(first)) {
		t.Error("the document is not valid JSON")
	}
}
