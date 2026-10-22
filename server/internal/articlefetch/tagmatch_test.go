package articlefetch

import (
	"reflect"
	"testing"
)

func TestTagMatcher(t *testing.T) {
	m := NewTagMatcher([]TagRef{
		{ID: 1, Name: "Go"},
		{ID: 2, Name: "Spring Boot"},
		{ID: 3, Name: "C++"},
		{ID: 4, Name: "React Native"},
		{ID: 5, Name: "ReactNative2"}, // 空白を除いた名前が "reactnative2"
		{ID: 6, Name: "Next.js"},
		{ID: 7, Name: "Vue js"},
		{ID: 8, Name: "Vuejs"},
	})
	for _, tc := range []struct {
		in     string
		want   int32
		wantOK bool
	}{
		{"Go", 1, true},
		{"go", 1, true},
		{"  GO  ", 1, true},
		{"SpringBoot", 2, true},
		{"springboot", 2, true},
		{"Spring Boot", 2, true},
		{"c++", 3, true},
		{"ReactNative", 4, true},
		{"next.js", 6, true},
		{"Vuejs", 8, true}, // 完全一致を優先
		{"Vue js", 7, true},
		{"unknown", 0, false},
		{"", 0, false},
		{"   ", 0, false},
	} {
		got, ok := m.Match(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("Match(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestTagMatcherAmbiguousCompactMatchIsSkipped(t *testing.T) {
	// "Spring Boot" と "Spring  Boot!"（空白を除くと別）ではなく、空白を除くと同じになる2つのタグ。
	m := NewTagMatcher([]TagRef{{ID: 1, Name: "Spring Boot"}, {ID: 2, Name: "Spr ing Boot"}})
	if id, ok := m.Match("SpringBoot"); ok {
		t.Fatalf("候補が2つなのに採用された: %d", id)
	}
	// 完全一致なら採用する
	if id, ok := m.Match("spring boot"); !ok || id != 1 {
		t.Fatalf("Match = %d, %v", id, ok)
	}
}

func TestTagMatcherCaseOnlyDuplicatesUseSmallestID(t *testing.T) {
	m := NewTagMatcher([]TagRef{{ID: 9, Name: "GO"}, {ID: 3, Name: "Go"}})
	if id, ok := m.Match("go"); !ok || id != 3 {
		t.Fatalf("Match = %d, %v, want 3", id, ok)
	}
}

func TestTagMatcherMatchAllDedupesAndIgnoresUnknown(t *testing.T) {
	m := NewTagMatcher([]TagRef{{ID: 1, Name: "Go"}, {ID: 2, Name: "Spring Boot"}})
	got := m.MatchAll([]string{"Go", "unknown", "SpringBoot", "go", "GO"})
	if want := []int32{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchAll = %v, want %v", got, want)
	}
	if got := m.MatchAll([]string{"x", "y"}); len(got) != 0 {
		t.Fatalf("MatchAll = %v", got)
	}
}
