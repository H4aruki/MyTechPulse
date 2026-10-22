package articlefetch

import (
	"sort"
	"strings"

	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

// TagMatcher は、外部の記事のタグ名を tag 表の tag_ID に照合する。
// 照合だけを行い、tag 表の行は作らない（知らないタグは無視する）。
type TagMatcher struct {
	exact   map[string]int32   // 正規化した名前 → 最小のID
	compact map[string][]int32 // 空白を除いた名前 → ID（重複なし）
}

// NewTagMatcher は tag 表の内容から照合表を作る。
func NewTagMatcher(tags []TagRef) *TagMatcher {
	m := &TagMatcher{exact: make(map[string]int32, len(tags)), compact: make(map[string][]int32, len(tags))}
	sorted := append([]TagRef(nil), tags...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, t := range sorted {
		key := interest.NormalizeTag(t.Name)
		if key == "" {
			continue
		}
		if _, ok := m.exact[key]; !ok {
			m.exact[key] = t.ID // 大文字小文字だけ違う既存タグは、最小のIDを使う
		}
		ck := compactKey(key)
		if ids := m.compact[ck]; len(ids) == 0 || ids[len(ids)-1] != t.ID {
			m.compact[ck] = append(ids, t.ID)
		}
	}
	return m
}

// compactKey は空白をすべて除く（Qiitaの "SpringBoot" と表の "Spring Boot" を結ぶため）。
func compactKey(s string) string { return strings.Join(strings.Fields(s), "") }

// Match は name に対応する tag_ID を返す。
// まず正規化した名前（前後の空白を除いて小文字）の完全一致、なければ空白を除いた名前で候補が1つだけのときに採用する。
func (m *TagMatcher) Match(name string) (int32, bool) {
	key := interest.NormalizeTag(name)
	if key == "" {
		return 0, false
	}
	if id, ok := m.exact[key]; ok {
		return id, true
	}
	if ids := m.compact[compactKey(key)]; len(ids) == 1 {
		return ids[0], true
	}
	return 0, false
}

// MatchAll は names を照合し、見つかった tag_ID を重複なく、見つかった順に返す。
func (m *TagMatcher) MatchAll(names []string) []int32 {
	var ids []int32
	seen := map[int32]struct{}{}
	for _, n := range names {
		id, ok := m.Match(n)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
