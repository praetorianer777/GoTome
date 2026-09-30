package config

import (
	"log/slog"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Config{Env: EnvProduction, HTTPAddr: ":8080", LogLevel: slog.LevelInfo}
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
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Config{Env: EnvDevelopment, HTTPAddr: "127.0.0.1:9000", LogLevel: slog.LevelDebug}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct{ key, value string }{
		{"GOTOME_ENV", "staging"},
		{"GOTOME_HTTP_ADDR", "8080"},
		{"GOTOME_LOG_LEVEL", "loud"},
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
