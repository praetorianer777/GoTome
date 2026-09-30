package procexec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var generous = Limits{Timeout: 10 * time.Second, MaxOutput: 1 << 20, MaxMemory: 1 << 30, MaxCPU: 10 * time.Second}

func TestRunReturnsOutput(t *testing.T) {
	out, err := Run(context.Background(), generous, "echo", "hello")
	if err != nil || string(out) != "hello\n" {
		t.Fatalf("Run = %q, %v", out, err)
	}
}

func TestRunEndsAProgramThatHangs(t *testing.T) {
	limits := generous
	limits.Timeout = 300 * time.Millisecond
	started := time.Now()
	// The shell starts a child of its own, which must end with it.
	_, err := Run(context.Background(), limits, "sh", "-c", "sleep 30 & sleep 30")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("Run returned after %s", took)
	}
}

func TestRunStopsAProgramThatWritesWithoutEnd(t *testing.T) {
	limits := generous
	limits.MaxOutput = 1000
	_, err := Run(context.Background(), limits, "yes")
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("err = %v, want ErrOutputTooLarge", err)
	}
}

func TestRunReportsAFailureWithWhatTheProgramSaid(t *testing.T) {
	_, err := Run(context.Background(), generous, "sh", "-c", "echo 'Syntax Error: bad xref' >&2; exit 3")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 || exit.Stderr != "Syntax Error: bad xref" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "bad xref") {
		t.Errorf("the message %q leaves out why", err)
	}
}

func TestRunReportsAProgramThatCrashed(t *testing.T) {
	_, err := Run(context.Background(), generous, "sh", "-c", "kill -SEGV $$")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Signal == "" {
		t.Fatalf("err = %#v, want a program killed by a signal", err)
	}
}

func TestRunPutsLimitsOnTheProgram(t *testing.T) {
	if prlimit == "" {
		t.Skip("no prlimit here")
	}
	limits := generous
	limits.MaxMemory = 64 << 20
	limits.MaxCPU = 7 * time.Second
	out, err := Run(context.Background(), limits, "cat", "/proc/self/limits")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Max address space         67108864             67108864",
		"Max cpu time              7                    7",
		"Max core file size        0                    0",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the program's limits lack %q:\n%s", want, out)
		}
	}
}

func TestRunGivesUpWhenAsked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err := Run(ctx, generous, "sleep", "30")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's", err)
	}
}
