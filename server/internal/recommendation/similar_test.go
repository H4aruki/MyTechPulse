package recommendation

import (
	"slices"
	"testing"
)

func TestSelectSimilarTagsKeepsTagsAtTheFloorAndDropsTheRest(t *testing.T) {
	if minCooccurrence < 2 {
		t.Fatalf("minCooccurrence = %d。下限の境目をこの試験で確かめられない", minCooccurrence)
	}
	got := selectSimilarTags([]TagCooccurrence{
		{Tag: "Docker", Count: minCooccurrence - 1},
		{Tag: "Rust", Count: minCooccurrence},
		{Tag: "  ", Count: 100},
		{Tag: "Kotlin", Count: 0},
		{Tag: "Swift", Count: -1},
	}, nil, 5)
	if !slices.Equal(got, []string{"Rust"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestSelectSimilarTagsSumsDuplicatesBeforeTheFloor(t *testing.T) {
	// 1件ずつなら下限未満でも、正規化して足すと届く。空白と大文字小文字は同じタグ。
	half := minCooccurrence - 1
	if half < 1 {
		half = 1
	}
	got := selectSimilarTags([]TagCooccurrence{
		{Tag: "Rust", Count: half},
		{Tag: " rust ", Count: half},
	}, nil, 5)
	if minCooccurrence > 1 && half*2 < minCooccurrence {
		t.Fatal("試験データが下限に届いていない")
	}
	if !slices.Equal(got, []string{"Rust"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestSelectSimilarTagsSkipsTagsTheUserAlreadyLikes(t *testing.T) {
	got := selectSimilarTags([]TagCooccurrence{
		{Tag: " Rust ", Count: minCooccurrence + 5},
		{Tag: "Kotlin", Count: minCooccurrence},
	}, []string{"rust"}, 5)
	if !slices.Equal(got, []string{"Kotlin"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestSelectSimilarTagsOrdersByCountThenNameAndLimits(t *testing.T) {
	floor := minCooccurrence
	got := selectSimilarTags([]TagCooccurrence{
		{Tag: "Swift", Count: floor},
		{Tag: "Rust", Count: floor + 2},
		{Tag: "Kotlin", Count: floor + 2},
		{Tag: "Go", Count: floor + 9},
	}, []string{"go"}, 2)
	// Go は興味度があるので除く。同じ回数の Kotlin と Rust は、名前の順。
	if !slices.Equal(got, []string{"Kotlin", "Rust"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestSelectSimilarTagsReturnsNothingWithoutRoomOrCounts(t *testing.T) {
	counts := []TagCooccurrence{{Tag: "Rust", Count: minCooccurrence}}
	if got := selectSimilarTags(counts, nil, 0); len(got) != 0 {
		t.Fatalf("limit 0: %v", got)
	}
	if got := selectSimilarTags(counts, nil, -1); len(got) != 0 {
		t.Fatalf("limit -1: %v", got)
	}
	if got := selectSimilarTags(nil, nil, 5); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
}
