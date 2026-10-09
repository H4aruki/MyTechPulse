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

## 新しい窓口を足すときに触るファイル

1. サーバーのDB変更は `server/db/migrations/` に追加する（既存の移行は変更・削除しない）。
2. SQLを `server/db/queries/` に追加・変更してから、`server/` で `go tool sqlc generate` を実行する。既存3表は `server/db/sqlc/legacy_schema.sql` から読む。
3. DB処理を `server/internal/store/` に追加し、機能を `server/internal/<機能>/` の model・service・handler に実装する。
4. `server/internal/app/app.go` の `Dependencies` と窓口登録、`server/cmd/api/main.go` の実依存の組み立てを更新する。
5. `server/cmd/openapi/main.go` の `Dependencies` にも機能の空の値を渡す。渡さないと仕様ファイルに窓口が出ない。
6. `server/` で `go run ./cmd/openapi` を実行し、`server/openapi/openapi.json` を更新する。
7. 画面は `frontend/` で `npm run api:generate` を実行し、`src/api/types.ts` の型別名、`endpoints.ts` の呼び出し、`generated-contract.ts` の契約参照を順に更新する。
8. `src/api/client.ts` の `request` は現在GET・POSTのみ。PUTなど別の方式を使うなら、ここで受け付ける方式も追加する。
9. 新しい呼び出し関数を `vi.mock('@/api/endpoints')` で差し替える既存試験にもモックを足す（例: `frontend/src/pages/ArticlesPage.test.tsx`）。
10. DB試験の補助は `server/internal/store/auth_integration_test.go` の `newTestPool`・`queryInt`・`hashOf`・`fixedNow`、移行試験は `server/db/migrations/migrations_test.go` の `newIsolatedDatabase` を参考にする。
11. ハンドラー試験は `app.New` で依存を組み立てる（例: `server/internal/recommendation/handler_test.go`）。
12. `docs/BasicDesignSpecifications/API/ApiList.md` の一覧表に行を足し、「現在の窓口は以上のN個」の数も更新する。
13. 窓口の詳しい説明を `docs/BasicDesignSpecifications/API/Details/` に追加し、`docs/DocumentMap.md` の資料一覧も更新する。
