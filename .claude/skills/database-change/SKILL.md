---
name: database-change
description: Plans and verifies safe MyTechPulse database changes. Use for SQLAlchemy models, schema changes, migrations, constraints, indexes, seed data, backups, or destructive data operations.
---

# DB変更を安全に行う

1. model、crud、service、schemaと既存データへの影響を確認する。
2. カラム追加・型変更・制約追加では、既存行、既定値、NULL、戻し方を先に決める。
3. データ削除、ファイル削除、ボリューム削除は実行前に利用者の許可を得る。
4. 本番DBへ接続しない。ローカルDBを使う場合も接続先を値ではなく構成から確認する。
5. `recommend.match_int` の10000倍整数という保存形式を守る。
6. 移行は再実行可能性、途中失敗、ロールバック、バックアップ要否を説明する。
7. 変更後はモデル作成、代表的な読み書き、制約違反、既存データ互換を確認する。
