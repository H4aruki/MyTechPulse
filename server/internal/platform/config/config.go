// Package config は環境変数から起動設定を読み込み、起動前に検証する。
package config

import (
	"errors"
	"fmt"
	"net/netip"
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
	// ClientIPHeader はログイン試行の制限に使う接続元IPを読むヘッダー名。productionでは必須。local/testで空なら接続元アドレス。
	ClientIPHeader string
	// パスキー(WebAuthn)。RPIDは登録済みのパスキーに結び付くため、公開後は変えない。
	WebAuthnRPID    string
	WebAuthnRPName  string
	WebAuthnOrigins []string
	// ArticleFetch は記事の定期一括取得(Issue #66)。既定は無効(全環境)。
	ArticleFetch ArticleFetch
}

// ArticleFetch は記事の定期一括取得の設定。定時・保持期間・件数・並列数は決定事項なので設定にしない。
type ArticleFetch struct {
	Enabled bool
	// QiitaMaxRequests は1回の実行でQiitaへ通信してよい回数（再試行を含む）。
	QiitaMaxRequests int
	// RunTimeout は1回の実行の上限。
	RunTimeout time.Duration
}

const maxQiitaMaxRequests = 800

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
	origins, err := parseOrigins("CORS_ALLOWED_ORIGINS", getDefault(lookup, "CORS_ALLOWED_ORIGINS", "http://localhost:5173"))
	if err != nil {
		return Config{}, err
	}
	rpID, rpName, rpOrigins, err := parseWebAuthn(lookup, environment)
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
	clientIPHeader, err := parseClientIPHeader(lookup, environment)
	if err != nil {
		return Config{}, err
	}
	articleFetch, err := parseArticleFetch(lookup)
	if err != nil {
		return Config{}, err
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
		ClientIPHeader:   clientIPHeader,
		WebAuthnRPID:     rpID,
		WebAuthnRPName:   rpName,
		WebAuthnOrigins:  rpOrigins,
		ArticleFetch:     articleFetch,
	}, nil
}

// parseArticleFetch は記事の定期一括取得の設定を読む。ARTICLE_FETCH_ENABLED の既定は、どの環境でも false。
func parseArticleFetch(lookup func(string) (string, bool)) (ArticleFetch, error) {
	enabled, err := parseBool(lookup, "ARTICLE_FETCH_ENABLED", false)
	if err != nil {
		return ArticleFetch{}, err
	}
	maxRequests, err := parsePositiveInt64(lookup, "ARTICLE_FETCH_QIITA_MAX_REQUESTS", "300")
	if err != nil || maxRequests > maxQiitaMaxRequests {
		return ArticleFetch{}, fmt.Errorf("ARTICLE_FETCH_QIITA_MAX_REQUESTSは1〜%dの整数で指定してください", maxQiitaMaxRequests)
	}
	timeout, err := parseDuration(lookup, "ARTICLE_FETCH_RUN_TIMEOUT", "20m")
	if err != nil {
		return ArticleFetch{}, err
	}
	return ArticleFetch{Enabled: enabled, QiitaMaxRequests: int(maxRequests), RunTimeout: timeout}, nil
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

func parseOrigins(key, raw string) ([]string, error) {
	errInvalid := fmt.Errorf("%sはhttp/httpsのorigin(path・query・userinfoなし)をカンマ区切りで指定してください", key)
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

// parseClientIPHeader はCLIENT_IP_HEADERを読んで検証する。
// 本番で空だと、中継(Cloudflare/Caddy)の裏では全員が同じIPに見え、ログイン試行の制限が全員で1つの枠になるため、必須にする。
func parseClientIPHeader(lookup func(string) (string, bool), environment string) (string, error) {
	value, _ := lookup("CLIENT_IP_HEADER")
	value = strings.TrimSpace(value)
	if value == "" {
		if environment == "production" {
			return "", errors.New("productionではCLIENT_IP_HEADERが必須です(例: CF-Connecting-IP)。空だと全員が同じ接続元に見え、ログイン試行の制限が全員で共有されます")
		}
		return "", nil
	}
	if !validHeaderName(value) {
		return "", errors.New("CLIENT_IP_HEADERはHTTPヘッダー名(英数字とハイフンのみ)で指定してください")
	}
	return value, nil
}

// validHeaderName はHTTPヘッダー名として、英数字とハイフンだけでできているかを確かめる。
func validHeaderName(name string) bool {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return name != ""
}

// parseWebAuthn はパスキーの設定を読んで検証する。未設定または空のときは、環境ごとの既定値を使う。
// 本番は、localhostやIPアドレスを使えず、オリジンはhttpsだけ。オリジンのホストは、RP IDと同じか、その配下に限る。
func parseWebAuthn(lookup func(string) (string, bool), environment string) (rpID, rpName string, origins []string, err error) {
	production := environment == "production"
	defaultID, defaultOrigins := "localhost", "http://localhost:5173"
	if production {
		defaultID, defaultOrigins = "mytechpulse.net", "https://mytechpulse.net"
	}
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	rpID = strings.ToLower(value("WEBAUTHN_RP_ID", defaultID))
	rpName = value("WEBAUTHN_RP_NAME", "MyTechPulse")
	if !validRPID(rpID) || (production && isLocalHost(rpID)) {
		return "", "", nil, errors.New("WEBAUTHN_RP_IDはドメイン名だけで指定してください(スキーム・ポート・パスなし。本番ではlocalhostとIPアドレスは使えません)")
	}
	origins, err = parseOrigins("WEBAUTHN_ORIGINS", value("WEBAUTHN_ORIGINS", defaultOrigins))
	if err != nil {
		return "", "", nil, err
	}
	for _, origin := range origins {
		u, _ := url.Parse(origin)
		host := strings.ToLower(u.Hostname())
		if host != rpID && !strings.HasSuffix(host, "."+rpID) {
			return "", "", nil, errors.New("WEBAUTHN_ORIGINSのホストは、WEBAUTHN_RP_IDと同じか、その配下にしてください")
		}
		if production && (u.Scheme != "https" || isLocalHost(host)) {
			return "", "", nil, errors.New("productionのWEBAUTHN_ORIGINSは、localhostを含まないhttpsのoriginにしてください")
		}
	}
	return rpID, rpName, origins, nil
}

// validRPID はRP IDとして使えるドメイン名か(英数字とハイフンのラベルをドットでつないだもの)を確かめる。
func validRPID(id string) bool {
	if id == "" || len(id) > 253 {
		return false
	}
	for _, label := range strings.Split(id, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// isLocalHost はlocalhost(配下を含む)またはIPアドレスらしいホスト名か。
func isLocalHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && ip.IsValid() {
		return true
	}
	return false
}
