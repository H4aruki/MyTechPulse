package interest

import (
	"reflect"
	"testing"
)

func TestTopSkipsZeroSortsAndLimits(t *testing.T) {
	in := []Weight{
		{TagID: 1, Tag: "Python", Value: 0},
		{TagID: 2, Tag: "go", Value: 8000},
		{TagID: 3, Tag: "Android", Value: 8000},
		{TagID: 4, Tag: " Rust ", Value: 9000},
		{TagID: 5, Tag: "TypeScript", Value: 100},
		{TagID: 6, Tag: "AWS", Value: 100},
		{TagID: 7, Tag: "Docker", Value: 50},
		{TagID: 8, Tag: "negative", Value: -1},
	}
	before := append([]Weight(nil), in...)
	got := Top(in, 5)
	var ids []int64
	for _, w := range got {
		ids = append(ids, w.TagID)
	}
	if want := []int64{4, 3, 2, 6, 5}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("Top = %v, want %v", ids, want)
	}
	if !reflect.DeepEqual(in, before) {
		t.Fatal("入力を書き換えてはいけない")
	}
}

func TestTopReturnsFewerWhenNotEnoughPositive(t *testing.T) {
	got := Top([]Weight{{TagID: 1, Tag: "Go", Value: 10000}, {TagID: 2, Tag: "Android", Value: 0}}, 5)
	if len(got) != 1 || got[0].Tag != "Go" {
		t.Fatalf("Top = %#v", got)
	}
	if got := Top(nil, 5); len(got) != 0 {
		t.Fatalf("空なら空: %#v", got)
	}
	if got := Top([]Weight{{TagID: 1, Tag: "Go", Value: 1}}, -1); len(got) != 0 {
		t.Fatalf("nが負なら空: %#v", got)
	}
}

func TestTopBreaksNameTiesByTagID(t *testing.T) {
	// 正規化後の名前が同じ行は本来ありえない(ValidateCurrentで止まる)が、並びは毎回同じにする
	got := Top([]Weight{{TagID: 9, Tag: "Go", Value: 1}, {TagID: 3, Tag: "go", Value: 1}}, 5)
	if got[0].TagID != 3 || got[1].TagID != 9 {
		t.Fatalf("Top = %#v", got)
	}
}
