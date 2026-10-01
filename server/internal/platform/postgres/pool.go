// Package postgres はpgxpoolの生成を担う。エラーに接続文字列を含めない。
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open は接続プールを作成し、Pingで疎通を確認する。
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URLの形式が不正です")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("DB接続を作成できません")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("DBへ接続できません")
	}
	return pool, nil
}
