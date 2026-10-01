// Command migrate はDBマイグレーションを明示的に実行する。APIの起動時には実行しない。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

func main() {
	const usage = "使い方: migrate\n\n環境変数DATABASE_URLのDBへ、埋め込み済みのマイグレーションを未適用分だけ適用します。\nSQLが0件でも正常終了します。"
	flag.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	help := flag.Bool("help", false, "使い方を表示する")
	flag.Parse()
	if *help {
		fmt.Fprintln(os.Stdout, usage)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	url, _ := os.LookupEnv("DATABASE_URL")
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("DATABASE_URLが未設定です")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("DATABASE_URLの形式が不正です")
	}
	defer db.Close()
	ctx := context.Background()
	if err := migrate.Run(ctx, db, migrations.FS); err != nil {
		return fmt.Errorf("マイグレーションに失敗しました: %w", err)
	}
	return nil
}
