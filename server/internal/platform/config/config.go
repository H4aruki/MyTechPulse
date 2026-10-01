// Package config は環境変数から起動設定を読み込み、起動前に検証する。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment      string
	HTTPAddr         string
	DatabaseURL      string
	QiitaToken       string
	CORSOrigins      []string
	SessionTTL       time.Duration
	ProviderTimeout  time.Duration
	FeedTimeout      time.Duration
	ProviderMaxBytes int64
	SwaggerEnabled   bool
	CookieName       string
	CookieSecure     bool
}

// Load は lookup から設定を読む。エラーに入力値は含めない。
func Load(lookup func(string) (string, bool)) (Config, error) {
	environment := getDefault(lookup, "APP_ENV", "local")
	if environment != "local" && environment != "test" && environment != "production" {
		return Config{}, errors.New("APP_ENVはlocal/test/productionのいずれかにしてください")
	}
	databaseURL, err := required(lookup, "DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	qiitaToken, err := required(lookup, "QIITA_ACCESS_TOKEN")
	if err != nil {
		return Config{}, err
	}
	origins, err := parseOrigins(getDefault(lookup, "CORS_ALLOWED_ORIGINS", "http://localhost:5173"))
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := parseDuration(lookup, "SESSION_TTL", "24h")
	if err != nil {
		return Config{}, err
	}
	providerTimeout, err := parseDuration(lookup, "PROVIDER_TIMEOUT", "5s")
	if err != nil {
		return Config{}, err
	}
	feedTimeout, err := parseDuration(lookup, "FEED_TIMEOUT", "8s")
	if err != nil {
		return Config{}, err
	}
	maxBytes, err := parsePositiveInt64(lookup, "PROVIDER_MAX_BYTES", "2097152")
	if err != nil {
		return Config{}, err
	}
	swagger, err := parseBool(lookup, "SWAGGER_ENABLED", environment != "production")
	if err != nil {
		return Config{}, err
	}
	if environment == "production" && swagger {
		return Config{}, errors.New("productionではSWAGGER_ENABLEDをtrueにできません")
	}
	cookieName, cookieSecure := "mtp_session", false
	if environment == "production" {
		cookieName, cookieSecure = "__Host-mtp_session", true
	}
	return Config{
		Environment:      environment,
		HTTPAddr:         getDefault(lookup, "HTTP_ADDR", ":8001"),
		DatabaseURL:      databaseURL,
		QiitaToken:       qiitaToken,
		CORSOrigins:      origins,
		SessionTTL:       sessionTTL,
		ProviderTimeout:  providerTimeout,
		FeedTimeout:      feedTimeout,
		ProviderMaxBytes: maxBytes,
		SwaggerEnabled:   swagger,
		CookieName:       cookieName,
		CookieSecure:     cookieSecure,
	}, nil
}

func required(lookup func(string) (string, bool), key string) (string, error) {
	value, _ := lookup(key)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%sが未設定です", key)
	}
	return value, nil
}

func getDefault(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

func parseDuration(lookup func(string) (string, bool), key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(getDefault(lookup, key, fallback))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%sは0より大きい期間(例: 5s)で指定してください", key)
	}
	return d, nil
}

func parsePositiveInt64(lookup func(string) (string, bool), key, fallback string) (int64, error) {
	n, err := strconv.ParseInt(getDefault(lookup, key, fallback), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%sは正の整数で指定してください", key)
	}
	return n, nil
}

func parseBool(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%sはtrueまたはfalseで指定してください", key)
	}
	return b, nil
}

func parseOrigins(raw string) ([]string, error) {
	errInvalid := errors.New("CORS_ALLOWED_ORIGINSはhttp/httpsのorigin(path・query・userinfoなし)をカンマ区切りで指定してください")
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "*" {
			return nil, errInvalid
		}
		u, err := url.Parse(part)
		if err != nil {
			return nil, errInvalid
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
			u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" ||
			u.Fragment != "" || strings.Contains(part, "?") || strings.Contains(part, "#") {
			return nil, errInvalid
		}
		origins = append(origins, u.Scheme+"://"+u.Host)
	}
	return origins, nil
}
