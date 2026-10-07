---
name: auth-security-review
description: Reviews MyTechPulse authentication and security-sensitive changes. Use for sessions, login, signup, authorization, secrets, CORS, CSRF, input validation, cookies, or protected endpoints.
---

# 認証と安全性を確認する

- `.env` や `server/.env` を読まず、設定名は `.env.example`・`server/.env.example` で確認する。
- ユーザー不存在とパスワード不一致で、本文・HTTP状態・処理時間に不要な差を作らない。
- 認証はサーバー側セッション（`docs/adr/0002-use-server-side-sessions.md`）。Cookieは HttpOnly・SameSite=Lax で、本番は `__Host-` 付きの名前と Secure。DBには乱数トークンのSHA-256ハッシュだけを保存し、平文を保存・ログ出力しない。
- 保護APIは、セッションから利用者を特定する。要求本文の利用者IDだけを信用しない。
- 更新系（`/api/v1/` のPOST）の、許可Origin検査と `X-MTP-CSRF` ヘッダーを弱めない。`CORS_ALLOWED_ORIGINS` に広すぎる値（`*` など）を入れない。
- 期限切れ・ログアウトでサーバー側のセッションを失効させる挙動を保つ。
- 本番では、Swagger UI・OpenAPIの公開を有効にしない（`APP_ENV=production` では有効にできない）。
- CORS、入力制限、ログ、例外文にトークンや個人情報を出さない。
- 画面側は、認証情報を `localStorage` 等へ保存しない。新しい注入経路（XSS）を作らない。
- 指摘は攻撃条件、影響、対策、未確認事項を分ける。本番環境へ接続して確認しない。
