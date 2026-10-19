#!/usr/bin/env bash
# #240 既存の利用者の興味度を、新しい初期値のルール（選ばなかったタグには行を作らない）にそろえる、一回だけの作業。
#
# 何をするか
#   1. #203 の後に登録した利用者の「登録時に選ばず、一度も押していないタグ」の行を消す（delete_unselected）
#   2. #203 より前に登録し、一度もクリックしていない利用者（行の値がすべて1）の行を10000（1.0）に直す（raise_legacy）
#   見分け方の詳細は ops/sql/initial_interest_240_functions.sql の先頭を参照。
#   DBの移行（goose）では行わない。移行は追加だけの決まり（server/db/migrations/README.md、additive_test.go）で、
#   自動デプロイで流れて巻き戻せないため。
#
# 使い方（リポジトリのルートで実行する）
#   ./ops/fix_initial_interest_240.sh --report              読み取りだけ。対象の件数と内訳を出す
#   ./ops/fix_initial_interest_240.sh [--dry-run]           読み取りだけ。対象の件数を出す（既定）
#   ./ops/fix_initial_interest_240.sh --verify              読み取りだけ。対象が0件なら0、残っていれば1で終わる
#   ./ops/fix_initial_interest_240.sh --execute --backup backups/mytechpulse_X.dump [--yes]
#                                                           実際に直す。専用のバックアップ（.keep 付き）が必要
#   環境変数: DB_NAME（既定 mytechpulse。復元した別のDBで予行するときに変える）、POSTGRES_PASSWORD（必要なときだけ）
#
# 本番での手順（#240 の決定。オーナーの承認が要るのは 4 だけ）
#   0. #240 のコード変更（登録で未選択の行を作らない・フィードで0のタグを除く）が本番に入っていることを確かめる。
#      先に流すと、まだ古い登録処理で、新しく登録した人に8000の行ができてしまう。
#   1. 読み取りだけの確認:  ./ops/fix_initial_interest_240.sh --report
#      出た件数（人数・行数だけ）を #240 に記録する。利用者名やタグ名は出さないし、貼らない。
#   2. 専用のバックアップ:
#        dump_path="$(./ops/backup_db.sh | tail -1)" && ./ops/verify_backup.sh "$dump_path" && touch "$dump_path.keep"
#      .keep を付けると、7日を超えても自動で消えない（ops/prune_backups.sh）。戻すときは
#      docs/deploy/database-backup-and-restore.md の手順で、DB全体をこのバックアップへ戻す。
#   3. （任意・推奨）予行: ./ops/restore_db.sh "$dump_path" mytechpulse_rehearsal_240 で復元したDBに対して、
#      DB_NAME=mytechpulse_rehearsal_240 で 1→4→5 を流し、件数が 1 と合うことを確かめる。
#   4. オーナーの承認のうえで実行:
#        ./ops/fix_initial_interest_240.sh --execute --backup "$dump_path"
#      利用者1人ずつ、クリックの記録と同じ行ロック（SELECT … FROM "user" WHERE "user_ID"=$1 FOR UPDATE）を取ってから
#      対象を数え直して直し、1人ずつ確定する。サービスを止める必要はない。途中で止まっても、もう一度流せばよい
#      （済んだ人は対象から外れる）。
#   5. 結果の確認:  ./ops/fix_initial_interest_240.sh --verify   （対象が0件で、終了コード0になること）
#      件数を #240 に記録する。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
functions_sql="$repo_root/ops/sql/initial_interest_240_functions.sql"
report_sql="$repo_root/ops/sql/initial_interest_240_report.sql"
DB_NAME="${DB_NAME:-mytechpulse}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"

usage() {
  sed -n '/^# 使い方/,/^#   環境変数/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

mode="dry-run"
backup=""
assume_yes=false
while [ $# -gt 0 ]; do
  case "$1" in
    --report) mode="report" ;;
    --dry-run) mode="dry-run" ;;
    --verify) mode="verify" ;;
    --execute) mode="execute" ;;
    --backup)
      [ $# -ge 2 ] || usage
      backup="$2"
      shift
      ;;
    --yes) assume_yes=true ;;
    -h | --help) usage ;;
    *)
      printf 'ERROR: 知らない引数です: %s\n' "$1" >&2
      usage
      ;;
  esac
  shift
done

if [[ ! "$DB_NAME" =~ ^[a-z_][a-z0-9_]*$ ]]; then
  printf 'ERROR: DB_NAME が不正です: %s\n' "$DB_NAME" >&2
  exit 2
fi
if [ "$mode" != "execute" ] && [ -n "$backup" ]; then
  printf 'ERROR: --backup は --execute と一緒に使います\n' >&2
  exit 2
fi

psql_in_db() {
  local exec_args=(docker compose exec -T)
  if [ -n "$POSTGRES_PASSWORD" ]; then
    exec_args+=(-e "PGPASSWORD=$POSTGRES_PASSWORD")
  fi
  "${exec_args[@]}" db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$DB_NAME" "$@"
}

summary_sql() {
  cat "$functions_sql"
  printf '%s\n' 'BEGIN TRANSACTION READ ONLY;' \
    "\\echo '== 対象（delete_unselected=消す行、raise_legacy=10000に直す行）'" \
    'SELECT * FROM pg_temp.ii240_summary();' 'ROLLBACK;'
}

remaining_rows() {
  # 対象の残りの行数（両方の合計）だけを出す
  {
    cat "$functions_sql"
    printf '%s\n' 'BEGIN TRANSACTION READ ONLY;' 'SELECT COALESCE(sum(rows), 0) FROM pg_temp.ii240_summary();' 'ROLLBACK;'
  } | psql_in_db -A -t | tr -d '[:space:]'
}

case "$mode" in
  report)
    cat "$functions_sql" "$report_sql" | psql_in_db
    ;;
  dry-run)
    printf '読み取りだけです。データは変えません（DB: %s）\n' "$DB_NAME"
    summary_sql | psql_in_db
    printf '実行するときは --execute --backup <専用のバックアップ> を付けます（手順はこのファイルの先頭）\n'
    ;;
  verify)
    left="$(remaining_rows)"
    printf '残っている対象の行数: %s（DB: %s）\n' "$left" "$DB_NAME"
    [ "$left" = "0" ] || exit 1
    ;;
  execute)
    if [ -z "$backup" ]; then
      printf 'ERROR: --execute には --backup <専用のバックアップ> が必要です\n' >&2
      exit 2
    fi
    if [ ! -f "$backup" ] || [ ! -f "$backup.keep" ]; then
      printf 'ERROR: バックアップ %s と、自動で消えないようにする %s.keep の両方が必要です\n' "$backup" "$backup" >&2
      exit 1
    fi
    "$repo_root/ops/verify_backup.sh" "$backup"
    printf '実行前の対象（DB: %s）:\n' "$DB_NAME"
    summary_sql | psql_in_db
    if [ "$assume_yes" != true ]; then
      if [ ! -t 0 ]; then
        printf 'ERROR: 確認の入力ができません。オーナーの承認を得たうえで --yes を付けてください\n' >&2
        exit 1
      fi
      printf 'オーナーの承認を得ていれば、EXECUTE と入力してください: '
      read -r answer
      if [ "$answer" != "EXECUTE" ]; then
        printf '中止しました（何も変えていません）\n' >&2
        exit 1
      fi
    fi
    {
      cat "$functions_sql"
      printf '%s\n' "SET lock_timeout = '5s';" 'CALL pg_temp.ii240_run();'
    } | psql_in_db
    left="$(remaining_rows)"
    printf '実行後に残っている対象の行数: %s\n' "$left"
    [ "$left" = "0" ] || {
      printf 'ERROR: 対象が残っています。もう一度 --execute を流すか、原因を調べてください\n' >&2
      exit 1
    }
    printf 'OK: 完了しました。--verify の結果と件数を #240 に記録してください\n'
    ;;
esac
