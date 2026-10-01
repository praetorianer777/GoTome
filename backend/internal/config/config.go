// Package config loads and validates process configuration from the
// environment. Every setting is a GOTOME_* variable; there is no file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/secret"
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
	// DefaultUploadLimitMB is the largest file one upload may carry, in
	// mebibytes: room for an audiobook in one file.
	DefaultUploadLimitMB = 4096
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
	// UploadLimit is the largest file one upload may carry, in bytes.
	UploadLimit int64
	// Offline asks no metadata provider on the internet.
	Offline bool
	// SecretKey encrypts the secrets kept in the database. Commands that
	// keep none run without it; see RequireSecretKey.
	SecretKey []byte
}

// RequireSecretKey is the error a command that keeps secrets starts with
// when no key is configured.
func (c Config) RequireSecretKey() error {
	if c.SecretKey == nil {
		return errors.New("GOTOME_SECRET_KEY_FILE is not set, want the file gotome init writes, such as /secrets/secret-key")
	}
	return nil
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
	limit := get("GOTOME_UPLOAD_LIMIT_MB", strconv.Itoa(DefaultUploadLimitMB))
	mb, err := strconv.ParseInt(limit, 10, 64)
	if err != nil || mb < 1 || mb > 1<<20 {
		return Config{}, fmt.Errorf("GOTOME_UPLOAD_LIMIT_MB is %q, want a whole number of mebibytes from 1 to 1048576, such as %d", limit, DefaultUploadLimitMB)
	}
	cfg.UploadLimit = mb << 20
	offline := get("GOTOME_OFFLINE", "false")
	if cfg.Offline, err = strconv.ParseBool(offline); err != nil {
		return Config{}, fmt.Errorf("GOTOME_OFFLINE is %q, want true or false", offline)
	}
	level := get("GOTOME_LOG_LEVEL", DefaultLogLevel)
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("GOTOME_LOG_LEVEL is %q, want debug, info, warn or error", level)
	}
	if file := get("GOTOME_SECRET_KEY_FILE", ""); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return Config{}, fmt.Errorf("GOTOME_SECRET_KEY_FILE: %w", err)
		}
		if cfg.SecretKey, err = secret.ParseKey(string(data)); err != nil {
			return Config{}, fmt.Errorf("GOTOME_SECRET_KEY_FILE: %s: %w", file, err)
		}
	}
	if file := get("GOTOME_DATABASE_PASSWORD_FILE", ""); file != "" {
		if cfg.DatabaseURL, err = withPassword(cfg.DatabaseURL, file); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// withPassword puts the password kept in the file into the database URL. The
// compose file keeps it there, written once by gotome init, so that it is in
// no environment variable that docker inspect would show.
func withPassword(databaseURL, file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("GOTOME_DATABASE_PASSWORD_FILE: %w", err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", fmt.Errorf("GOTOME_DATABASE_PASSWORD_FILE: %s is empty", file)
	}
	u, err := url.Parse(databaseURL)
	if err != nil || u.Scheme == "" || u.User == nil {
		return "", errors.New("GOTOME_DATABASE_PASSWORD_FILE needs GOTOME_DATABASE_URL with a user name, such as postgres://gotome@db:5432/gotome")
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}
