package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSecretsDir is where gotome init keeps what the containers share and
// nobody should have to type: the database password.
const DefaultSecretsDir = "/secrets"

// initSecrets writes the database password into the secrets directory, the
// first time only: the database keeps the password it was created with, so
// changing the file later would lock the app out. POSTGRES_PASSWORD is used
// in place of a random one when it is set, for an installation that chose
// its own before this existed.
func initSecrets(dir, chosen string) (created bool, err error) {
	path := filepath.Join(dir, "db-password")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	password := strings.TrimSpace(chosen)
	if password == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return false, err
		}
		password = base64.RawURLEncoding.EncodeToString(b)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	// Written whole under another name, so that a container that starts
	// while this one is cut off never reads half a password.
	tmp, err := os.CreateTemp(dir, ".db-password-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(password + "\n"); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	// Readable by the database's user and the app's, which are different
	// users in different containers; only those two mount the volume.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

func runInit() error {
	dir := os.Getenv("GOTOME_SECRETS_DIR")
	if dir == "" {
		dir = DefaultSecretsDir
	}
	created, err := initSecrets(dir, os.Getenv("POSTGRES_PASSWORD"))
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if created {
		fmt.Println("init: wrote the database password to", filepath.Join(dir, "db-password"))
	} else {
		fmt.Println("init: the database password is already there; nothing to do")
	}
	return nil
}
