package auth

import (
	"testing"
	"time"
)

func TestSystemClockReturnsCurrentTime(t *testing.T) {
	before := time.Now()
	got := (SystemClock{}).Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("Now() = %v, want within [%v, %v]", got, before, after)
	}
}
