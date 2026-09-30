package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesARandomPasswordOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	created, err := initSecrets(dir, "")
	if err != nil || !created {
		t.Fatalf("first init: created %v, %v", created, err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	if len(strings.TrimSpace(string(first))) < 40 {
		t.Errorf("the password %q is short", first)
	}

	created, err = initSecrets(dir, "something else")
	second, _ := os.ReadFile(filepath.Join(dir, "db-password"))
	if err != nil || created || string(second) != string(first) {
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
