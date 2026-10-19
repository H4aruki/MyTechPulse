package interest

import "sort"

// Top は、興味度が0より大きいタグを強い順に最大 n 件返す。入力は変えない。
// 同じ強さのときは、正規化したタグ名(NormalizeTag)のバイト順、さらにタグIDの順で決める。
// フィードの検索に使うタグと、アンケートの興味スナップショットは、どちらもこの順番を使う。
// 興味度が0のタグ(行が無いのと同じ)は、上位に入れない(#240)。
func Top(weights []Weight, n int) []Weight {
	out := make([]Weight, 0, len(weights))
	for _, weight := range weights {
		if weight.Value > 0 {
			out = append(out, weight)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		a, b := NormalizeTag(out[i].Tag), NormalizeTag(out[j].Tag)
		if a != b {
			return a < b
		}
		return out[i].TagID < out[j].TagID
	})
	if n < 0 {
		n = 0
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}
