package articlefetch

import (
	"testing"
	"time"
)

func TestSlotsAroundEachScheduledTime(t *testing.T) {
	loc := Location()
	for _, tc := range []struct {
		name         string
		now          time.Time
		latest, next time.Time
	}{
		{"5:59は前日の16:30と6:00", jstTime(2026, 10, 22, 5, 59), jstTime(2026, 10, 21, 16, 30), jstTime(2026, 10, 22, 6, 0)},
		{"6:00ちょうど", jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 11, 30)},
		{"6:01", jstTime(2026, 10, 22, 6, 1), jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 11, 30)},
		{"11:29", jstTime(2026, 10, 22, 11, 29), jstTime(2026, 10, 22, 6, 0), jstTime(2026, 10, 22, 11, 30)},
		{"11:30ちょうど", jstTime(2026, 10, 22, 11, 30), jstTime(2026, 10, 22, 11, 30), jstTime(2026, 10, 22, 16, 30)},
		{"16:29", jstTime(2026, 10, 22, 16, 29), jstTime(2026, 10, 22, 11, 30), jstTime(2026, 10, 22, 16, 30)},
		{"16:30ちょうど", jstTime(2026, 10, 22, 16, 30), jstTime(2026, 10, 22, 16, 30), jstTime(2026, 10, 23, 6, 0)},
		{"23:59は翌6:00", jstTime(2026, 10, 22, 23, 59), jstTime(2026, 10, 22, 16, 30), jstTime(2026, 10, 23, 6, 0)},
		{"0:00", jstTime(2026, 10, 23, 0, 0), jstTime(2026, 10, 22, 16, 30), jstTime(2026, 10, 23, 6, 0)},
		{"月またぎ", jstTime(2026, 10, 31, 20, 0), jstTime(2026, 10, 31, 16, 30), jstTime(2026, 11, 1, 6, 0)},
		{"年またぎ", jstTime(2026, 12, 31, 20, 0), jstTime(2026, 12, 31, 16, 30), jstTime(2027, 1, 1, 6, 0)},
		{"UTCで渡しても日本時間で数える", time.Date(2026, 10, 21, 20, 59, 0, 0, time.UTC), jstTime(2026, 10, 21, 16, 30), jstTime(2026, 10, 22, 6, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LatestSlot(tc.now, loc); !got.Equal(tc.latest) {
				t.Errorf("LatestSlot = %v, want %v", got, tc.latest)
			}
			if got := NextSlot(tc.now, loc); !got.Equal(tc.next) {
				t.Errorf("NextSlot = %v, want %v", got, tc.next)
			}
		})
	}
}

func TestFixedZoneFallbackGivesSameSlots(t *testing.T) {
	fixed := time.FixedZone("JST", 9*60*60)
	for _, now := range []time.Time{jstTime(2026, 10, 22, 5, 59), jstTime(2026, 10, 22, 12, 0), jstTime(2026, 10, 22, 17, 0)} {
		if a, b := LatestSlot(now, Location()), LatestSlot(now, fixed); !a.Equal(b) {
			t.Errorf("LatestSlot %v != %v", a, b)
		}
		if a, b := NextSlot(now, Location()), NextSlot(now, fixed); !a.Equal(b) {
			t.Errorf("NextSlot %v != %v", a, b)
		}
	}
}
