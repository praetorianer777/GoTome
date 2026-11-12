package similar

import (
	"context"

	"github.com/google/uuid"

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
	// MetadataLastHour is how many metadata vectors were made in the last
	// hour, the pace of a pass.
	MetadataLastHour int `json:"metadataLastHour"`
	// Passages is how far the passages are embedded (#174); left out
	// while they are not.
	Passages *PassageStatus `json:"passages,omitempty"`
}

// PassageStatus is how far the books a scope sees have a vector per
// passage of their text.
type PassageStatus struct {
	// WithText is how many books have text, WithPassages how many of them
	// have their passages' vectors of the chosen model.
	WithText     int `json:"withText"`
	WithPassages int `json:"withPassages"`
	// Bytes is what the passages' vectors take on disk, of every library.
	Bytes int64 `json:"bytes"`
}

// Status counts the books the scope sees, of one library when given, and
// their vectors of the chosen model.
func (s *Service) Status(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (Status, error) {
	spec, enabled, err := s.config(ctx)
	if err != nil {
		return Status{}, err
	}
	row, err := sqlc.New(s.pool).CountEmbedded(ctx, sqlc.CountEmbeddedParams{
		Model: spec.Model.Name, ModelVersion: Version(spec.Model), Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID,
	})
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Model: spec.Model.Name, Enabled: enabled,
		Books: int(row.Books), WithText: int(row.WithText), Metadata: int(row.Metadata), Content: int(row.Content),
		MetadataLastHour: int(row.MetadataLastHour),
	}
	if s.PassageStatus != nil {
		if status.Passages, err = s.PassageStatus(ctx, scope, libraryID); err != nil {
			return Status{}, err
		}
	}
	return status, nil
}
