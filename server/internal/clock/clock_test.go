package clock

import (
	"testing"
	"time"
)

func TestFakeAdvance(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	f := &Fake{T: t0}
	f.Advance(90 * time.Minute)
	if got, want := f.Now(), t0.Add(90*time.Minute); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
	var _ Clock = f
	var _ Clock = Real{}
}
