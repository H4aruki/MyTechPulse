---
name: api-contract
description: Coordinates MyTechPulse backend and frontend API contract changes. Use when editing routes, schemas, response status values, error behavior, request fields, or frontend API types.
---

# API契約を変更する

1. 契約の正本は `server/openapi/openapi.json`（`go run ./cmd/openapi` で生成）。Goのハンドラ・型と、`frontend/src/api/` の呼び出し・型を両方確認する。
2. 結果はHTTPステータスで表し、エラーの本文はProblem Details。旧Python版の「本文の `status` で表す」規約は使わない。
3. ステータスや本文の形を変える場合は、OpenAPIの再生成と、画面側の型・分岐を同時に更新する（CIが生成物の差分を検査する）。
4. 必須・任意、null、既定値、エラー文言、後方互換性を確認する。
5. ログイン失敗で利用者の存在を推測できる差を作らない（存在しない場合とパスワード違いは、同じ `invalid_credentials`）。
6. 更新系の要求（POST）は、許可Originと `X-MTP-CSRF` ヘッダーが要ることを前提にする。
7. バックエンド検証（`go test ./...`）とフロントエンドのlint・test・buildを実行し、可能なら成功・失敗・境界値を確認する。
