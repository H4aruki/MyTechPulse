// Package migrations はgoose SQLを実行ファイルへ埋め込む。
package migrations

import "embed"

//go:embed *
var FS embed.FS
