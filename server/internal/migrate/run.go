// Package migrate は埋め込まれたgoose SQLを明示的に適用する。
package migrate

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Run は migrationFS 直下のSQLを未適用分だけ適用する。SQLが0件ならDBへ触れず成功する。
func Run(ctx context.Context, db *sql.DB, migrationFS fs.FS) error {
	files, err := fs.Glob(migrationFS, "*.sql")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, ".")
}
