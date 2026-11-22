package study

import (
	"testing"
	"time"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestDayStart(t *testing.T) {
	loc := london(t)
	cases := []struct{ now, want time.Time }{
		{time.Date(2026, 11, 2, 3, 59, 0, 0, loc), time.Date(2026, 11, 1, 4, 0, 0, 0, loc)},
		{time.Date(2026, 11, 2, 4, 0, 0, 0, loc), time.Date(2026, 11, 2, 4, 0, 0, 0, loc)},
		{time.Date(2026, 11, 2, 23, 0, 0, 0, loc), time.Date(2026, 11, 2, 4, 0, 0, 0, loc)},
		// Same instant given in UTC: the result is computed in loc.
		{time.Date(2026, 11, 2, 3, 59, 0, 0, loc).UTC(), time.Date(2026, 11, 1, 4, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := DayStart(c.now, loc, 4); !got.Equal(c.want) {
			t.Errorf("DayStart(%v) = %v, want %v", c.now, got, c.want)
		}
	}
	if got, want := DayStart(time.Date(2026, 11, 2, 0, 30, 0, 0, loc), loc, 0), time.Date(2026, 11, 2, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("hour 0: got %v, want %v", got, want)
	}
}

func TestDayStartAcrossDST(t *testing.T) {
	loc := london(t)
	cases := []struct{ now, want time.Time }{
		// Clocks go back at 02:00 BST on 2026-10-25.
		{time.Date(2026, 10, 25, 12, 0, 0, 0, loc), time.Date(2026, 10, 25, 4, 0, 0, 0, loc)},
		// The day after: 25 hours since the previous rollover; Add(-24h) would give 05:00.
		{time.Date(2026, 10, 26, 3, 0, 0, 0, loc), time.Date(2026, 10, 25, 4, 0, 0, 0, loc)},
		// Clocks go forward at 01:00 GMT on 2026-03-29.
		{time.Date(2026, 3, 29, 12, 0, 0, 0, loc), time.Date(2026, 3, 29, 4, 0, 0, 0, loc)},
		{time.Date(2026, 3, 30, 3, 0, 0, 0, loc), time.Date(2026, 3, 29, 4, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := DayStart(c.now, loc, 4); !got.Equal(c.want) {
			t.Errorf("DayStart(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}
