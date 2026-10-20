package interest

import "testing"

func TestInitialWeights(t *testing.T) {
	if InitialSelected != 10000 {
		t.Fatalf("selected=%d", InitialSelected)
	}
	if TopTagLimit != 5 {
		t.Fatalf("top tags=%d", TopTagLimit)
	}
}

// 選ばなかったタグを0から始めると、そのタグの記事を4回押せば、押していない選んだタグ(1.0)を超える(#240の決定)。
func TestUnselectedTagOvertakesSelectedAfterFourClicks(t *testing.T) {
	current := []Weight{{TagID: 1, Tag: "Go", Value: InitialSelected}}
	for clicks := 1; clicks <= 4; clicks++ {
		next, err := UpdateOnClick(current, []string{"Rust"})
		if err != nil {
			t.Fatal(err)
		}
		current = next
		top := Top(current, 1)
		if wantRust := clicks >= 4; (top[0].Tag == "rust") != wantRust {
			t.Fatalf("%d回目: 1位 = %#v (全体 %#v)", clicks, top[0], current)
		}
	}
}
