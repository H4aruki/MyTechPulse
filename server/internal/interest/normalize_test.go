package interest

import (
	"errors"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	if got := NormalizeTag(" Go "); got != "go" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateCurrentRejectsCollision(t *testing.T) {
	if err := ValidateCurrent([]Weight{{Tag: "Go"}, {Tag: " go "}}); !errors.Is(err, ErrNormalizedTagCollision) {
		t.Fatalf("got %v", err)
	}
}
