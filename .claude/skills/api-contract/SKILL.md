---
name: api-contract
description: Coordinates MyTechPulse backend and frontend API contract changes. Use when editing routes, schemas, response status values, error behavior, request fields, or frontend API types.
---

# API契約を変更する

1. バックエンドのroute・schemaと、`frontend/src/api/` の呼び出し・型を両方確認する。
2. 本文の `status` を使う既存規約と、HTTP 401を使う認証例外を区別する。
3. `status` の値や意味を変える場合は `ApiStatus` と画面分岐も同時に更新する。
4. 必須・任意、null、既定値、エラー文言、後方互換性を確認する。
5. ログイン失敗で利用者の存在を推測できる差を作らない。
6. バックエンド検証とフロントエンドのlint・buildを実行し、可能なら成功・失敗・境界値を確認する。
