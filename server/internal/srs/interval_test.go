package srs

import (
	"testing"
	"time"
)

func TestFormatInterval(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1m"},
		{30 * time.Second, "<1m"},
		{time.Minute, "1m"},
		{10 * time.Minute, "10m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23*time.Hour + 59*time.Minute, "23h"},
		{day, "1d"},
		{29 * day, "29d"},
		{30 * day, "1mo"},
		{45 * day, "1.5mo"},
		{364 * day, "12.1mo"},
		{365 * day, "1y"},
		{548 * day, "1.5y"},
	}
	for _, c := range cases {
		if got := FormatInterval(c.d); got != c.want {
			t.Errorf("FormatInterval(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
