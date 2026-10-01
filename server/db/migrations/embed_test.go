package migrations

import "testing"

func TestFSContainsMarker(t *testing.T) {
	if _, err := FS.ReadFile("README.md"); err != nil {
		t.Fatalf("README.md must be embedded: %v", err)
	}
}
