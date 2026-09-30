// Package config loads and validates process configuration from the
// environment. Every setting is a GOTOME_* variable; there is no file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Environments a deployment may declare itself as.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Defaults, named so the tests and the documentation can quote them.
const (
	DefaultEnv      = EnvProduction
	DefaultHTTPAddr = ":8080"
	DefaultLogLevel = "info"
	// DefaultDataDir is where the image keeps what GOtome itself stores:
	// managed libraries, covers. The compose file mounts a volume there.
	DefaultDataDir = "/data"
	// DefaultScanInterval is how often every library's folder is looked
	// through for changes.
	DefaultScanInterval = 6 * time.Hour
)

// Config is the fully resolved configuration of the process.
type Config struct {
	Env      string
	HTTPAddr string
	LogLevel slog.Level
	// DatabaseURL is the Postgres connection string. Commands that never
	// touch the database run without it; see RequireDatabase.
	DatabaseURL string
	// DataDir is the directory GOtome keeps its own files in.
	DataDir string
	// ScanInterval is how often the libraries are scanned on their own; zero
	// leaves scanning to whoever asks for it.
	ScanInterval time.Duration
}

// RequireDatabase is the error a command that needs the database starts with
// when none is configured.
func (c Config) RequireDatabase() error {
	if c.DatabaseURL == "" {
		return errors.New("GOTOME_DATABASE_URL is not set, want a Postgres URL such as postgres://gotome:secret@db:5432/gotome")
	}
	return nil
}

// IsProduction reports whether the deployment declared itself as production.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads the configuration from the process environment.
func Load() (Config, error) { return load(os.Getenv) }

func load(getenv func(string) string) (Config, error) {
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		Env:         get("GOTOME_ENV", DefaultEnv),
		HTTPAddr:    get("GOTOME_HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL: get("GOTOME_DATABASE_URL", ""),
		DataDir:     get("GOTOME_DATA_DIR", DefaultDataDir),
	}
	if cfg.Env != EnvDevelopment && cfg.Env != EnvProduction {
		return Config{}, fmt.Errorf("GOTOME_ENV is %q, want %s or %s", cfg.Env, EnvDevelopment, EnvProduction)
	}
	if _, _, err := net.SplitHostPort(cfg.HTTPAddr); err != nil {
		return Config{}, fmt.Errorf("GOTOME_HTTP_ADDR is %q, want host:port such as %s: %w", cfg.HTTPAddr, DefaultHTTPAddr, err)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		return Config{}, fmt.Errorf("GOTOME_DATA_DIR is %q, want a full path such as %s", cfg.DataDir, DefaultDataDir)
	}
	interval := get("GOTOME_SCAN_INTERVAL", DefaultScanInterval.String())
	var err error
	if cfg.ScanInterval, err = time.ParseDuration(interval); err != nil || (cfg.ScanInterval != 0 && cfg.ScanInterval < time.Minute) {
		return Config{}, fmt.Errorf("GOTOME_SCAN_INTERVAL is %q, want a duration of a minute or more such as 6h, or 0 to switch scheduled scans off", interval)
	}
	level := get("GOTOME_LOG_LEVEL", DefaultLogLevel)
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("GOTOME_LOG_LEVEL is %q, want debug, info, warn or error", level)
	}
	return cfg, nil
}
