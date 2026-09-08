---
name: auth-security-review
description: Reviews MyTechPulse authentication and security-sensitive changes. Use for JWT, login, signup, authorization, secrets, CORS, input validation, tokens, or protected endpoints.
---

# 認証と安全性を確認する

- `.env` や `backend/.env` を読まず、設定名は `.env.example` で確認する。
- ユーザー不存在とパスワード不一致で、本文・HTTP状態・処理時間に不要な差を作らない。
- 保護APIは `get_current_user` を通り、要求本文の利用者IDだけを信用しない。
- JWTの署名方式、期限、Bearer形式、秘密鍵未設定時の失敗を保つ。
- CORS、入力制限、ログ、例外文にトークンや個人情報を出さない。
- フロントの `localStorage` 利用にはXSS時の窃取リスクが残るため、新しい注入経路を作らない。
- 指摘は攻撃条件、影響、対策、未確認事項を分ける。本番環境へ接続して確認しない。
