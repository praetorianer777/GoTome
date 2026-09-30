// Package procexec runs the programs GOtome hands untrusted files to, such as
// poppler's, so that a file which makes one of them hang, run wild or crash
// costs that one file and nothing else.
package procexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Limits bound one run.
type Limits struct {
	// Timeout is how long the run may take in all.
	Timeout time.Duration
	// MaxOutput is how much the program may write to its standard output.
	MaxOutput int64
	// MaxMemory is the address space the program may take, in bytes.
	MaxMemory int64
	// MaxCPU is how much processor time the program may use.
	MaxCPU time.Duration
}

var (
	// ErrTimeout is returned when the program did not finish in time.
	ErrTimeout = errors.New("the program took too long")
	// ErrOutputTooLarge is returned when the program wrote more than allowed.
	ErrOutputTooLarge = errors.New("the program wrote too much")
)

// ExitError is a program that failed: it exited with an error or was killed.
type ExitError struct {
	Program string
	// Code is the exit status, or -1 when a signal ended the program.
	Code   int
	Signal string
	// Stderr is the start of what the program said about it.
	Stderr string
}

func (e *ExitError) Error() string {
	how := "exited with " + strconv.Itoa(e.Code)
	if e.Signal != "" {
		how = "was killed by " + e.Signal
	}
	if e.Stderr == "" {
		return e.Program + " " + how
	}
	return e.Program + " " + how + ": " + e.Stderr
}

const maxStderr = 4 << 10

// prlimit sets the limits of the process it starts and then becomes the
// program. It is part of util-linux, which every Debian system has; where it
// is missing, programs run with the timeout and output cap alone.
var prlimit, _ = exec.LookPath("prlimit")

// Run runs the program and returns what it wrote to standard output. The
// program gets no input, a minimal environment, and a process group of its
// own, which is killed as a whole when time is up.
func Run(ctx context.Context, limits Limits, program string, args ...string) ([]byte, error) {
	name := program
	if prlimit != "" {
		limited := []string{"--core=0"}
		if limits.MaxMemory > 0 {
			limited = append(limited, "--as="+strconv.FormatInt(limits.MaxMemory, 10))
		}
		if limits.MaxCPU > 0 {
			limited = append(limited, "--cpu="+strconv.Itoa(max(1, int(limits.MaxCPU.Seconds()))))
		}
		args = append(append(limited, "--", program), args...)
		name = prlimit
	}

	runCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "HOME=/nonexistent"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The whole group, so that a child the program started goes with it.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	stdout := &capped{max: limits.MaxOutput}
	stderr := &capped{max: maxStderr, quiet: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case runCtx.Err() != nil:
		return nil, fmt.Errorf("%s: %w after %s", program, ErrTimeout, limits.Timeout)
	case stdout.over:
		return nil, fmt.Errorf("%s: %w: more than %d bytes", program, ErrOutputTooLarge, limits.MaxOutput)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		e := &ExitError{Program: program, Code: exitErr.ExitCode(), Stderr: strings.TrimSpace(stderr.buf.String())}
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			e.Signal = status.Signal().String()
		}
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", program, err)
	}
	return stdout.buf.Bytes(), nil
}

// capped keeps what is written up to max bytes. Past that it refuses, which
// closes the pipe and ends a program that goes on writing; quiet ones instead
// keep taking and drop the rest, for output that only explains a failure.
type capped struct {
	buf   bytes.Buffer
	max   int64
	over  bool
	quiet bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.max - int64(c.buf.Len())
	if int64(len(p)) <= room {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:max(room, 0)])
	if c.quiet {
		return len(p), nil
	}
	c.over = true
	return 0, ErrOutputTooLarge
}
