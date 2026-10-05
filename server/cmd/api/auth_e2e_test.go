package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

// 実際のbcrypt・PostgreSQL・HTTP経路をつなぎ、Python版が作ったハッシュでログインできることまで確かめる。
// テスト専用の一時DBを作り、終了時にそのDBだけを破棄する。
func TestAuthEndToEndWithRealDependencies(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mtp_e2etest_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		db.Close()
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
		admin.Close()
	})

	// #118のPython生成bcrypt(合成値)を持つ既存利用者を作る
	fixture, err := os.ReadFile("../../../testdata/compatibility/auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Password string `json:"password"`
		Bcrypt2b string `json:"bcrypt_2b"`
	}
	if err := json.Unmarshal(fixture, &f); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO "user"(user_name, password) VALUES ('legacy-user', $1)`, f.Bcrypt2b); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{
		Environment: "test", SwaggerEnabled: true, CookieName: "mtp_session",
		CORSOrigins: []string{"http://localhost:5173"}, SessionTTL: time.Hour,
	}
	svc, err := newAuthService(cfg, pool, logger)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool, Auth: svc})

	send := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-MTP-CSRF", "1")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	cookieOf := func(rec *httptest.ResponseRecorder) *http.Cookie {
		cs := (&http.Response{Header: rec.Header()}).Cookies()
		if len(cs) != 1 {
			t.Fatalf("want one cookie, got %v", rec.Header().Values("Set-Cookie"))
		}
		return cs[0]
	}

	// 既存利用者がPython版のハッシュでログインできる
	login := send(http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"username":"legacy-user","password":%q}`, f.Password), nil)
	if login.Code != http.StatusOK {
		t.Fatalf("legacy login = %d: %s", login.Code, login.Body.String())
	}
	legacyCookie := cookieOf(login)
	if me := send(http.MethodGet, "/api/v1/auth/me", "", legacyCookie); me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"member"`) {
		t.Fatalf("me = %d: %s", me.Code, me.Body.String())
	}
	bad := send(http.MethodPost, "/api/v1/auth/login", `{"username":"legacy-user","password":"wrong"}`, nil)
	missing := send(http.MethodPost, "/api/v1/auth/login", `{"username":"nobody","password":"wrong"}`, nil)
	if bad.Code != http.StatusUnauthorized || bad.Body.String() != missing.Body.String() {
		t.Fatalf("failures must be identical: %d %s / %d %s", bad.Code, bad.Body.String(), missing.Code, missing.Body.String())
	}

	// 新規登録 → 初期の興味度が保存され、すぐログイン状態になる
	signup := send(http.MethodPost, "/api/v1/auth/signup", `{"username":"new-user","password":"new-password","favorite_tags":["Go","go","Rust"]}`, nil)
	if signup.Code != http.StatusCreated {
		t.Fatalf("signup = %d: %s", signup.Code, signup.Body.String())
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM recommend r JOIN "user" u USING ("user_ID")
		WHERE u.user_name = 'new-user' AND r.match_int = 1`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("initial interests = %d, %v", n, err)
	}
	if dup := send(http.MethodPost, "/api/v1/auth/signup", `{"username":"new-user","password":"x","favorite_tags":["go"]}`, nil); dup.Code != http.StatusConflict {
		t.Fatalf("duplicate signup = %d", dup.Code)
	}
	// 保存されたパスワードはbcryptハッシュで、平文ではない
	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT password FROM "user" WHERE user_name = 'new-user'`).Scan(&stored); err != nil ||
		!strings.HasPrefix(stored, "$2a$12$") && !strings.HasPrefix(stored, "$2b$12$") {
		t.Fatalf("stored password = %q, %v", stored, err)
	}

	// ログアウトで該当セッションだけが無効になる
	newCookie := cookieOf(signup)
	if out := send(http.MethodPost, "/api/v1/auth/logout", "", newCookie); out.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", out.Code)
	}
	if me := send(http.MethodGet, "/api/v1/auth/me", "", newCookie); me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d", me.Code)
	}
	if me := send(http.MethodGet, "/api/v1/auth/me", "", legacyCookie); me.Code != http.StatusOK {
		t.Fatalf("other session must survive, got %d", me.Code)
	}
}
