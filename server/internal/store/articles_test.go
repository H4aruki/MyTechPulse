package store

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsSkippableArticleErrors(t *testing.T) {
	for code, want := range map[string]bool{
		"23505": true,  // 一意制約違反
		"23503": true,  // 外部キー違反
		"23514": true,  // CHECK違反
		"22021": true,  // 不正な文字
		"22P05": true,  // 変換できない文字
		"23502": false, // NOT NULL違反は飛ばさない
		"08006": false, // 接続の失敗は実行全体を止める
	} {
		err := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code})
		if got := isSkippable(err); got != want {
			t.Errorf("%s: isSkippable = %v, want %v", code, got, want)
		}
	}
	if isSkippable(errors.New("not a pg error")) {
		t.Error("PostgreSQL以外のエラーは飛ばさない")
	}
}

func TestStripNUL(t *testing.T) {
	if got := stripNUL("a\x00b\x00"); got != "ab" {
		t.Fatalf("got = %q", got)
	}
}

func TestClampLikes(t *testing.T) {
	for in, want := range map[int]int32{
		-5:                0,
		0:                 0,
		42:                42,
		math.MaxInt32:     math.MaxInt32,
		math.MaxInt32 + 1: math.MaxInt32,
	} {
		if got := clampLikes(in); got != want {
			t.Errorf("clampLikes(%d) = %d, want %d", in, got, want)
		}
	}
}
