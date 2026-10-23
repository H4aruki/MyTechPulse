// Package interest implements fixed-point interest learning.
package interest

import "errors"

const (
	Scale            = int64(10000)
	DecayNumerator   = int64(8)
	DecayDenominator = int64(10)
	ClickBoost       = int64(2000)
	// 記事を新しく保存したときに、その記事のタグへ足す量。クリックより強い意思表示なので大きい。
	SaveBoost = int64(3000)

	// 会員登録時に選んだタグの初期の重み(1.0)。選ばなかったタグには行を作らない(0扱い。#240)。
	InitialSelected = Scale

	// フィードで記事を検索するタグの数。興味度が0のタグは数えない。
	TopTagLimit = 5
)

var ErrNormalizedTagCollision = errors.New("interest: normalized tag collision")

type Weight struct {
	TagID int64
	Tag   string
	Value int64
}
