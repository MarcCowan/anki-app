package backup

import (
	"testing"
	"time"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return loc
}

// at builds a local time in loc.
func at(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func contains(ts []time.Time, want time.Time) bool {
	for _, x := range ts {
		if x.Equal(want) {
			return true
		}
	}
	return false
}

func assertSplit(t *testing.T, keep, drop []time.Time, wantKept, wantDropped []time.Time) {
	t.Helper()
	for _, w := range wantKept {
		if !contains(keep, w) || contains(drop, w) {
			t.Errorf("%v should be kept (keep=%v)", w, keep)
		}
	}
	for _, w := range wantDropped {
		if !contains(drop, w) || contains(keep, w) {
			t.Errorf("%v should be dropped (keep=%v)", w, keep)
		}
	}
}

func TestKeepTierBoundaries(t *testing.T) {
	loc := london(t)
	now := at(loc, 2026, time.November, 5, 12, 0)
	oct31 := at(loc, 2026, time.October, 31, 12, 0)
	sep30 := at(loc, 2026, time.September, 30, 12, 0)
	oct30 := at(loc, 2026, time.October, 30, 12, 0)
	oct29 := at(loc, 2026, time.October, 29, 12, 0)
	oct5 := at(loc, 2026, time.October, 5, 12, 0)
	oct4 := at(loc, 2026, time.October, 4, 12, 0)
	aug5 := at(loc, 2026, time.August, 5, 12, 0)
	aug4 := at(loc, 2026, time.August, 4, 12, 0)
	nov4y := at(loc, 2025, time.November, 4, 12, 0)
	nov3y := at(loc, 2025, time.November, 3, 12, 0)

	cases := []struct {
		name  string
		input []time.Time
		kept  []time.Time
		drop  []time.Time
	}{
		// Daily: age 6 = Oct 30, age 7 = Oct 29. Oct 31 is newer in the same week and month.
		{"daily 6 kept", []time.Time{oct30, oct31}, []time.Time{oct30}, nil},
		{"daily 7 dropped", []time.Time{oct29, oct31}, nil, []time.Time{oct29}},
		// Weekly: age 31 = Mon Oct 5, age 32 = Sun Oct 4. Oct 31 is newest of October.
		{"weekly 31 kept", []time.Time{oct5, oct31}, []time.Time{oct5}, nil},
		{"weekly 32 dropped", []time.Time{oct4, oct31}, nil, []time.Time{oct4}},
		// Monthly: age 92 = Aug 5, age 93 = Aug 4. Sep 30 is the newest of Q3.
		{"monthly 92 kept", []time.Time{aug5, sep30}, []time.Time{aug5}, nil},
		{"monthly 93 dropped", []time.Time{aug4, sep30}, []time.Time{sep30}, []time.Time{aug4}},
		// Quarterly: age 366 = 2025-11-04, age 367 = 2025-11-03 (both Q4 2025, beyond monthly).
		{"quarterly 366 kept", []time.Time{nov4y}, []time.Time{nov4y}, nil},
		{"quarterly 367 dropped", []time.Time{nov3y}, nil, []time.Time{nov3y}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keep, drop := Keep(c.input, now, loc)
			assertSplit(t, keep, drop, c.kept, c.drop)
		})
	}
}

func TestKeepWeekIsMondayToSunday(t *testing.T) {
	loc := london(t)
	now := at(loc, 2026, time.November, 5, 12, 0)
	sun := at(loc, 2026, time.October, 11, 12, 0)
	mon := at(loc, 2026, time.October, 12, 12, 0)
	oct31 := at(loc, 2026, time.October, 31, 12, 0)
	keep, drop := Keep([]time.Time{sun, mon, oct31}, now, loc)
	assertSplit(t, keep, drop, []time.Time{sun, mon}, nil)
}

func TestKeepMissingSundayUsesSaturday(t *testing.T) {
	loc := london(t)
	now := at(loc, 2026, time.November, 6, 12, 0)
	var in []time.Time
	for d := 12; d <= 17; d++ { // Mon Oct 12 .. Sat Oct 17
		in = append(in, at(loc, 2026, time.October, d, 3, 5))
	}
	in = append(in, at(loc, 2026, time.October, 31, 3, 5))
	keep, drop := Keep(in, now, loc)
	sat := at(loc, 2026, time.October, 17, 3, 5) // age 20
	assertSplit(t, keep, drop, []time.Time{sat}, in[:5])
}

func TestKeepMonthAndQuarterEnds(t *testing.T) {
	loc := london(t)
	t.Run("31 Mar vs 1 Apr", func(t *testing.T) {
		now := at(loc, 2026, time.May, 10, 12, 0) // Mar 31 is age 40, Apr 1 age 39
		m30 := at(loc, 2026, time.March, 30, 12, 0)
		m31 := at(loc, 2026, time.March, 31, 12, 0)
		a1 := at(loc, 2026, time.April, 1, 12, 0)
		keep, drop := Keep([]time.Time{m30, m31, a1}, now, loc)
		assertSplit(t, keep, drop, []time.Time{m31, a1}, []time.Time{m30})
	})
	t.Run("30 Sep vs 1 Oct", func(t *testing.T) {
		now := at(loc, 2027, time.January, 20, 12, 0) // Sep 30 is age 112, Oct 1 age 111
		s29 := at(loc, 2026, time.September, 29, 12, 0)
		s30 := at(loc, 2026, time.September, 30, 12, 0)
		o1 := at(loc, 2026, time.October, 1, 12, 0)
		keep, drop := Keep([]time.Time{s29, s30, o1}, now, loc)
		assertSplit(t, keep, drop, []time.Time{s30, o1}, []time.Time{s29})
	})
}

func TestKeepAcrossDST(t *testing.T) {
	loc := london(t)
	// daily returns one backup per local date from..to at 00:30 local.
	daily := func(from, to time.Time) []time.Time {
		var out []time.Time
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			out = append(out, time.Date(d.Year(), d.Month(), d.Day(), 0, 30, 0, 0, loc))
		}
		return out
	}
	check := func(t *testing.T, in []time.Time, now time.Time, wantKept []time.Time) {
		t.Helper()
		seen := map[string]bool{}
		for _, b := range in {
			k := b.In(loc).Format("2006-01-02")
			if seen[k] {
				t.Fatalf("fixture has two backups on %s", k)
			}
			seen[k] = true
		}
		keep, _ := Keep(in, now, loc)
		if len(keep) != len(wantKept) {
			t.Fatalf("kept %d, want %d: %v", len(keep), len(wantKept), keep)
		}
		for _, w := range wantKept {
			if !contains(keep, w) {
				t.Errorf("%v missing from keep", w)
			}
		}
	}
	t.Run("spring forward 2026-03-29", func(t *testing.T) {
		in := daily(at(loc, 2026, time.March, 27, 0, 0), at(loc, 2026, time.April, 5, 0, 0))
		now := at(loc, 2026, time.April, 5, 12, 0)
		// Mar 30..Apr 5 are age 6..0. Sun Mar 29 is the newest of its week. Mar 27/28 go.
		want := daily(at(loc, 2026, time.March, 29, 0, 0), at(loc, 2026, time.April, 5, 0, 0))
		check(t, in, now, want)
	})
	t.Run("fall back 2026-10-25", func(t *testing.T) {
		in := daily(at(loc, 2026, time.October, 17, 0, 0), at(loc, 2026, time.October, 25, 0, 0))
		now := at(loc, 2026, time.October, 25, 12, 0)
		// Oct 19..25 are age 6..0. Sun Oct 18 is its week's newest. Sat Oct 17 goes.
		want := daily(at(loc, 2026, time.October, 18, 0, 0), at(loc, 2026, time.October, 25, 0, 0))
		check(t, in, now, want)
	})
}

func TestKeepTwoYearsOfDailies(t *testing.T) {
	loc := london(t)
	var kept []time.Time
	var last time.Time
	end := time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)
	for d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); !d.After(end); d = d.AddDate(0, 0, 1) {
		now := time.Date(d.Year(), d.Month(), d.Day(), 3, 5, 0, 0, loc)
		kept = append(kept, now)
		keep, _ := Keep(kept, now, loc)
		if len(keep) > 20 {
			t.Fatalf("%s: kept %d backups", d.Format("2006-01-02"), len(keep))
		}
		kept = keep
		last = now
	}
	// Expected survivors on Fri 2027-12-31, worked out from the rules:
	//   daily:     Dec 25..Dec 31 (age 6..0)
	//   weekly:    Sundays Dec 19, 12, 5 (Dec 26 is already daily; Nov 28 is age 33)
	//   monthly:   Nov 30, Oct 31, Sep 30 (age 92)
	//   quarterly: Jun 30 and Mar 31 2027, Dec 31 2026 (age 365)
	want := map[string]bool{}
	for d := 25; d <= 31; d++ {
		want[time.Date(2027, 12, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")] = true
	}
	for _, s := range []string{"2027-12-19", "2027-12-12", "2027-12-05",
		"2027-11-30", "2027-10-31", "2027-09-30",
		"2027-06-30", "2027-03-31", "2026-12-31"} {
		want[s] = true
	}
	got := map[string]bool{}
	for _, k := range kept {
		got[k.In(loc).Format("2006-01-02")] = true
	}
	for s := range want {
		if !got[s] {
			t.Errorf("survivor %s missing on %s", s, last.Format("2006-01-02"))
		}
	}
	for s := range got {
		if !want[s] {
			t.Errorf("unexpected survivor %s", s)
		}
	}
}

func TestKeepNeverDropsFutureBackups(t *testing.T) {
	loc := london(t)
	now := at(loc, 2026, time.November, 5, 12, 0)
	tomorrow := at(loc, 2026, time.November, 6, 3, 5)
	keep, drop := Keep([]time.Time{tomorrow}, now, loc)
	assertSplit(t, keep, drop, []time.Time{tomorrow}, nil)
}

func TestKeepNewest(t *testing.T) {
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var in []time.Time
	for i := 0; i < 12; i++ {
		in = append(in, base.AddDate(0, 0, i))
	}
	keep, drop := KeepNewest(in, 10)
	if len(keep) != 10 || len(drop) != 2 {
		t.Fatalf("keep=%d drop=%d", len(keep), len(drop))
	}
	if !keep[0].Equal(in[2]) || !keep[9].Equal(in[11]) {
		t.Errorf("keep not the newest 10 ascending: %v", keep)
	}
	if !drop[0].Equal(in[0]) || !drop[1].Equal(in[1]) {
		t.Errorf("drop = %v", drop)
	}
	keep, drop = KeepNewest(in[:3], 10)
	if len(keep) != 3 || len(drop) != 0 {
		t.Fatalf("keep=%d drop=%d", len(keep), len(drop))
	}
}

func TestDue(t *testing.T) {
	loc := london(t)
	cases := []struct {
		name  string
		now   time.Time
		daily []time.Time
		want  bool
	}{
		{"before 03:00, none today", at(loc, 2026, time.November, 5, 2, 59), nil, false},
		{"at 03:00, none today", at(loc, 2026, time.November, 5, 3, 0), nil, true},
		{"at 03:00, one at 03:00 today", at(loc, 2026, time.November, 5, 3, 0),
			[]time.Time{at(loc, 2026, time.November, 5, 3, 0)}, false},
		{"last backup 4 days ago", at(loc, 2026, time.November, 5, 12, 0),
			[]time.Time{at(loc, 2026, time.November, 1, 10, 0)}, true},
		// 00:30 BST today is 23:30 UTC yesterday: still today's local date.
		{"summer backup at 00:30 local today", at(loc, 2026, time.July, 1, 3, 10),
			[]time.Time{at(loc, 2026, time.July, 1, 0, 30)}, false},
		{"backup yesterday evening", at(loc, 2026, time.July, 1, 3, 10),
			[]time.Time{at(loc, 2026, time.June, 30, 23, 30)}, true},
		{"only backup is tomorrow", at(loc, 2026, time.November, 5, 3, 10),
			[]time.Time{at(loc, 2026, time.November, 6, 3, 5)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Due(c.now, loc, c.daily); got != c.want {
				t.Fatalf("Due = %v, want %v", got, c.want)
			}
		})
	}
}
