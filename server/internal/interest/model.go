// Package interest implements fixed-point interest learning.
package interest

import "errors"

const (
	Scale            = int64(10000)
	DecayNumerator   = int64(8)
	DecayDenominator = int64(10)
	ClickBoost       = int64(2000)

	// 会員登録時の初期の重み。選んだタグは1.0、選ばなかったタグは0.8(1万倍の整数)。
	InitialSelected   = Scale
	InitialUnselected = Scale * DecayNumerator / DecayDenominator
)

var ErrNormalizedTagCollision = errors.New("interest: normalized tag collision")

type Weight struct {
	TagID int64
	Tag   string
	Value int64
}
