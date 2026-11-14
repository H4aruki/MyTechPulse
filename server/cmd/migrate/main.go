// Command migrate はDBマイグレーションを明示的に実行する。APIの起動時には実行しない。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

// usageText は -help のとき標準出力へ、-h や不明な引数のとき標準エラーへ出す説明。
const usageText = "使い方: migrate [-version]\n\n環境変数DATABASE_URLのDBへ、埋め込み済みのマイグレーションを未適用分だけ適用します。\nSQLが0件でも正常終了します。\n-version を付けると、適用はせず、DBの現在の版（数字だけ）を表示します。"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run は引数を解釈し、終了コードを返す。出力先は呼び出し側から渡す。
// 使い方の表示と成功は 0、DBの失敗は 1、フラグの誤りは 2。
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usageText) }
	help := fs.Bool("help", false, "使い方を表示する")
	showVersion := fs.Bool("version", false, "適用済みの版を表示する（適用はしない）")
	if err := fs.Parse(args); err != nil {
		// -h は使い方を出して ErrHelp を返す。終了コードは 0（flag の既定と同じ）。
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *help {
		fmt.Fprintln(stdout, usageText)
		return 0
	}
	if err := runDatabase(*showVersion, stdout); err != nil {
		fmt.Fprintln(stderr, "migrate:", err)
		return 1
	}
	return 0
}

// runDatabase は DATABASE_URL のDBへ未適用分を適用する。showVersion のときは版の数字だけを出す。
func runDatabase(showVersion bool, stdout io.Writer) error {
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
		fmt.Fprintln(stdout, v)
		return nil
	}
	if err := migrate.Run(ctx, db, migrations.FS); err != nil {
		return fmt.Errorf("マイグレーションに失敗しました: %w", err)
	}
	return nil
}
