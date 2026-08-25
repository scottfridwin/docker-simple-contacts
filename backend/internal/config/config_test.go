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
	want := "postgres://u:p@h:5432/postgres?sslmode=disable"
	if got := cfg.DatabaseURL(); got != want {
		t.Errorf("DatabaseURL() = %q, want %q", got, want)
	}
}
