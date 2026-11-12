//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// asModel embeds as another embedder does under the stored model's name,
// as bge-m3 on the CPU does under the server's.
type asModel struct {
	embed.Embedder
	model embed.Model
}

func (a asModel) Model() embed.Model { return a.model }

func TestVectorsMadeAgainAreComparedWithTheStoredOnes(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	a.embedAll()
	q := sqlc.New(a.pool)
	ctx := context.Background()

	same := asModel{&embed.Fake{}, embed.E5Small.Model}
	c, err := similar.CompareStored(ctx, q, embed.E5Small, same, 10)
	if err != nil {
		t.Fatal(err)
	}
	if c.Books != 2 || !c.Passed() || c.MinCosine < 0.9999 {
		t.Errorf("the same vectors: %+v", c)
	}

	// Another prefix makes other texts, and so other vectors.
	other := embed.E5Small
	other.Prefix = "passage: "
	if c, err := similar.CompareStored(ctx, q, other, same, 10); err != nil || c.Passed() {
		t.Errorf("other vectors pass: %+v %v", c, err)
	}

	if c, err := similar.CompareStored(ctx, q, embed.BGEM3, asModel{&embed.Fake{Dim: 1024}, embed.BGEM3.Model}, 10); err != nil || c.Books != 0 || c.Passed() {
		t.Errorf("a model without stored vectors: %+v %v", c, err)
	}
}
