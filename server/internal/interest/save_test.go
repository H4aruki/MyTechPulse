package interest

import (
	"reflect"
	"testing"
)

func TestUpdateOnSaveBoostsExistingAndAddsNewWithoutDecay(t *testing.T) {
	current := []Weight{
		{TagID: 1, Tag: "Go", Value: 5000},
		{TagID: 2, Tag: "Rust", Value: 4000},
	}
	got, err := UpdateOnSave(current, []string{"go", "zig", "Elixir"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Weight{
		{TagID: 1, Tag: "Go", Value: 8000},   // 既存は +3000
		{TagID: 2, Tag: "Rust", Value: 4000}, // ほかのタグは減衰しない
		{Tag: "elixir", Value: 3000},         // 新しいタグは 3000。正規化した名前の順
		{Tag: "zig", Value: 3000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUpdateOnSaveStopsAtScale(t *testing.T) {
	got, err := UpdateOnSave([]Weight{{TagID: 1, Tag: "Go", Value: 9000}, {TagID: 2, Tag: "Rust", Value: Scale}}, []string{"go", "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Value != Scale || got[1].Value != Scale {
		t.Fatalf("got %+v, want both %d", got, Scale)
	}
}

func TestUpdateOnSaveCountsDuplicatesAndCaseOnce(t *testing.T) {
	got, err := UpdateOnSave([]Weight{{TagID: 1, Tag: "Go", Value: 1000}}, []string{"Go", " GO ", "go", "Zig", "zig", "", "  "})
	if err != nil {
		t.Fatal(err)
	}
	want := []Weight{{TagID: 1, Tag: "Go", Value: 4000}, {Tag: "zig", Value: 3000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUpdateOnSaveDoesNotMutateInput(t *testing.T) {
	current := []Weight{{TagID: 1, Tag: "Go", Value: 5000}}
	if _, err := UpdateOnSave(current, []string{"go", "zig"}); err != nil {
		t.Fatal(err)
	}
	if want := []Weight{{TagID: 1, Tag: "Go", Value: 5000}}; !reflect.DeepEqual(current, want) {
		t.Fatalf("入力が変わった: %+v", current)
	}
}

func TestUpdateOnSaveRejectsNormalizedCollision(t *testing.T) {
	got, err := UpdateOnSave([]Weight{{TagID: 1, Tag: "Go", Value: 1}, {TagID: 2, Tag: "GO", Value: 1}}, []string{"go"})
	if err == nil || got != nil {
		t.Fatalf("got %+v, err = %v", got, err)
	}
}
