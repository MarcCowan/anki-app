package srs

import (
	"strconv"
	"strings"
	"time"
)

// FormatInterval renders a duration as a short button label: "<1m", "10m",
// "3h", "4d", "1.5mo", "2y". Minutes, hours and days are truncated; months
// (30 days) and years (365 days) show at most one decimal.
func FormatInterval(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d < day:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	case d < 30*day:
		return strconv.FormatInt(int64(d/day), 10) + "d"
	}
	days := d.Hours() / 24
	if days < 365 {
		return oneDecimal(days/30) + "mo"
	}
	return oneDecimal(days/365) + "y"
}

func oneDecimal(x float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(x, 'f', 1, 64), ".0")
}
