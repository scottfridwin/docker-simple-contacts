// Package config loads and validates runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the server.
type Config struct {
	Port               string
	LogLevel           string
	Env                string
	DBHost             string
	DBPort             string
	DBUser             string
	DBPassword         string
	DBName             string
	DBSSLMode          string
	CORSAllowedOrigins []string
	PurgeAfterDays     int
}

// IsProduction reports whether the server runs in a production environment.
func (c Config) IsProduction() bool {
	return strings.EqualFold(c.Env, "production") || strings.EqualFold(c.Env, "prod")
}

// DatabaseURL builds a PostgreSQL connection string from the configuration.
func (c Config) DatabaseURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode,
	)
}

// Load reads configuration from environment variables, applies defaults, and
// validates required values. It returns an error when the configuration is
// invalid so startup can fail fast.
func Load() (Config, error) {
	cfg := Config{
		Port:           getEnv("PORT", "8080"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		Env:            getEnv("ENV", "production"),
		DBHost:         getEnv("DB_HOST", ""),
		DBPort:         getEnv("DB_PORT", "5432"),
		DBUser:         getEnv("DB_USER", ""),
		DBName:         getEnv("DB_NAME", "postgres"),
		DBSSLMode:      getEnv("DB_SSLMODE", "disable"),
		PurgeAfterDays: 30,
	}

	password, err := resolvePassword()
	if err != nil {
		return Config{}, err
	}
	cfg.DBPassword = password

	cfg.CORSAllowedOrigins = parseOrigins(getEnv(
		"CORS_ALLOWED_ORIGINS",
		"http://localhost:5173,http://127.0.0.1:5173",
	))

	if v := os.Getenv("PURGE_AFTER_DAYS"); v != "" {
		days, convErr := strconv.Atoi(v)
		if convErr != nil || days <= 0 {
			return Config{}, fmt.Errorf("PURGE_AFTER_DAYS must be a positive integer, got %q", v)
		}
		cfg.PurgeAfterDays = days
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var missing []string
	if c.DBHost == "" {
		missing = append(missing, "DB_HOST")
	}
	if c.DBUser == "" {
		missing = append(missing, "DB_USER")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

// resolvePassword implements the secret precedence rule: DB_PASSWORD_FILE takes
// precedence over DB_PASSWORD. If neither is present, startup fails.
func resolvePassword() (string, error) {
	if path := os.Getenv("DB_PASSWORD_FILE"); path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied config
		if err != nil {
			return "", fmt.Errorf("reading DB_PASSWORD_FILE %q: %w", path, err)
		}
		secret := strings.TrimRight(string(data), "\r\n")
		if secret == "" {
			return "", fmt.Errorf("DB_PASSWORD_FILE %q is empty", path)
		}
		return secret, nil
	}
	if pw := os.Getenv("DB_PASSWORD"); pw != "" {
		return pw, nil
	}
	return "", errors.New("database password required: set DB_PASSWORD_FILE or DB_PASSWORD")
}

func parseOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
