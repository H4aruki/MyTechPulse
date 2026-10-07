---
name: database-change
description: Plans and verifies safe MyTechPulse database changes. Use for schema changes, goose migrations, sqlc queries, constraints, indexes, seed data, backups, or destructive data operations.
---

# DB変更を安全に行う

1. 移行（`server/db/migrations/` のgoose SQL）、問い合わせ（`server/db/queries/`、sqlcで生成）、利用するGoのコードと、既存データへの影響を確認する。
2. **移行（Up）は追加だけにする。** 削除・名前の変更・型の変更・既定値の無いNOT NULLの列の追加は、自動デプロイが失敗して直前の版へ戻ったとき、DBが巻き戻らないため、直前の版が動かなくなる。`server/db/migrations/additive_test.go` が機械的に確かめる。列を消す・名前を変えるときは、複数回の更新に分ける（新しい列を足す → 新旧どちらも動く版を出す → 使わなくなってから消す）。
3. カラム追加・制約追加では、既存行、既定値、NULL、戻し方を先に決める。
4. データ削除、ファイル削除、ボリューム削除は実行前に利用者の許可を得る。
5. 本番DBへ接続しない。ローカルDBを使う場合も接続先を値ではなく構成から確認する。
6. `recommend.match_int` の10000倍整数（固定小数点）という保存形式を守る。
7. タグ名は、前後の空白を除いて小文字にそろえて同一視される。大文字小文字だけが違うタグを作らない。
8. 移行は再実行可能性（goose）、途中失敗、バックアップ要否を説明する。本番では自動デプロイが、入れ替えの前に移行を実行する。
9. 変更後は、`go test ./...`（DB結合試験は `TEST_DATABASE_URL` が要る）、代表的な読み書き、制約違反、既存データ互換を確認する。
