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

	"github.com/praetorianer777/gotome/backend/internal/secret"
)

// DefaultSecretsDir is where gotome init keeps what the containers share and
// nobody should have to type: the database password and the key secrets
// are encrypted with.
const DefaultSecretsDir = "/secrets"

// appUID is the user the app runs as in its image (deploy/Dockerfile), the
// only one that may read the encryption key.
const appUID = 10001

// Files in the secrets directory.
const (
	passwordFile = "db-password"
	keyFile      = "secret-key"
)

// initSecrets writes what is missing from the secrets directory and returns
// the names it wrote. Nothing that is there is changed: the database keeps
// the password it was created with, and secrets sealed under a key do not
// open under another. POSTGRES_PASSWORD is used in place of a random
// password when it is set, for an installation that chose its own before
// this existed.
func initSecrets(dir, chosen string) ([]string, error) {
	password := strings.TrimSpace(chosen)
	if password == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		password = base64.RawURLEncoding.EncodeToString(b)
	}
	key, err := secret.NewKey()
	if err != nil {
		return nil, err
	}
	var created []string
	for _, f := range []struct {
		name, content string
		mode          fs.FileMode
		owner         int
	}{
		// Read by the database's user and the app's, which are different
		// users in different containers; only those two mount the volume.
		{passwordFile, password, 0o644, -1},
		// Read by the app alone: the database has no business with it.
		{keyFile, key, 0o400, appUID},
	} {
		wrote, err := writeOnce(dir, f.name, f.content+"\n", f.mode, f.owner)
		if err != nil {
			return created, fmt.Errorf("%s: %w", f.name, err)
		}
		if wrote {
			created = append(created, f.name)
		}
	}
	return created, nil
}

// writeOnce writes the file unless it exists. It is written whole under
// another name and renamed into place, so that a container that starts
// while this one is cut off never reads half of it. The owner is set only
// when running as root, which init in the compose file does; -1 keeps it.
func writeOnce(dir, name, content string, mode fs.FileMode, owner int) (bool, error) {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if owner >= 0 && os.Geteuid() == 0 {
		if err := os.Chown(tmp.Name(), owner, owner); err != nil {
			return false, err
		}
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
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
	if len(created) == 0 {
		fmt.Println("init: the secrets are already there; nothing to do")
	}
	for _, name := range created {
		fmt.Println("init: wrote", filepath.Join(dir, name))
	}
	return nil
}
