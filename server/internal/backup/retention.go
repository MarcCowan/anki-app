package backup

import (
	"sort"
	"time"
)

const (
	dailyDays     = 6   // age <= 6: every backup
	weeklyDays    = 31  // age <= 31: newest of each Monday-Sunday week
	monthlyDays   = 92  // age <= 92: newest of each calendar month
	quarterlyDays = 366 // age <= 366: newest of each calendar quarter
	dueHour       = 3   // scheduled backups start at 03:00 local
)

// localDate returns t's calendar date in loc as midnight UTC, so that
// differences between dates are whole days regardless of DST.
func localDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type periodKey struct {
	kind  byte // 'w' (week), 'm' (month), 'q' (quarter)
	year  int
	index int // month number, or quarter 0-3 (for a week: month of its Monday)
	day   int // day of month of the week's Monday; 0 for month and quarter
}

func keysFor(date time.Time) [3]periodKey {
	weekStart := date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
	y, m, d := weekStart.Date()
	return [3]periodKey{
		{'w', y, int(m), d},
		{'m', date.Year(), int(date.Month()), 0},
		{'q', date.Year(), (int(date.Month()) - 1) / 3, 0},
	}
}

// Keep applies the daily-folder retention rules (spec 4.3) and returns the
// backups to keep and to drop, both ascending. Backups dated after now are
// always kept.
func Keep(backups []time.Time, now time.Time, loc *time.Location) (keep, drop []time.Time) {
	sorted := append([]time.Time(nil), backups...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	today := localDate(now, loc)
	newest := map[periodKey]time.Time{}
	for _, b := range sorted { // ascending, so the last write wins
		for _, k := range keysFor(localDate(b, loc)) {
			newest[k] = b
		}
	}

	for _, b := range sorted {
		date := localDate(b, loc)
		age := int(today.Sub(date).Hours() / 24)
		keys := keysFor(date)
		switch {
		case age < 0, age <= dailyDays:
			keep = append(keep, b)
		case age <= weeklyDays && newest[keys[0]].Equal(b):
			keep = append(keep, b)
		case age <= monthlyDays && newest[keys[1]].Equal(b):
			keep = append(keep, b)
		case age <= quarterlyDays && newest[keys[2]].Equal(b):
			keep = append(keep, b)
		default:
			drop = append(drop, b)
		}
	}
	return keep, drop
}

// KeepNewest keeps the n newest backups and drops the rest, both ascending.
func KeepNewest(backups []time.Time, n int) (keep, drop []time.Time) {
	sorted := append([]time.Time(nil), backups...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	if n < 0 {
		n = 0
	}
	cut := len(sorted) - n
	if cut < 0 {
		cut = 0
	}
	return sorted[cut:], sorted[:cut:cut]
}

// Due reports whether a scheduled backup should run now: the local time is at
// or after 03:00 and no backup in daily has today's local date.
func Due(now time.Time, loc *time.Location, daily []time.Time) bool {
	if now.In(loc).Hour() < dueHour {
		return false
	}
	today := localDate(now, loc)
	for _, b := range daily {
		if localDate(b, loc).Equal(today) {
			return false
		}
	}
	return true
}
