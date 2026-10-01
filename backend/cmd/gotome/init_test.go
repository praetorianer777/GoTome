package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/secret"
)

func TestInitWritesARandomPasswordOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	created, err := initSecrets(dir, "")
	if err != nil || !slices.Equal(created, []string{"db-password", "secret-key"}) {
		t.Fatalf("first init: created %v, %v", created, err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	if len(strings.TrimSpace(string(first))) < 40 {
		t.Errorf("the password %q is short", first)
	}

	created, err = initSecrets(dir, "something else")
	second, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	if err != nil || len(created) > 0 || string(second) != string(first) {
		t.Errorf("second init changed the password (created %v, %v)", created, err)
	}

	other := filepath.Join(t.TempDir(), "secrets")
	initSecrets(other, "")
	third, _ := os.ReadFile(filepath.Join(other, "db-password"))
	if string(third) == string(first) {
		t.Error("two installations got the same password")
	}
}

func TestInitTakesAPasswordThatWasChosen(t *testing.T) {
	dir := t.TempDir()
	if _, err := initSecrets(dir, " mine \n"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	if string(got) != "mine\n" {
		t.Errorf("password = %q", got)
	}
}

func TestInitWritesAKeyOnlyTheAppReads(t *testing.T) {
	dir := t.TempDir()
	if _, err := initSecrets(dir, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secret-key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Errorf("the key's mode is %v, want 0400", info.Mode().Perm())
	}
	first, _ := os.ReadFile(path)
	if _, err := secret.ParseKey(string(first)); err != nil {
		t.Errorf("the key %q does not parse: %v", first, err)
	}

	// An installation from before the key existed gets one, and keeps its
	// password.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	password, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	created, err := initSecrets(dir, "")
	if err != nil || !slices.Equal(created, []string{"secret-key"}) {
		t.Errorf("an upgrade created %v, %v", created, err)
	}
	if again, _ := os.ReadFile(filepath.Join(dir, "db-password")); string(again) != string(password) {
		t.Error("the upgrade changed the password")
	}
	if second, _ := os.ReadFile(path); string(second) == string(first) {
		t.Error("two keys came out the same")
	}
}
