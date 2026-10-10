package similar

import (
	"context"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Status is how far the books a scope sees are embedded with the chosen
// model, and whether embedding goes on.
type Status struct {
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
	// Books is how many books there are, WithText how many of them have
	// text, and Metadata and Content how many have that vector of the
	// model.
	Books    int `json:"books"`
	WithText int `json:"withText"`
	Metadata int `json:"metadata"`
	Content  int `json:"content"`
}

// Status counts the books the scope sees and their vectors of the chosen
// model.
func (s *Service) Status(ctx context.Context, scope library.Scope) (Status, error) {
	spec, enabled, err := s.config(ctx)
	if err != nil {
		return Status{}, err
	}
	row, err := sqlc.New(s.pool).CountEmbedded(ctx, sqlc.CountEmbeddedParams{
		Model: spec.Model.Name, ModelVersion: Version(spec.Model), Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return Status{}, err
	}
	return Status{
		Model: spec.Model.Name, Enabled: enabled,
		Books: int(row.Books), WithText: int(row.WithText), Metadata: int(row.Metadata), Content: int(row.Content),
	}, nil
}
