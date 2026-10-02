package catalog

import (
	"context"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Timeline is a book's audio as one recording: its files one after the
// other, and the chapters across them.
type Timeline struct {
	Parts    []TimelinePart
	Chapters []TimelineChapter
	// DurationMS is the whole length, the parts' together.
	DurationMS int64
	// Complete is false while a part's length is not known yet, because
	// the part has not been read; such parts are left out.
	Complete bool
}

// TimelinePart is one file of the timeline and where it starts in it.
type TimelinePart struct {
	FileID     uuid.UUID
	Format     string
	Name       string
	StartMS    int64
	DurationMS int64
}

// TimelineChapter is a chapter, its times on the whole timeline.
type TimelineChapter struct {
	Title   string
	FileID  uuid.UUID
	StartMS int64
	EndMS   int64
}

// AudioTimeline returns the audio of a book the scope may see, or
// ErrNotFound. A book in several parts plays them in their order; one that
// has whole audio files and no parts plays the first, as the others are the
// same book again. A part that marks no chapters is one chapter, named
// after its file.
func (s *Service) AudioTimeline(ctx context.Context, scope library.Scope, bookID uuid.UUID) (Timeline, error) {
	if _, err := s.Get(ctx, scope, bookID); err != nil {
		return Timeline{}, err
	}
	q := sqlc.New(s.pool)
	files, err := q.ListAudioFiles(ctx, bookID)
	if err != nil {
		return Timeline{}, err
	}
	var parts []sqlc.ListAudioFilesRow
	for _, f := range files {
		if f.PartIndex != nil {
			parts = append(parts, f)
		}
	}
	if len(parts) == 0 && len(files) > 0 {
		parts = files[:1]
	}
	out := Timeline{Parts: []TimelinePart{}, Chapters: []TimelineChapter{}, Complete: true}
	var ids []uuid.UUID
	for _, f := range parts {
		if f.DurationMs == nil {
			out.Complete = false
			continue
		}
		name := path.Base(f.RelPath)
		out.Parts = append(out.Parts, TimelinePart{
			FileID: f.ID, Format: f.Format, Name: name, StartMS: out.DurationMS, DurationMS: *f.DurationMs,
		})
		out.DurationMS += *f.DurationMs
		ids = append(ids, f.ID)
	}
	chapters, err := q.ListFileChapters(ctx, ids)
	if err != nil {
		return Timeline{}, err
	}
	byFile := map[uuid.UUID][]sqlc.AudioChapter{}
	for _, c := range chapters {
		byFile[c.FileID] = append(byFile[c.FileID], c)
	}
	for _, p := range out.Parts {
		marked := byFile[p.FileID]
		if len(marked) == 0 {
			out.Chapters = append(out.Chapters, TimelineChapter{
				Title: strings.TrimSuffix(p.Name, path.Ext(p.Name)), FileID: p.FileID,
				StartMS: p.StartMS, EndMS: p.StartMS + p.DurationMS,
			})
			continue
		}
		for _, c := range marked {
			out.Chapters = append(out.Chapters, TimelineChapter{
				Title: c.Title, FileID: p.FileID,
				StartMS: p.StartMS + c.StartMs, EndMS: p.StartMS + min(c.EndMs, p.DurationMS),
			})
		}
	}
	return out, nil
}
