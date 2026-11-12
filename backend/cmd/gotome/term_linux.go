package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

// readHidden reads a line from a terminal with echo off; ok is false when f
// is not a terminal, and nothing is read then.
func readHidden(f *os.File, prompt io.Writer, question string) (string, bool, error) {
	fd := int(f.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", false, nil
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO
	quiet.Lflag |= unix.ICANON | unix.ISIG
	quiet.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &quiet); err != nil {
		return "", true, err
	}
	// Ctrl-C would otherwise leave the operator's terminal without echo.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-interrupted:
			_ = unix.IoctlSetTermios(fd, unix.TCSETS, old)
			fmt.Fprintln(prompt)
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signal.Stop(interrupted)
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, old)
		fmt.Fprintln(prompt)
	}()

	fmt.Fprint(prompt, question)
	// A byte at a time, so that nothing of the next line is taken with it.
	var line strings.Builder
	b := make([]byte, 1)
	for line.Len() <= auth.MaxPasswordLen {
		n, err := f.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return line.String(), true, nil
			}
			line.WriteByte(b[0])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", true, err
		}
	}
	if line.Len() == 0 {
		return "", true, errors.New("no password given")
	}
	return line.String(), true, nil
}
