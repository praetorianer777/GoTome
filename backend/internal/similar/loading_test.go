package similar

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/embed"
)

func TestAModelStillOpeningIsAnsweredAsLoadingAndThenUsed(t *testing.T) {
	loadWait = 20 * time.Millisecond
	t.Cleanup(func() { loadWait = 5 * time.Second })
	release := make(chan struct{})
	opened := 0
	s := NewService(nil, nil, func(_ context.Context, spec embed.Spec) (embed.Embedder, error) {
		opened++
		<-release
		return &embed.Fake{Dim: spec.Model.Dim}, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := embed.E5Small

	if _, err := s.EmbedQueries(context.Background(), spec, []string{"a"}); !errors.Is(err, ErrLoading) {
		t.Fatalf("while opening: %v", err)
	}
	if _, err := s.EmbedQueries(context.Background(), spec, []string{"a"}); !errors.Is(err, ErrLoading) {
		t.Fatalf("still opening: %v", err)
	}
	close(release)
	var vecs [][]float32
	var err error
	for range 100 {
		if vecs, err = s.EmbedQueries(context.Background(), spec, []string{"a"}); !errors.Is(err, ErrLoading) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(vecs) != 1 || len(vecs[0]) != spec.Model.Dim {
		t.Fatalf("after opening: %v %d", err, len(vecs))
	}
	if opened != 1 {
		t.Errorf("opened %d times", opened)
	}
}

func TestAModelThatFailsToOpenIsReportedOnceAndAskedAgain(t *testing.T) {
	broken := errors.New("broken")
	s := NewService(nil, nil, func(context.Context, embed.Spec) (embed.Embedder, error) { return nil, broken }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := s.EmbedQueries(context.Background(), embed.E5Small, []string{"a"}); !errors.Is(err, broken) {
		t.Fatalf("got %v", err)
	}
}
