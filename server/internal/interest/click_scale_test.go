package interest

import (
	"reflect"
	"testing"
)

// 興味度は 0〜1 の小数だが、DB には 10000 倍した整数で保存する。
// ここが 1000 倍や 100000 倍にずれても画面は動いてしまうため、数値で固定する。
func TestFixedPointScaleDigits(t *testing.T) {
	if Scale != 10000 {
		t.Fatalf("Scale = %d; want 10000", Scale)
	}
	if ClickBoost != 2000 { // 0.2
		t.Fatalf("ClickBoost = %d; want 2000 (0.2)", ClickBoost)
	}
	if DecayNumerator != 8 || DecayDenominator != 10 { // 0.8
		t.Fatalf("decay = %d/%d; want 8/10", DecayNumerator, DecayDenominator)
	}
}

// 小数→整数→小数と往復しても、桁がずれない(0.5 は 5000、1.0 は 10000)。
func TestFixedPointRoundTripKeepsDigits(t *testing.T) {
	for _, tc := range []struct {
		fraction float64
		stored   int64
	}{{0, 0}, {0.0001, 1}, {0.2, 2000}, {0.5, 5000}, {0.8, 8000}, {1, 10000}} {
		if got := int64(tc.fraction*float64(Scale) + 0.5); got != tc.stored {
			t.Fatalf("%v → %d; want %d", tc.fraction, got, tc.stored)
		}
		if back := float64(tc.stored) / float64(Scale); back != tc.fraction {
			t.Fatalf("%d → %v; want %v", tc.stored, back, tc.fraction)
		}
	}
}

func TestUpdateOnClickDecaysOthersAndBoostsClicked(t *testing.T) {
	current := []Weight{{TagID: 1, Tag: "Go", Value: 5000}, {TagID: 2, Tag: "Rust", Value: 10000}}
	got, err := UpdateOnClick(current, []string{"go"})
	want := []Weight{{TagID: 1, Tag: "Go", Value: 6000}, {TagID: 2, Tag: "Rust", Value: 8000}} // 0.5→0.4+0.2、1.0→0.8
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v; want %#v", got, err, want)
	}
}

func TestUpdateOnClickNewTagAlongsideExistingIsNotDecayed(t *testing.T) {
	got, err := UpdateOnClick([]Weight{{TagID: 1, Tag: "Go", Value: 5000}}, []string{"Rust"})
	want := []Weight{{TagID: 1, Tag: "Go", Value: 4000}, {Tag: "rust", Value: 2000}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v; want %#v", got, err, want)
	}
}

func TestUpdateOnClickNeverExceedsOne(t *testing.T) {
	for stored, want := range map[int64]int64{8500: 8800, 9500: 9600, 10000: 10000, 20000: 10000} {
		got, err := UpdateOnClick([]Weight{{TagID: 1, Tag: "go", Value: stored}}, []string{"go"})
		if err != nil || got[0].Value != want {
			t.Fatalf("stored %d: got %#v, %v; want %d", stored, got, err, want)
		}
	}
	// 何度押しても 1(=10000)を超えず、収束する。
	w := []Weight{{TagID: 1, Tag: "go", Value: 1}}
	for i := 0; i < 50; i++ {
		var err error
		if w, err = UpdateOnClick(w, []string{"go"}); err != nil || w[0].Value > Scale || w[0].Value < 0 {
			t.Fatalf("click %d: %#v, %v", i, w, err)
		}
	}
	// 整数の切り捨てで数ポイント手前に止まるのは仕様どおり。
	if w[0].Value < Scale-10 {
		t.Fatalf("連打後は上限 %d 付近に収束するはず: %d", Scale, w[0].Value)
	}
}

func TestUpdateOnClickDecayReachesZeroWithoutGoingNegative(t *testing.T) {
	w := []Weight{{TagID: 1, Tag: "go", Value: 10000}, {TagID: 2, Tag: "other", Value: -5}}
	var err error
	for i := 0; i < 200; i++ {
		if w, err = UpdateOnClick(w, nil); err != nil {
			t.Fatal(err)
		}
	}
	if w[0].Value != 0 || w[1].Value != 0 {
		t.Fatalf("got %#v", w)
	}
}
