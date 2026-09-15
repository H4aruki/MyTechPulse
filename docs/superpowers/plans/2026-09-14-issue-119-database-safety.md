# Issue 119 Database Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 既存データを変更せず、DB構造を履歴管理し、バックアップを新しいDBへ復元して制約・件数・シーケンスを検証できるようにする。

**Architecture:** #120より前に読み取り専用のスキーマ監査と安全なバックアップ手順を作る。#120でGo・goose基盤ができた後、空DB作成と既存DB検証を両立するベースラインマイグレーションを追加する。

**Execution order:** Task 1〜2を先行し、SQLが0件でもビルドできる#120を完成させ、その後Task 3〜5で復元検証とベースラインを完成する。#120は本IssueのベースラインSQLに依存しない。#121・#123の開始には、本Issueの完了とタグ衝突監査の0件確認を必須とする。

**Tech Stack:** PostgreSQL 17、pg_dump/pg_restore、Bash、Go 1.26、goose v3.28.0、pgx v5.11.0

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 本番DBへCodexから接続しない。本番バックアップ取得は人間または承認済みCIが行う
- DBボリューム、既存DB、バックアップ世代を削除しない
- 復元先は新規の空DBだけにし、既存DB名を上書きしない
- `user`、`tag`、`recommend` と引用符付き `user_ID`、`tag_ID` を改名しない
- 既存行を更新するマイグレーションはこのIssueで作らない
- バックアップの外部保存先、費用、保存期間、世代削除は別途承認を得る
- `.env` の値を表示・記録しない

---

## File Map

- Create: `ops/sql/inspect_schema.sql` — スキーマ・制約・件数の読み取り
- Create: `ops/sql/audit_tag_collisions.sql` — 正規化タグの衝突件数と開始可否の監査
- Modify: `ops/backup_db.sh` — custom形式、チェックサム、削除なし
- Create: `ops/verify_backup.sh` — チェックサムとpg_restore一覧検査
- Create: `ops/restore_db.sh` — 新規検証DBへの復元
- Create: `ops/sql/verify_restored_db.sql` — 制約、関連、シーケンス検証
- Create: `ops/verify_restored_db.sh` — SQL検査と合成書き込み
- Create: `server/db/migrations/00001_legacy_baseline.sql` — 空DB作成と既存DB検証
- Create: `server/db/migrations/migrations_test.go` — 空DB・既存相当DBの適用テスト
- Create: `docs/deploy/database-backup-and-restore.md` — 人間向け手順

### Task 1: 現行スキーマを読み取り検査する

**Files:**
- Create: `ops/sql/inspect_schema.sql`
- Create: `ops/sql/audit_tag_collisions.sql`

**Interfaces:**
- Consumes: PostgreSQL 17のsystem catalog
- Produces: テーブル、列型、NULL、主キー、外部キー、UNIQUE、CASCADE、件数、シーケンスのTSV
- Produces: `lower(btrim(tag_name))` の衝突グループ件数と、#121・#123を開始できるかの終了コード

- [ ] **Step 1: 必須テーブル検査SQLを書く**

```sql
\set ON_ERROR_STOP on
SELECT to_regclass('public."user"') IS NOT NULL AS has_user,
       to_regclass('public.tag') IS NOT NULL AS has_tag,
       to_regclass('public.recommend') IS NOT NULL AS has_recommend;

SELECT table_name, column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name IN ('user', 'tag', 'recommend')
ORDER BY table_name, ordinal_position;

SELECT 'user' AS table_name, count(*) AS row_count FROM "user"
UNION ALL SELECT 'tag', count(*) FROM tag
UNION ALL SELECT 'recommend', count(*) FROM recommend;
```

同じファイルへ `pg_constraint` を使うPK、FK、UNIQUE、delete action検査と、`pg_get_serial_sequence` と最大IDの照合を追加する。結果に利用者名、パスワード、タグ名、興味度の実値を出さない。

`ops/sql/audit_tag_collisions.sql` は次の読み取り専用SQLとし、タグ名・正規形・IDは出さず件数だけを表示する。

```sql
\set ON_ERROR_STOP on
BEGIN READ ONLY;
SELECT count(*) AS collision_groups,
       count(*) = 0 AS can_continue
FROM (
  SELECT lower(btrim(tag_name))
  FROM public.tag
  GROUP BY lower(btrim(tag_name))
  HAVING count(*) > 1
) AS collisions
\gset audit_
\echo :audit_collision_groups
COMMIT;
\if :audit_can_continue
\else
  DO $$ BEGIN RAISE EXCEPTION 'tag normalization collision'; END $$;
\endif
```

`ON_ERROR_STOP` でSQL例外を非対話実行時の終了コード3へ変換する（[PostgreSQL 17 psqlの終了コード](https://www.postgresql.org/docs/17/app-psql.html#APP-PSQL-EXIT-STATUS)）。例外文言は固定とし、実値を含めない。

0件・終了コード0なら続行する。1件以上・終了コード3なら#121・#123の開始を停止する。監査失敗も続行不可とする。既存タグの表記・IDや興味度は自動更新しない。統合が必要なら、データ変更の対象（タグと関連するrecommend行）、理由、影響、統合方法・復旧方法をオーナーだけが確認できる承認用資料で提示し、別途明示承認を得る。公開ログ・CI・Issue・リポジトリへ実値を載せず、件数と可否のみ記録する。承認された対応後に再監査して0件を確認するまで停止を解除しない。

- [ ] **Step 2: ローカルDBで検査を実行する**

```bash
docker compose up -d db
docker compose exec -T db psql -v ON_ERROR_STOP=1 -U postgres -d mytechpulse < ops/sql/inspect_schema.sql
docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d mytechpulse < ops/sql/audit_tag_collisions.sql
```

Expected: 3テーブルがtrue、制約一覧と件数だけが表示される。DBが無い場合はこの時点で止め、ボリュームを作り直さない。

衝突監査は新規の合成検証DBで、タグなし・衝突なしなら0件/exit 0、`Go` と ` go ` なら1件/exit 3を確認する。本番の監査は人間または承認済みCIが実施し、Codexから接続しない。

- [ ] **Step 3: 読み取り専用であることを確認する**

```bash
rg -n "INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER" ops/sql/inspect_schema.sql ops/sql/audit_tag_collisions.sql
```

Expected: 0 matches。

- [ ] **Step 4: スキーマ監査をコミットする**

```bash
git add ops/sql/inspect_schema.sql ops/sql/audit_tag_collisions.sql
git commit -m "test(db): 現行スキーマ監査を追加" -m "Refs #119"
```

### Task 2: 削除を伴わないバックアップを作る

**Files:**
- Modify: `ops/backup_db.sh`
- Create: `ops/verify_backup.sh`

**Interfaces:**
- Consumes: `BACKUP_DIR`、`POSTGRES_PASSWORD`、Docker Composeの `db`
- Produces: `mytechpulse_YYYYmmddTHHMMSSZ.dump` と同名 `.sha256`

- [ ] **Step 1: 現行スクリプトの削除処理を検出するテストを実行する**

```bash
rg -n "find .* -delete|RETENTION_DAYS" ops/backup_db.sh
```

Expected: 現行の自動世代削除が見つかる。このIssueではバックアップ自体を削除せず、保持設計の承認まで無効化する。

- [ ] **Step 2: custom形式とチェックサムへ変更する**

中核処理を次に置き換える。

```bash
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="$BACKUP_DIR/mytechpulse_${STAMP}.dump"

docker compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" db \
  pg_dump -Fc -U postgres -d mytechpulse > "$DEST"

(cd "$(dirname "$DEST")" && sha256sum "$(basename "$DEST")") > "$DEST.sha256"
docker compose exec -T db pg_restore --list < "$DEST" > /dev/null
printf '%s\n' "$DEST"
```

`find ... -delete` と `RETENTION_DAYS` は削除する。これは将来の自動削除を止めるコード変更であり、既存バックアップファイル自体は削除しない。

チェックサムファイルの対象名はdumpのbasenameだけとする。生成時にdumpのディレクトリへ移動して計算するため、`BACKUP_DIR` が相対パスでも絶対パスでも同じ形式となり、搬送先でも検証できる。

- [ ] **Step 3: 検証スクリプトを書く**

```bash
#!/usr/bin/env bash
set -euo pipefail
dump_path="${1:?usage: verify_backup.sh DUMP_FILE}"
test -f "$dump_path"
test -f "$dump_path.sha256"
(cd "$(dirname "$dump_path")" && sha256sum -c "$(basename "$dump_path").sha256")
docker compose exec -T db pg_restore --list < "$dump_path" > /dev/null
printf 'OK: backup verified\n'
```

- [ ] **Step 4: 合成ローカルDBで取得・検証する**

```bash
backup_test_dir="$(mktemp -d ./backup-check-XXXXXX)"
dump_path="$(BACKUP_DIR="$backup_test_dir" ./ops/backup_db.sh)"
./ops/verify_backup.sh "$dump_path"
absolute_dump="$(cd "$(dirname "$dump_path")" && pwd)/$(basename "$dump_path")"
./ops/verify_backup.sh "$absolute_dump"
absolute_backup_dir="$(mktemp -d)"
absolute_created_dump="$(BACKUP_DIR="$absolute_backup_dir" ./ops/backup_db.sh)"
./ops/verify_backup.sh "$absolute_created_dump"
transport_dir="$(mktemp -d)"
cp "$dump_path" "$dump_path.sha256" "$transport_dir/"
./ops/verify_backup.sh "$transport_dir/$(basename "$dump_path")"
```

Expected: 相対パスでの取得・検証、絶対パスでの検証・取得、dumpと`.sha256`だけを別ディレクトリへ搬送した後の検証がすべてchecksum `OK` と `backup verified`。各`.sha256`のファイル名欄にディレクトリ部分が含まれないことも確認する。この検証で作った一時バックアップだけは検証後に削除できる。既存バックアップは触らない。

- [ ] **Step 5: バックアップ改善をコミットする**

```bash
git add ops/backup_db.sh ops/verify_backup.sh
git commit -m "chore(db): バックアップの利用可能性を検証" -m "Refs #119"
```

### Task 3: 新規DBだけへ復元して不変条件を検査する

**Files:**
- Create: `ops/restore_db.sh`
- Create: `ops/sql/verify_restored_db.sql`
- Create: `ops/verify_restored_db.sh`

**Interfaces:**
- Consumes: dumpパス、`mytechpulse_restore_` で始まる新規DB名
- Produces: 復元済み検証DBと、秘密情報を含まない検証結果

- [ ] **Step 1: 不正な復元先を拒否するシェルテストを書く**

```bash
if ./ops/restore_db.sh sample.dump mytechpulse; then
  echo "existing database name was accepted" >&2
  exit 1
fi
```

Expected: `restore database must start with mytechpulse_restore_` で失敗。

- [ ] **Step 2: 復元スクリプトを書く**

```bash
#!/usr/bin/env bash
set -euo pipefail
dump_path="${1:?usage: restore_db.sh DUMP_FILE NEW_DB_NAME}"
restore_db="${2:?usage: restore_db.sh DUMP_FILE NEW_DB_NAME}"
case "$restore_db" in
  mytechpulse_restore_*) ;;
  *) echo "restore database must start with mytechpulse_restore_" >&2; exit 2 ;;
esac

./ops/verify_backup.sh "$dump_path"
if docker compose exec -T db psql -U postgres -d postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname = '$restore_db'" | grep -q 1; then
  echo "restore database already exists" >&2
  exit 3
fi
docker compose exec -T db createdb -U postgres "$restore_db"
docker compose exec -T db pg_restore -U postgres -d "$restore_db" --exit-on-error < "$dump_path"
```

DB名は正規表現 `^mytechpulse_restore_[a-z0-9_]+$` でも検証し、SQL文字列への任意入力混入を防ぐ。

- [ ] **Step 3: 復元検査SQLを書く**

`verify_restored_db.sql` は次を `\if` とPL/pgSQL例外で失敗扱いにする。

```sql
\set ON_ERROR_STOP on
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM recommend r
    LEFT JOIN "user" u ON u."user_ID" = r."user_ID"
    LEFT JOIN tag t ON t."tag_ID" = r."tag_ID"
    WHERE u."user_ID" IS NULL OR t."tag_ID" IS NULL
  ) THEN
    RAISE EXCEPTION 'orphan recommend row';
  END IF;
END $$;
```

同じSQLで3テーブル、列、PK、FK、UNIQUE、NOT NULL、CASCADE、シーケンス値が最大IDより大きいことを検査する。値そのものは出力しない。

- [ ] **Step 4: 合成書き込みをrollback内で確認する**

```bash
docker compose exec -T db psql -v ON_ERROR_STOP=1 -U postgres -d "$restore_db" <<'SQL'
BEGIN;
INSERT INTO "user" (user_name, password) VALUES ('__restore_probe_user__', '$2b$12$synthetic.not.a.real.user.hash');
INSERT INTO tag (tag_name) VALUES ('__restore_probe_tag__');
INSERT INTO recommend ("user_ID", "tag_ID", match_int)
SELECT u."user_ID", t."tag_ID", 1
FROM "user" u, tag t
WHERE u.user_name = '__restore_probe_user__' AND t.tag_name = '__restore_probe_tag__';
ROLLBACK;
SQL
```

本番由来の値は表示せず、合成行はrollbackで残さない。

- [ ] **Step 5: 復元手順をコミットする**

```bash
git add ops/restore_db.sh ops/sql/verify_restored_db.sql ops/verify_restored_db.sh
git commit -m "test(db): バックアップ復元検証を追加" -m "Refs #119"
```

### Task 4: gooseベースラインを追加する（#120完了後）

**Files:**
- Create: `server/db/migrations/00001_legacy_baseline.sql`
- Create: `server/db/migrations/migrations_test.go`

**Interfaces:**
- Consumes: #120の `migrate.Run(ctx, db, fs) error`
- Consumes: #120の `server/db/migrations/embed.go` と常設 `README.md`（SQLが0件の状態でもcompile/test可能）
- Produces: 空DBへの3テーブル作成、既存DBの構造検証、goose version 1

- [ ] **Step 1: 空DBと不正な既存DBの失敗テストを書く**

```go
func TestLegacyBaselineCreatesEmptyDatabase(t *testing.T) {
    db := newIsolatedDatabase(t)
    if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
        t.Fatalf("migrate empty database: %v", err)
    }
    assertLegacySchema(t, db)
}

func TestLegacyBaselineRejectsIncompatibleExistingUserTable(t *testing.T) {
    db := newIsolatedDatabase(t)
    mustExec(t, db, `CREATE TABLE "user" ("user_ID" text PRIMARY KEY)`)
    err := migrate.Run(context.Background(), db, migrations.FS)
    if err == nil || !strings.Contains(err.Error(), "legacy schema mismatch") {
        t.Fatalf("expected schema mismatch, got %v", err)
    }
}
```

`newIsolatedDatabase` は `TEST_DATABASE_URL` が無ければskipし、既存DBをdropしない。テスト自身が作成したDBだけを `t.Cleanup` で削除する。

- [ ] **Step 2: テストがmigration欠如で失敗することを確認する**

```bash
cd server
go test ./db/migrations -run LegacyBaseline -v
```

Expected: ビルドは成功するが、SQLが0件でrunnerが何も適用しないため、3テーブル作成・不正スキーマ拒否のassertionがFAILする。

- [ ] **Step 3: ベースラインSQLを書く**

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS "user" (
  "user_ID" serial PRIMARY KEY,
  user_name varchar(50) NOT NULL UNIQUE,
  password varchar(255) NOT NULL
);
CREATE TABLE IF NOT EXISTS tag (
  "tag_ID" serial PRIMARY KEY,
  tag_name varchar(50) NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS recommend (
  "user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
  "tag_ID" integer NOT NULL REFERENCES tag("tag_ID") ON DELETE CASCADE,
  match_int integer NOT NULL,
  PRIMARY KEY ("user_ID", "tag_ID")
);
```

この後へsystem catalog検査のDO blockを置く。`information_schema.columns` でuser 3列・tag 2列・recommend 3列の計8列を、次のテーブル名と列名の組ごとに照合する。総列数だけで互換と判定しない。

| テーブル | 列 | 型 | NULL | 追加の照合 |
| --- | --- | --- | --- | --- |
| user | user_ID | integer | 不可 | serialの既定値と所有シーケンス |
| user | user_name | character varying | 不可 | 最大50文字 |
| user | password | character varying | 不可 | 最大255文字 |
| tag | tag_ID | integer | 不可 | serialの既定値と所有シーケンス |
| tag | tag_name | character varying | 不可 | 最大50文字 |
| recommend | user_ID | integer | 不可 | 参照先user.user_ID |
| recommend | tag_ID | integer | 不可 | 参照先tag.tag_ID |
| recommend | match_int | integer | 不可 | 興味度の整数保存 |

不足列、対象3テーブルの余分な列、型・文字数上限・NULL条件の不一致を拒否する。`pg_constraint` も件数だけでなく、3 PKの対象列（recommendはuser_ID/tag_IDの複合キー）、2 UNIQUEの対象列（user_name/tag_name）、2 FKの参照元・参照先と両方のCASCADEを照合する。`pg_get_serial_sequence` でuser/tag sequenceを検査し、不一致なら `RAISE EXCEPTION 'legacy schema mismatch'` とする。down節は作らない。SQLは既定の `server/db/migrations/00001_legacy_baseline.sql` へ追加し、#120のembed設定を変更せずに取り込めることを確認する。

- [ ] **Step 4: 空DB・互換DB・不正DBを検証する**

```bash
cd server
go test ./db/migrations -v
go test ./... -race
```

Expected: 空DBと互換DBはpass、不正DBを拒否するテストもpass。

- [ ] **Step 5: ベースラインをコミットする**

```bash
git add server/db/migrations
git commit -m "chore(db): 既存スキーマを移行基準へ登録" -m "Refs #119"
```

### Task 5: 手順書と最終検証を完成する

**Files:**
- Create: `docs/deploy/database-backup-and-restore.md`

**Interfaces:**
- Consumes: Task 1〜4のスクリプト
- Produces: 取得、検証、復元、失敗時中止、外部保存承認の手順

- [ ] **Step 1: 手順書を書く**

手順を `準備 → タグ衝突監査 → 取得 → checksum確認 → 新規DBへ復元 → 制約・件数確認 → 合成書き込み → 結果記録` の順にし、各段階の中止条件と秘密情報を記録しない例を載せる。衝突時の#121・#123停止、承認前の自動統合禁止、承認後の再監査を明記する。DBロール、環境変数名、Dockerボリューム、Caddy設定はdump外であることを明記する。

- [ ] **Step 2: シェル構文を確認する**

```bash
bash -n ops/backup_db.sh ops/verify_backup.sh ops/restore_db.sh ops/verify_restored_db.sh
```

Expected: exit 0。

- [ ] **Step 3: 全DB検査をローカル合成データで実行する**

```bash
cd server
go test ./db/migrations -v
cd ..
dump_path="$(./ops/backup_db.sh)"
./ops/verify_backup.sh "$dump_path"
./ops/restore_db.sh "$dump_path" mytechpulse_restore_issue119
./ops/verify_restored_db.sh mytechpulse_restore_issue119
```

Expected: 全工程OK。`dump_path` はこの実行で新規作成した合成dumpを指す。既存バックアップや既存DBは削除しない。

- [ ] **Step 4: 文書をコミットする**

```bash
git add docs/deploy/database-backup-and-restore.md
git commit -m "docs(db): バックアップ復元手順を記録" -m "Refs #119"
```

- [ ] **Step 5: PRを作る**

PR本文には、空DB・既存相当DB・不正DBの結果、合成バックアップのchecksum、既存データを変更していないこと、外部保存は未実行であることを記載する。外部保存先と費用をオーナーが承認し、承認済み環境で復元できることを人間が確認するまで `Closes #119` を付けない。
