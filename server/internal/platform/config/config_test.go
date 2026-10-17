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

// productionValues は本番として有効な最小の設定(CLIENT_IP_HEADERは本番では必須)。
func productionValues() map[string]string {
	values := validValues()
	values["APP_ENV"] = "production"
	values["CLIENT_IP_HEADER"] = "CF-Connecting-IP"
	return values
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"APP_ENV": "local"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestLoadRejectsSwaggerInProduction(t *testing.T) {
	values := productionValues()
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
	values := productionValues()
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

func TestLoadWebAuthnDefaults(t *testing.T) {
	cfg, err := Load(mapLookup(validValues()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebAuthnRPID != "localhost" || cfg.WebAuthnRPName != "MyTechPulse" ||
		len(cfg.WebAuthnOrigins) != 1 || cfg.WebAuthnOrigins[0] != "http://localhost:5173" {
		t.Fatalf("unexpected local defaults: %+v", cfg)
	}
	values := productionValues()
	// docker-composeは未設定の項目を空文字で渡すため、空も未設定と同じ扱いにする
	values["WEBAUTHN_RP_ID"] = ""
	values["WEBAUTHN_ORIGINS"] = " "
	cfg, err = Load(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebAuthnRPID != "mytechpulse.net" || len(cfg.WebAuthnOrigins) != 1 || cfg.WebAuthnOrigins[0] != "https://mytechpulse.net" {
		t.Fatalf("unexpected production defaults: %+v", cfg)
	}
}

func TestLoadWebAuthnAcceptsSubdomainOrigin(t *testing.T) {
	values := productionValues()
	values["WEBAUTHN_RP_ID"] = "Example.com"
	values["WEBAUTHN_ORIGINS"] = "https://example.com, https://app.example.com"
	cfg, err := Load(mapLookup(values))
	if err != nil || cfg.WebAuthnRPID != "example.com" || len(cfg.WebAuthnOrigins) != 2 {
		t.Fatalf("unexpected result: %+v %v", cfg, err)
	}
}

func TestLoadRejectsBadWebAuthn(t *testing.T) {
	cases := []struct {
		name string
		env  string
		id   string
		orig string
	}{
		{"本番でRP IDがlocalhost", "production", "localhost", "https://localhost"},
		{"本番でRP IDがIPアドレス", "production", "127.0.0.1", "https://127.0.0.1"},
		{"本番でhttpのオリジン", "production", "example.com", "http://example.com"},
		{"本番でlocalhostのオリジン", "production", "example.com", "https://example.com, http://localhost:5173"},
		{"ワイルドカード", "production", "example.com", "*"},
		{"RP IDにスキーム", "local", "https://example.com", "https://example.com"},
		{"RP IDにポート", "local", "example.com:443", "https://example.com"},
		{"RP IDにパス", "local", "example.com/x", "https://example.com"},
		{"オリジンがRP IDの配下でない", "local", "example.com", "https://evil.test"},
		{"RP IDを後ろに含むだけの別ドメイン", "local", "example.com", "https://notexample.com"},
		{"親ドメインのオリジン", "local", "app.example.com", "https://example.com"},
		{"パス付きオリジン", "local", "example.com", "https://example.com/app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values := productionValues()
			values["APP_ENV"] = c.env
			values["WEBAUTHN_RP_ID"] = c.id
			values["WEBAUTHN_ORIGINS"] = c.orig
			_, err := Load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), "WEBAUTHN_") {
				t.Fatalf("expected WEBAUTHN error, got %v", err)
			}
			if strings.Contains(err.Error(), c.orig) && c.orig != "*" {
				t.Fatalf("error must not echo input: %v", err)
			}
		})
	}
}

func TestLoadProductionRequiresClientIPHeader(t *testing.T) {
	cases := map[string]func(map[string]string){
		"未設定":  func(v map[string]string) { delete(v, "CLIENT_IP_HEADER") },
		"空":    func(v map[string]string) { v["CLIENT_IP_HEADER"] = "" },
		"空白のみ": func(v map[string]string) { v["CLIENT_IP_HEADER"] = "  \t " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			values := productionValues()
			mutate(values)
			_, err := Load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), "CLIENT_IP_HEADER") {
				t.Fatalf("expected CLIENT_IP_HEADER error, got %v", err)
			}
		})
	}
}

func TestLoadRejectsBadClientIPHeaderWithoutEchoingIt(t *testing.T) {
	for _, env := range []string{"production", "local", "test"} {
		for _, bad := range []string{"CF Connecting-IP-secret", "X-Real_IP-secret", "CF-Connecting-IP:-secret", "ＣＦ-secret", "X-Forwarded-For, secret"} {
			values := productionValues()
			values["APP_ENV"] = env
			values["CLIENT_IP_HEADER"] = bad
			_, err := Load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), "CLIENT_IP_HEADER") {
				t.Fatalf("%s %q: expected CLIENT_IP_HEADER error, got %v", env, bad, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("%s %q: error echoes input: %v", env, bad, err)
			}
		}
	}
}

func TestLoadAcceptsClientIPHeader(t *testing.T) {
	values := productionValues()
	values["CLIENT_IP_HEADER"] = " CF-Connecting-IP "
	cfg, err := Load(mapLookup(values))
	if err != nil || cfg.ClientIPHeader != "CF-Connecting-IP" {
		t.Fatalf("unexpected result: %q %v", cfg.ClientIPHeader, err)
	}
}

func TestLoadAllowsEmptyClientIPHeaderOutsideProduction(t *testing.T) {
	for _, env := range []string{"local", "test"} {
		for _, set := range []bool{false, true} {
			values := validValues()
			values["APP_ENV"] = env
			if set {
				values["CLIENT_IP_HEADER"] = "  "
			}
			cfg, err := Load(mapLookup(values))
			if err != nil || cfg.ClientIPHeader != "" {
				t.Fatalf("%s set=%v: unexpected result: %q %v", env, set, cfg.ClientIPHeader, err)
			}
		}
	}
}
