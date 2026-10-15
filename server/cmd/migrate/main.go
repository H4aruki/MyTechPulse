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
	const usage = "使い方: migrate [-version]\n\n環境変数DATABASE_URLのDBへ、埋め込み済みのマイグレーションを未適用分だけ適用します。\nSQLが0件でも正常終了します。\n-version を付けると、適用はせず、DBの現在の版（数字だけ）を表示します。"
	flag.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	help := flag.Bool("help", false, "使い方を表示する")
	showVersion := flag.Bool("version", false, "適用済みの版を表示する（適用はしない）")
	flag.Parse()
	if *help {
		fmt.Fprintln(os.Stdout, usage)
		return
	}
	if err := run(*showVersion); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(showVersion bool) error {
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
	if showVersion {
		v, err := migrate.Version(ctx, db)
		if err != nil {
			return fmt.Errorf("DBの版を取得できません")
		}
		fmt.Fprintln(os.Stdout, v)
		return nil
	}
	if err := migrate.Run(ctx, db, migrations.FS); err != nil {
		return fmt.Errorf("マイグレーションに失敗しました: %w", err)
	}
	return nil
}
