//go:build !linux

package main

import (
	"io"
	"os"
)

// readHidden knows no terminal outside Linux, where the image runs: the
// password is read from stdin as from a pipe.
func readHidden(*os.File, io.Writer, string) (string, bool, error) {
	return "", false, nil
}
