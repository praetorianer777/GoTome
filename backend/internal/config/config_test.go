package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Config{Env: EnvProduction, HTTPAddr: ":8080", LogLevel: slog.LevelInfo, DataDir: "/data", ScanInterval: 6 * time.Hour, UploadLimit: 4 << 30}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
	if !cfg.IsProduction() {
		t.Error("the default environment is not production")
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"GOTOME_ENV":       "development",
		"GOTOME_HTTP_ADDR": " 127.0.0.1:9000 ",
		"GOTOME_LOG_LEVEL": "DEBUG",
		"GOTOME_DATA_DIR":  "/srv/gotome",
		// Zero switches scheduled scans off.
		"GOTOME_SCAN_INTERVAL":   "0",
		"GOTOME_UPLOAD_LIMIT_MB": "100",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Config{Env: EnvDevelopment, HTTPAddr: "127.0.0.1:9000", LogLevel: slog.LevelDebug, DataDir: "/srv/gotome", UploadLimit: 100 << 20}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct{ key, value string }{
		{"GOTOME_ENV", "staging"},
		{"GOTOME_HTTP_ADDR", "8080"},
		{"GOTOME_LOG_LEVEL", "loud"},
		{"GOTOME_DATA_DIR", "data"},
		{"GOTOME_SCAN_INTERVAL", "often"},
		{"GOTOME_SCAN_INTERVAL", "5s"},
		{"GOTOME_SCAN_INTERVAL", "-6h"},
		{"GOTOME_UPLOAD_LIMIT_MB", "0"},
		{"GOTOME_UPLOAD_LIMIT_MB", "2GB"},
	}
	for _, c := range cases {
		_, err := load(env(map[string]string{c.key: c.value}))
		if err == nil {
			t.Errorf("%s=%q was accepted", c.key, c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.key) {
			t.Errorf("error for %s does not name the variable: %v", c.key, err)
		}
	}
}

func TestDatabaseURL(t *testing.T) {
	without, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := without.RequireDatabase(); err == nil || !strings.Contains(err.Error(), "GOTOME_DATABASE_URL") {
		t.Errorf("without a URL: err = %v, want one naming the variable", err)
	}

	with, err := load(env(map[string]string{"GOTOME_DATABASE_URL": "postgres://u:p@db:5432/gotome"}))
	if err != nil {
		t.Fatal(err)
	}
	if with.DatabaseURL != "postgres://u:p@db:5432/gotome" || with.RequireDatabase() != nil {
		t.Errorf("with a URL: %+v, %v", with, with.RequireDatabase())
	}
}

func TestDatabasePasswordFromAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "db-password")
	if err := os.WriteFile(file, []byte("s3cr/t+?&\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := load(env(map[string]string{
		"GOTOME_DATABASE_URL":           "postgres://gotome@db:5432/gotome?sslmode=disable",
		"GOTOME_DATABASE_PASSWORD_FILE": file,
	}))
	if err != nil {
		t.Fatal(err)
	}
	// Characters that mean something in a URL are escaped.
	if want := "postgres://gotome:s3cr%2Ft+%3F&@db:5432/gotome?sslmode=disable"; cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want)
	}

	for why, vars := range map[string]map[string]string{
		"no such file":  {"GOTOME_DATABASE_URL": "postgres://gotome@db/gotome", "GOTOME_DATABASE_PASSWORD_FILE": file + ".missing"},
		"no user name":  {"GOTOME_DATABASE_URL": "postgres://db/gotome", "GOTOME_DATABASE_PASSWORD_FILE": file},
		"no URL at all": {"GOTOME_DATABASE_PASSWORD_FILE": file},
	} {
		if _, err := load(env(vars)); err == nil || !strings.Contains(err.Error(), "GOTOME_DATABASE_PASSWORD_FILE") {
			t.Errorf("%s: err = %v", why, err)
		}
	}
}
