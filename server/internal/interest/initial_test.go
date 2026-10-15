package interest

import "testing"

func TestInitialWeights(t *testing.T) {
	if InitialSelected != 10000 || InitialUnselected != 8000 {
		t.Fatalf("selected=%d unselected=%d", InitialSelected, InitialUnselected)
	}
}
