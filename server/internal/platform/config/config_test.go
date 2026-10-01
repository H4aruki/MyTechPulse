package config

import (
	"strings"
	"testing"
	"time"
)

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

func validValues() map[string]string {
	return map[string]string{
		"APP_ENV":            "local",
		"DATABASE_URL":       "postgresql://user:pass@127.0.0.1:5432/db",
		"QIITA_ACCESS_TOKEN": "token",
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"APP_ENV": "local"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestLoadRejectsSwaggerInProduction(t *testing.T) {
	values := validValues()
	values["APP_ENV"] = "production"
	values["SWAGGER_ENABLED"] = "true"
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "SWAGGER_ENABLED") {
		t.Fatalf("expected SWAGGER_ENABLED error, got %v", err)
	}
}

func TestLoadDefaultsForLocal(t *testing.T) {
	cfg, err := Load(mapLookup(validValues()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8001" || !cfg.SwaggerEnabled || cfg.CookieName != "mtp_session" || cfg.CookieSecure {
		t.Fatalf("unexpected local defaults: %+v", cfg)
	}
	if cfg.SessionTTL != 24*time.Hour || cfg.ProviderTimeout != 5*time.Second || cfg.FeedTimeout != 8*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
	if cfg.ProviderMaxBytes != 2097152 {
		t.Fatalf("unexpected max bytes: %d", cfg.ProviderMaxBytes)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "http://localhost:5173" {
		t.Fatalf("unexpected origins: %v", cfg.CORSOrigins)
	}
}

func TestLoadProductionDisablesSwaggerAndSecuresCookie(t *testing.T) {
	values := validValues()
	values["APP_ENV"] = "production"
	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SwaggerEnabled || !cfg.CookieSecure || cfg.CookieName != "__Host-mtp_session" {
		t.Fatalf("unexpected production config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValuesWithoutEchoingThem(t *testing.T) {
	cases := map[string]string{
		"APP_ENV":              "staging-secret",
		"SESSION_TTL":          "0s",
		"PROVIDER_TIMEOUT":     "abc-secret",
		"FEED_TIMEOUT":         "-1s",
		"PROVIDER_MAX_BYTES":   "0",
		"SWAGGER_ENABLED":      "maybe-secret",
		"CORS_ALLOWED_ORIGINS": "*",
	}
	for key, bad := range cases {
		values := validValues()
		values[key] = bad
		_, err := Load(mapLookup(values))
		if err == nil {
			t.Fatalf("%s: expected error", key)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: error echoes input: %v", key, err)
		}
	}
}

func TestLoadRejectsBadOrigins(t *testing.T) {
	for _, origin := range []string{
		"*",
		"ftp://example.com",
		"http://",
		"https://example.com/path",
		"https://example.com?x=1",
		"https://example.com#frag",
		"https://user:pw@example.com",
		"example.com",
	} {
		values := validValues()
		values["CORS_ALLOWED_ORIGINS"] = origin
		if _, err := Load(mapLookup(values)); err == nil {
			t.Fatalf("expected error for origin %q", origin)
		}
	}
	values := validValues()
	values["CORS_ALLOWED_ORIGINS"] = "https://a.example.com, http://localhost:5173"
	cfg, err := Load(mapLookup(values))
	if err != nil || len(cfg.CORSOrigins) != 2 {
		t.Fatalf("unexpected result: %v %v", cfg.CORSOrigins, err)
	}
}

func TestLoadTrimsRequiredAndRejectsBlank(t *testing.T) {
	values := validValues()
	values["QIITA_ACCESS_TOKEN"] = "   "
	_, err := Load(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), "QIITA_ACCESS_TOKEN") {
		t.Fatalf("expected QIITA_ACCESS_TOKEN error, got %v", err)
	}
}
