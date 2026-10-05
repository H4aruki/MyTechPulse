package interest

import (
	"errors"
	"reflect"
	"testing"
)

func TestUpdateOnClickUsesApprovedIntegerRounding(t *testing.T) {
	got, err := UpdateOnClick([]Weight{{TagID: 1, Tag: "Go", Value: 875}}, []string{" go ", "GO"})
	want := []Weight{{TagID: 1, Tag: "Go", Value: 2700}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v; want %#v", got, err, want)
	}
}

func TestUpdateOnClickRejectsCollisionAndDoesNotMutate(t *testing.T) {
	current := []Weight{{TagID: 1, Tag: "Go", Value: 875}, {TagID: 2, Tag: " go ", Value: 1725}}
	before := append([]Weight(nil), current...)
	got, err := UpdateOnClick(current, []string{"GO"})
	if !errors.Is(err, ErrNormalizedTagCollision) || got != nil {
		t.Fatalf("got %#v, %v", got, err)
	}
	if !reflect.DeepEqual(current, before) {
		t.Fatal("current was mutated")
	}
}

func TestUpdateOnClickReturnsNewTagOnce(t *testing.T) {
	got, err := UpdateOnClick(nil, []string{" Rust ", "RUST", " "})
	want := []Weight{{Tag: "rust", Value: 2000}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestIntegerRoundingCompatibilityCases(t *testing.T) {
	for _, tc := range []struct{ stored, goDecay int64 }{{875, 700}, {1725, 1380}, {10000, 8000}} {
		got, err := UpdateOnClick([]Weight{{TagID: 1, Tag: "x", Value: tc.stored}}, nil)
		if err != nil || got[0].Value != tc.goDecay {
			t.Fatalf("stored %d: got %#v, %v", tc.stored, got, err)
		}
	}
}
