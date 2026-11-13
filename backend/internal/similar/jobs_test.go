package similar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/embed"
)

func TestPassResultSaysWhatTheJobDoes(t *testing.T) {
	broken := errors.New("broken")
	cases := []struct {
		name     string
		complete bool
		err      error
		snooze   bool
		want     error
		logged   bool
	}{
		{"finished", true, nil, false, nil, false},
		{"not finished", false, nil, true, nil, false},
		{"no model that runs", false, fmt.Errorf("%w: no runtime", ErrUnavailable), false, nil, true},
		{"no model that runs, though complete", true, ErrUnavailable, false, nil, true},
		{"another error", true, broken, false, broken, false},
		{"another error, not finished", false, broken, false, broken, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			got := PassResult(slog.New(slog.NewTextHandler(&out, nil)), "books are not embedded", c.complete, c.err)
			var snooze *river.JobSnoozeError
			if errors.As(got, &snooze) != c.snooze {
				t.Fatalf("%v, want a snooze: %v", got, c.snooze)
			}
			if !c.snooze && !errors.Is(got, c.want) {
				t.Errorf("%v, want %v", got, c.want)
			}
			if c.snooze && snooze.Duration != time.Second {
				t.Errorf("snoozed %s", snooze.Duration)
			}
			if says := strings.Contains(out.String(), "level=WARN msg=\"books are not embedded\""); says != c.logged {
				t.Errorf("logged %v, want %v: %s", says, c.logged, out.String())
			}
		})
	}
}

func TestTheEmbedWorkerEndsQuietlyWhenEmbeddingIsOffOrCannotRun(t *testing.T) {
	broken := errors.New("settings unreadable")
	run := func(config Config) (error, string) {
		var out bytes.Buffer
		s := NewService(nil, config, nil, slog.New(slog.NewTextHandler(&out, nil)))
		return (&EmbedWorker{Service: s}).Work(context.Background(), nil), out.String()
	}
	off := func(context.Context) (embed.Spec, bool, error) { return embed.E5Small, false, nil }
	if err, log := run(off); err != nil || log != "" {
		t.Errorf("off: %v %q", err, log)
	}
	unavailable := func(context.Context) (embed.Spec, bool, error) {
		return embed.Spec{}, false, fmt.Errorf("%w: down", ErrUnavailable)
	}
	if err, log := run(unavailable); err != nil || !strings.Contains(log, "books are not embedded") {
		t.Errorf("unavailable: %v %q", err, log)
	}
	bad := func(context.Context) (embed.Spec, bool, error) { return embed.Spec{}, false, broken }
	if err, _ := run(bad); !errors.Is(err, broken) {
		t.Errorf("a failing config: %v", err)
	}
}

func TestWarmOpensTheModelOnlyWhereItRunsHereAndOnlyOnce(t *testing.T) {
	spec := embed.E5Small
	config := func(context.Context) (embed.Spec, bool, error) { return spec, true, nil }
	type setup struct {
		name   string
		config Config
		open   bool
		probe  Probe
		opens  int
	}
	ready := func(state string) Probe {
		return func(context.Context, embed.Spec) Readiness { return Readiness{State: state} }
	}
	cases := []setup{
		{name: "runs here", config: config, open: true, probe: ready(ReadyHere), opens: 1},
		{name: "needs a download", config: config, open: true, probe: ready(ReadyAfterDownload), opens: 0},
		{name: "no probe", config: config, open: true, opens: 0},
		{name: "no opener", config: config, open: false, probe: ready(ReadyHere), opens: 0},
		{name: "config fails", config: func(context.Context) (embed.Spec, bool, error) { return spec, true, errors.New("x") }, open: true, probe: ready(ReadyHere), opens: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opened := make(chan struct{}, 4)
			var open Opener
			if c.open {
				open = func(_ context.Context, sp embed.Spec) (embed.Embedder, error) {
					opened <- struct{}{}
					return &embed.Fake{Dim: sp.Model.Dim}, nil
				}
			}
			s := NewService(nil, c.config, open, slog.New(slog.DiscardHandler))
			s.Probe = c.probe
			s.Warm(context.Background())
			s.Warm(context.Background())
			deadline := time.After(100 * time.Millisecond)
			n := 0
		wait:
			for {
				select {
				case <-opened:
					n++
				case <-deadline:
					break wait
				}
			}
			if n != c.opens {
				t.Errorf("opened %d times, want %d", n, c.opens)
			}
			if c.opens == 1 {
				vecs, err := s.EmbedQueries(context.Background(), spec, []string{"a"})
				if err != nil || len(vecs) != 1 {
					t.Errorf("the warmed model: %v %d", err, len(vecs))
				}
			}
		})
	}
}
