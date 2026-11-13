package jobs

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestQuietLoggerPassesOnlyWarningsAndErrors(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(quiet{slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})}).With("component", "river")

	log.Debug("debug line")
	log.Info("producer job counts")
	log.Warn("a job is stuck")
	log.Error("a job failed")

	got := out.String()
	if strings.Contains(got, "debug line") || strings.Contains(got, "producer job counts") {
		t.Errorf("chatter got through:\n%s", got)
	}
	if !strings.Contains(got, "a job is stuck") || !strings.Contains(got, "a job failed") || !strings.Contains(got, "component=river") {
		t.Errorf("warnings, errors or their attributes are missing:\n%s", got)
	}
}

func TestInsertOpts(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	plain := InsertOpts{Queue: QueueExtract, ScheduledAt: at, MaxAttempts: 3}.river()
	if plain.Queue != QueueExtract || !plain.ScheduledAt.Equal(at) || plain.MaxAttempts != 3 || plain.UniqueOpts.ByArgs {
		t.Errorf("plain options: %+v", plain)
	}

	unique := InsertOpts{Unique: true}.river()
	if !unique.UniqueOpts.ByArgs {
		t.Fatal("a unique insert does not compare arguments")
	}
	// A job that is done must not block asking for the same work again.
	for _, state := range unique.UniqueOpts.ByState {
		if state == rivertype.JobStateCompleted || state == rivertype.JobStateDiscarded || state == rivertype.JobStateCancelled {
			t.Errorf("uniqueness counts %s jobs", state)
		}
	}
}

func TestSchemaVersionIsKnownWithoutADatabase(t *testing.T) {
	if got := SchemaVersion(); got < 1 {
		t.Errorf("SchemaVersion() = %d", got)
	}
}

func TestResumeReturnsTheErrorElseSnoozesWhileWorkIsLeft(t *testing.T) {
	failed := errors.New("disk gone")
	cases := []struct {
		name   string
		more   bool
		err    error
		snooze bool
		want   error
	}{
		{"done", false, nil, false, nil},
		{"more to do", true, nil, true, nil},
		{"an error", false, failed, false, failed},
		{"an error beats more", true, failed, false, failed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Resume(c.more, c.err)
			var snooze *river.JobSnoozeError
			if errors.As(got, &snooze) != c.snooze {
				t.Fatalf("%v, want a snooze: %v", got, c.snooze)
			}
			if c.snooze && snooze.Duration != time.Second {
				t.Errorf("snoozed %s", snooze.Duration)
			}
			if !c.snooze && !errors.Is(got, c.want) {
				t.Errorf("%v, want %v", got, c.want)
			}
		})
	}
}
