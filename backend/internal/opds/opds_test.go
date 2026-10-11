package opds

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAFeedIsAtomWithTheOPDSNamespaces(t *testing.T) {
	f := NewFeed("urn:test", "Books", time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	f.Links = []Link{{Rel: "self", Href: "/opds", Type: NavigationType}}
	f.Entries = []Entry{{
		ID: "urn:uuid:1", Title: "Dune & more", Updated: Time(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		Authors: []Person{{Name: "Frank Herbert"}}, Language: "en", Issued: "1965",
		Content: TextContent("A <desert> planet."),
		Links:   []Link{{Rel: RelAcquisition, Href: "/f/1", Type: "application/epub+zip", Length: 42}},
	}}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/terms/" xmlns:opds="http://opds-spec.org/2010/catalog">`,
		`<updated>2026-10-11T12:00:00Z</updated>`,
		`<title>Dune &amp; more</title>`,
		`<dc:language>en</dc:language>`,
		`<dc:issued>1965</dc:issued>`,
		`<content type="text">A &lt;desert&gt; planet.</content>`,
		`<link rel="http://opds-spec.org/acquisition" href="/f/1" type="application/epub+zip" length="42"></link>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("feed lacks %s:\n%s", want, got)
		}
	}
}

func TestTheSearchDescriptionHoldsTheTemplate(t *testing.T) {
	var buf bytes.Buffer
	if err := NewSearch("GOtome", "Search", "/opds/search?q={searchTerms}").Write(&buf); err != nil {
		t.Fatal(err)
	}
	want := `<Url type="` + AcquisitionType + `" template="/opds/search?q={searchTerms}"></Url>`
	if got := buf.String(); !strings.Contains(got, `<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">`) || !strings.Contains(got, want) {
		t.Errorf("description:\n%s", got)
	}
}
