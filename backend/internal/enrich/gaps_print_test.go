package enrich

import (
	"context"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

func TestARecordFoundByAPrintISBNDoesNotGiveItToTheBook(t *testing.T) {
	s := &Service{}
	book := catalog.Book{Title: "Emma", PrintISBNs: []string{"9780141439587"}}
	record := metadata.Record{Identifiers: []catalog.Identifier{
		{Type: catalog.IDISBN, Value: "9780141439587"}, {Type: catalog.IDISBN, Value: "9780141439600"},
	}}
	e, err := s.gaps(context.Background(), book, record)
	if err != nil {
		t.Fatal(err)
	}
	if e.Identifiers == nil || len(*e.Identifiers) != 1 || (*e.Identifiers)[0].Value != "9780141439600" {
		t.Errorf("identifiers = %v, want only the other edition's ISBN the record names", e.Identifiers)
	}
}
