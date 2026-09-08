package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"PORT", "LOG_LEVEL", "ENV", "DB_HOST", "DB_PORT", "DB_USER",
		"DB_PASSWORD", "DB_PASSWORD_FILE", "DB_NAME", "DB_SSLMODE",
		"CORS_ALLOWED_ORIGINS", "PURGE_AFTER_DAYS",
		"AUTHENTIK_ISSUER", "AUTHENTIK_ISSUER_URL", "AUTHENTIK_CLIENT_ID",
		"AUTHENTIK_CLIENT_ID_FILE", "AUTHENTIK_CLIENT_SECRET",
		"AUTHENTIK_CLIENT_SECRET_FILE", "AUTHENTIK_REDIRECT_URL",
		"AUTHENTIK_REDIRECT_URI", "OIDC_ISSUER_URL", "OIDC_CLIENT_ID",
		"OIDC_CLIENT_ID_FILE", "OIDC_CLIENT_SECRET", "OIDC_CLIENT_SECRET_FILE",
		"OIDC_REDIRECT_URI", "SESSION_SECRET", "SESSION_SECRET_FILE",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.DBName != "postgres" {
		t.Errorf("DBName = %q, want postgres", cfg.DBName)
	}
	if cfg.PurgeAfterDays != 30 {
		t.Errorf("PurgeAfterDays = %d, want 30", cfg.PurgeAfterDays)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Errorf("CORSAllowedOrigins = %v, want 2 defaults", cfg.CORSAllowedOrigins)
	}
}

func TestLoadFailsWithoutRequired(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_PASSWORD", "secret")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing DB_HOST/DB_USER")
	}
}

func TestLoadFailsWithoutPassword(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when no password provided")
	}
}

func TestPasswordFileTakesPrecedence(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pw")
	if err := os.WriteFile(path, []byte("filesecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "envsecret")
	t.Setenv("DB_PASSWORD_FILE", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPassword != "filesecret" {
		t.Errorf("DBPassword = %q, want filesecret", cfg.DBPassword)
	}
}

func TestInvalidPurgeAfterDays(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("PURGE_AFTER_DAYS", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for negative PURGE_AFTER_DAYS")
	}
}

func TestDatabaseURL(t *testing.T) {
	cfg := Config{
		DBUser: "u", DBPassword: "p", DBHost: "h",
		DBPort: "5432", DBName: "postgres", DBSSLMode: "disable",
	}
	want := "postgres" + "://u:p@h:5432/postgres?sslmode=disable"
	if got := cfg.DatabaseURL(); got != want {
		t.Errorf("DatabaseURL() = %q, want %q", got, want)
	}
}

func TestIsProduction(t *testing.T) {
	for _, env := range []string{"production", "PROD", "prod"} {
		if !(Config{Env: env}).IsProduction() {
			t.Errorf("Env %q should be production", env)
		}
	}
	if (Config{Env: "development"}).IsProduction() {
		t.Error("development should not be production")
	}
}

func TestLoadIgnoresAuthentikSecretFilesWithoutIssuer(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "secret")
	// Docker Compose always passes these, backed by /dev/null when SSO is unused.
	t.Setenv("AUTHENTIK_CLIENT_ID_FILE", "/dev/null")
	t.Setenv("AUTHENTIK_CLIENT_SECRET_FILE", "/dev/null")
	t.Setenv("SESSION_SECRET_FILE", "/dev/null")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthentikIssuer != "" || cfg.AuthentikClientID != "" || cfg.AuthentikSecret != "" {
		t.Errorf("expected Authentik to be disabled, got %#v", cfg)
	}
}

func TestLoadFailsWithInlineAuthentikSecretOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("AUTHENTIK_CLIENT_SECRET", "inline-secret")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when only an inline client secret is set")
	}
}

func TestLoadAuthentikConfigurationUsesSecretFiles(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	clientIDPath := filepath.Join(dir, "client-id")
	clientSecretPath := filepath.Join(dir, "client-secret")
	sessionPath := filepath.Join(dir, "session")
	for path, value := range map[string]string{
		clientIDPath: "client-id\n", clientSecretPath: "client-secret\n",
		sessionPath: "01234567890123456789012345678901\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("AUTHENTIK_ISSUER", "https://auth.example/")
	t.Setenv("AUTHENTIK_CLIENT_ID_FILE", clientIDPath)
	t.Setenv("AUTHENTIK_CLIENT_SECRET_FILE", clientSecretPath)
	t.Setenv("AUTHENTIK_REDIRECT_URL", "https://contacts.example/auth/callback")
	t.Setenv("SESSION_SECRET_FILE", sessionPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthentikClientID != "client-id" || cfg.AuthentikSecret != "client-secret" {
		t.Fatalf("unexpected Authentik credentials: %#v", cfg)
	}
}
