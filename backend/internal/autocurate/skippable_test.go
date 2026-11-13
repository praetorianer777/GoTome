package autocurate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestSkippableIsAnErrorAboutOneBook(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"the book is gone", catalog.ErrNotFound, true},
		{"the book is gone, wrapped", fmt.Errorf("book 3: %w", catalog.ErrNotFound), true},
		{"the edit is invalid", catalog.EditError{"title": "too long"}, true},
		{"the edit is invalid, wrapped", fmt.Errorf("apply: %w", catalog.EditError{"year": "not a year"}), true},
		{"the database failed", errors.New("connection reset"), false},
		{"the job was cancelled", context.Canceled, false},
		{"nothing", nil, false},
	} {
		if got := skippable(c.err); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
