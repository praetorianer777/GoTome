package ingest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSkipCountsAFileItCouldNotReadAndGoesOn(t *testing.T) {
	var out bytes.Buffer
	s := &Scanner{log: slog.New(slog.NewTextHandler(&out, nil))}
	var res Result
	if err := s.skip(context.Background(), "Austen/Emma.epub", errors.New("bad zip"), &res); err != nil {
		t.Fatalf("the scan stopped: %v", err)
	}
	if err := s.skip(context.Background(), "Austen/Persuasion.epub", errors.New("bad zip"), &res); err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 2 {
		t.Errorf("%d skipped", res.Skipped)
	}
	if log := out.String(); strings.Count(log, "scan: cannot read") != 2 || !strings.Contains(log, "Austen/Emma.epub") || !strings.Contains(log, "bad zip") {
		t.Errorf("log: %s", log)
	}
}

func TestSkipStopsTheScanWhenItWasToldToStop(t *testing.T) {
	var out bytes.Buffer
	s := &Scanner{log: slog.New(slog.NewTextHandler(&out, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var res Result
	if err := s.skip(ctx, "a.epub", context.Canceled, &res); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v, want the context's error", err)
	}
	if res.Skipped != 0 || out.Len() != 0 {
		t.Errorf("a stopped scan counted a file: %d %q", res.Skipped, out.String())
	}
}
