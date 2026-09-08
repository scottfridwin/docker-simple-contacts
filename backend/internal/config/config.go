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
	AuthentikIssuer    string
	AuthentikClientID  string
	AuthentikSecret    string
	AuthentikRedirect  string
	SessionSecret      []byte
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
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

	cfg.AuthentikIssuer = firstEnv("AUTHENTIK_ISSUER", "AUTHENTIK_ISSUER_URL", "OIDC_ISSUER_URL")
	cfg.AuthentikRedirect = firstEnv("AUTHENTIK_REDIRECT_URL", "AUTHENTIK_REDIRECT_URI", "OIDC_REDIRECT_URI")
	clientIDFile := firstEnv("AUTHENTIK_CLIENT_ID_FILE", "OIDC_CLIENT_ID_FILE")
	clientID := firstEnv("AUTHENTIK_CLIENT_ID", "OIDC_CLIENT_ID")
	// Authentik is considered configured when an issuer, client ID, redirect
	// URL, or inline client secret is supplied. Docker Compose always passes
	// secret-file paths (backed by /dev/null when SSO is unused), so *_FILE
	// variables alone must not enable SSO.
	if cfg.AuthentikIssuer != "" || clientID != "" || cfg.AuthentikRedirect != "" || firstEnv("AUTHENTIK_CLIENT_SECRET", "OIDC_CLIENT_SECRET") != "" {
		if cfg.AuthentikIssuer == "" || (clientID == "" && clientIDFile == "") || cfg.AuthentikRedirect == "" {
			return Config{}, errors.New("AUTHENTIK_ISSUER, AUTHENTIK_CLIENT_ID, and AUTHENTIK_REDIRECT_URL are required when Authentik is configured")
		}
		cfg.AuthentikClientID, err = resolveSecretValue(clientIDFile, clientID, "AUTHENTIK_CLIENT_ID")
		if err != nil {
			return Config{}, err
		}
		cfg.AuthentikSecret, err = resolveSecretValue(
			firstEnv("AUTHENTIK_CLIENT_SECRET_FILE", "OIDC_CLIENT_SECRET_FILE"),
			firstEnv("AUTHENTIK_CLIENT_SECRET", "OIDC_CLIENT_SECRET"),
			"AUTHENTIK_CLIENT_SECRET",
		)
		if err != nil {
			return Config{}, err
		}
		secret, secretErr := resolveSecretValue(
			firstEnv("SESSION_SECRET_FILE"),
			firstEnv("SESSION_SECRET"),
			"SESSION_SECRET",
		)
		if secretErr != nil {
			return Config{}, secretErr
		}
		if len(secret) < 32 {
			return Config{}, errors.New("SESSION_SECRET must be at least 32 bytes")
		}
		cfg.SessionSecret = []byte(secret)
	}

	if v := os.Getenv("PURGE_AFTER_DAYS"); v != "" {
		days, convErr := strconv.Atoi(v)
		if convErr != nil || days <= 0 {
			return Config{}, fmt.Errorf("PURGE_AFTER_DAYS must be a positive integer, got %q", v)
		}
		cfg.PurgeAfterDays = days
	}

	googleClientIDFile := firstEnv("GOOGLE_CLIENT_ID_FILE")
	googleClientID := firstEnv("GOOGLE_CLIENT_ID")
	googleClientSecretFile := firstEnv("GOOGLE_CLIENT_SECRET_FILE")
	googleClientSecret := firstEnv("GOOGLE_CLIENT_SECRET")
	cfg.GoogleRedirectURL = firstEnv("GOOGLE_REDIRECT_URL", "GOOGLE_REDIRECT_URI")
	if googleClientID != "" || googleClientSecret != "" || cfg.GoogleRedirectURL != "" {
		if (googleClientID == "" && googleClientIDFile == "") || (googleClientSecret == "" && googleClientSecretFile == "") || cfg.GoogleRedirectURL == "" {
			return Config{}, errors.New("GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and GOOGLE_REDIRECT_URL are required when Google sync is configured")
		}
		cfg.GoogleClientID, err = resolveSecretValue(googleClientIDFile, googleClientID, "GOOGLE_CLIENT_ID")
		if err != nil {
			return Config{}, err
		}
		cfg.GoogleClientSecret, err = resolveSecretValue(googleClientSecretFile, googleClientSecret, "GOOGLE_CLIENT_SECRET")
		if err != nil {
			return Config{}, err
		}
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

func resolveSecretValue(filePath, value, label string) (string, error) {
	if filePath != "" {
		data, err := os.ReadFile(filePath) //nolint:gosec // path is operator-supplied config
		if err != nil {
			return "", fmt.Errorf("reading %s_FILE %q: %w", label, filePath, err)
		}
		secret := strings.TrimRight(string(data), "\r\n")
		if secret == "" {
			return "", fmt.Errorf("%s_FILE %q is empty", label, filePath)
		}
		return secret, nil
	}
	if value != "" {
		return value, nil
	}
	return "", fmt.Errorf("%s or %s_FILE is required", label, label)
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

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
