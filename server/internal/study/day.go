package study

import "time"

// DayStart returns the most recent rolloverHour:00 in loc at or before now.
// It steps back by calendar day (not 24 hours), so it is correct across DST changes.
func DayStart(now time.Time, loc *time.Location, rolloverHour int) time.Time {
	l := now.In(loc)
	ds := time.Date(l.Year(), l.Month(), l.Day(), rolloverHour, 0, 0, 0, loc)
	if ds.After(l) {
		ds = time.Date(l.Year(), l.Month(), l.Day()-1, rolloverHour, 0, 0, 0, loc)
	}
	return ds
}
