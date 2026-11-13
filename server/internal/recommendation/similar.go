package recommendation

import (
	"sort"

	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

// minCooccurrence は、似たタグとして使う共起回数の下限（Issue #241）。
// 同じ記事に1回だけ一緒に付いた組は、その記事だけの偶然であることが多い。2回でも、続けて書いた記事の組で起きうる。
// 3は、その偶然を外すための仮の下限。本番に貯まった記事で共起の分布を測った値ではなく、裏づけは無い。
// 記事が少ないうちは下限に届かず、似たタグは0件になる。そのときは空き枠を人気の記事で埋める。
const minCooccurrence = 3

// TagCooccurrence は、興味のあるタグと同じ記事に付いていた別のタグと、その記事の数。
type TagCooccurrence struct {
	Tag   string
	Count int
}

// selectSimilarTags は、共起回数が多い順に、空きの数（limit）だけタグを返す。
// 回数が minCooccurrence 未満のタグと、excluded（興味度が0より大きいタグ）は除く。
// 同じ回数のときは、正規化したタグ名の順。空白と大文字小文字の違いだけのタグは、回数を足して1つにまとめる。
func selectSimilarTags(counts []TagCooccurrence, excluded []string, limit int) []string {
	if limit <= 0 || len(counts) == 0 {
		return nil
	}
	skip := make(map[string]struct{}, len(excluded))
	for _, tag := range excluded {
		if key := interest.NormalizeTag(tag); key != "" {
			skip[key] = struct{}{}
		}
	}
	type row struct {
		key     string
		display string
		count   int
	}
	merged := make(map[string]*row)
	for _, item := range counts {
		key := interest.NormalizeTag(item.Tag)
		if key == "" || item.Count <= 0 {
			continue
		}
		if _, found := skip[key]; found {
			continue
		}
		got, ok := merged[key]
		if !ok {
			merged[key] = &row{key: key, display: item.Tag, count: item.Count}
			continue
		}
		got.count += item.Count
	}
	rows := make([]row, 0, len(merged))
	for _, item := range merged {
		if item.count < minCooccurrence {
			continue
		}
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].key < rows[j].key
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, len(rows))
	for i, item := range rows {
		out[i] = item.display
	}
	return out
}
